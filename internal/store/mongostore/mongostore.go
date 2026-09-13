// Package mongostore es la implementacion real de store.Store contra un
// Mongo (local para probar, o UAT/PRO cuando llegue el acceso). Reemplaza
// a mockstore: en vez de mantener datos de prueba como structs Go, lee
// documentos Mongo reales.
//
// Estructura real de un documento (confirmada contra UAT, restaurada en el
// Mongo local -- ver docker-compose.yml -- Y contra el codigo fuente real de
// Hermes en support-otc-hermes-hms-master/modules/entity/<coleccion>/
// content/<coleccion>_standard/model.go, que es la fuente mas confiable:
// una muestra en vivo puede tener un campo opcional vacio/ausente sin dejar
// de ser valida, el struct Go dice que existe de verdad para ese tipo),
// igual en las 6 colecciones salvo donde se aclara:
//   - Nivel raiz: {_id, uuid, ucode, revision, countryCode, parentUCode,
//     data, control, countryList} (mas parentUUID en servicePlace/
//     servicePoint, referencia por uuid interno que este proyecto no usa --
//     el traversal es 100% via ucode/parentUCode). "control" es metadata de
//     sincronizacion entre sistemas, no tiene datos de negocio.
//   - El nombre de negocio NO esta a nivel raiz (no existe un campo
//     "name") -- vive ADENTRO de "data", con distinto campo segun el tipo
//     y sin una unica fuente confiable (ver nameFieldCandidates/
//     pickDisplayName mas abajo para el detalle).
//   - "data" mezcla strings, booleanos (billingCenter, financialEntity,
//     merged) y arrays (agents, contacts) -- por eso Data en mongoDoc es
//     map[string]interface{}, no map[string]string.
//   - "direccion" (serviceAddress) tiene una forma totalmente distinta a
//     las otras 5: no tiene ningun campo de nombre de negocio ni control/
//     countryList -- ver dictionary.go para sus campos reales
//     (streetName, number, postalCode, descriptionLevel1/2, level1/2/3).
//
// Pendiente, NO resuelto todavia: "data.pointOfServiceType" y
// "data.centerType" son CODIGOS CORTOS ('1', 'CL', 'NO', 'AE', 'AG', 'BE',
// 'BP', 'CD', 'GN', 'MA', 'OP', 'ZS'), no texto descriptivo como "Cámara"
// -- todavia no se encontro la tabla que traduce estos codigos a su
// significado de negocio (no esta en el Excel EntidadesOTC -- ni MDMList ni
// ComparativoCRM-MDM la tienen -- ni comentada en el modelo Go).
package mongostore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/store"
)

// pipelineBatchSize es el tamaño de lote que el driver pide a Mongo en cada
// "getMore" al iterar el cursor de un pipeline -- ver la nota de TAvo
// (2026-09-11) de leer los resultados paginados para el caso de
// "guardarEnArchivo" en vez de traer todo de una. No expuesto como variable
// de entorno a proposito -- es un detalle de bajo nivel, no algo que haga
// falta ajustar por instalacion; si en el futuro hace falta, es un cambio
// de una linea.
const pipelineBatchSize = 500

// collectionNames mapea cada tipo a su coleccion Mongo. Vive en domain
// (domain.CollectionName) para que dictionary.Hierarchy() pueda exponer el
// mismo mapeo al subagente (necesita los nombres reales de coleccion para
// armar $lookup en un pipeline) sin duplicar la tabla. Ver advertencia del
// paquete sobre por que "serviceAddress" en particular es un supuesto, no
// una confirmacion contra UAT.
var collectionNames = domain.CollectionName

var allTypes = domain.AllTypes

// prefixToType es el inverso de domain.MDMPrefix -- para poder adivinar el
// tipo de un documento a partir del prefijo de su propio ucode (posiciones
// 2-3, justo despues del codigo de pais de 2 letras) sin tener que
// consultar las 5 colecciones.
var prefixToType = func() map[string]domain.EntityType {
	m := map[string]domain.EntityType{}
	for t, p := range domain.MDMPrefix {
		m[p] = t
	}
	return m
}()

// mongoDoc es la forma esperada de un documento en cualquiera de las 6
// colecciones. "Data" guarda los campos propios del tipo (centerType,
// pointOfServiceType, taxpayerId, nombres, etc.) como un mapa generico --
// igual que domain.Document.Attributes.
//
// NO hay un campo "name" a nivel raiz -- CONFIRMADO CONTRA UAT (2026-09-11,
// via reporting con un $limit sin filtro sobre cliente/centro/punto/
// grupo/holding/direccion) Y CONTRA EL MODELO GO REAL DE CADA TIPO (ver
// arriba) que el nombre de negocio vive ADENTRO de "data", con distinto
// campo segun el tipo y sin una unica fuente confiable:
//   - cliente/centro: data.customerName / data.legalName /
//     data.accountDescription tienen el nombre completo. data.accountName
//     tambien existe pero puede venir TRUNCADO a ~20 caracteres (ej.
//     "CAIXA CENTRAL DE CRE" en vez de "CAIXA CENTRAL DE CREDITO AGRICOLA
//     MUTUO") -- no usarlo como unica fuente. cliente/centro NO tienen
//     longName/shortName (no existen en su model.go, a diferencia de punto/
//     grupo/holding).
//   - punto: data.longName / data.customerName / data.accountName, los tres
//     confirmados en servicePoint_standard/model.go (a veces tambien
//     truncados en la muestra real -- sin confirmar cual queda siempre
//     completo para este tipo puntual).
//   - grupo/holding: data.customerName / data.longName / data.shortName /
//     data.accountName / data.accountDescription -- los cinco existen en
//     serviceGroup_standard/model.go y serviceHolding_standard/model.go
//     (accountName SI existe para estos dos tipos, aunque en la muestra en
//     vivo que se reviso primero aparecia vacio -- no confundir "vacio en
//     esa muestra" con "no existe en el modelo").
//   - direccion: NO tiene ningun campo de nombre de negocio -- se identifica
//     por sus componentes de domicilio (streetName, number, postalCode,
//     descriptionLevel1/2, level1/2/3/4), ver dictionary.go.
//
// Por eso mongoDoc no tiene un campo Name fijo: pickDisplayName elige, de
// los campos candidatos que existan en Data, el mas largo no vacio (mejor
// heuristica disponible para evitar quedarse con una version truncada
// cuando hay una completa al lado).
type mongoDoc struct {
	UCode       string                 `bson:"ucode"`
	ParentUCode string                 `bson:"parentUCode,omitempty"`
	Data        map[string]interface{} `bson:"data,omitempty"`
}

// nameFieldCandidates son, en los datos reales, los campos de "data" que
// pueden contener el nombre de negocio de una entidad (no aplica a
// direccion, que no tiene ninguno de estos -- ver comentario de mongoDoc).
// Se usan tanto para elegir que mostrar como Name (pickDisplayName) como
// para buscar por nombre (FindByNameAndType) -- una sola lista, no dos
// mantenidas por separado.
var nameFieldCandidates = []string{
	"accountName", "customerName", "legalName", "longName", "shortName", "accountDescription",
}

// pickDisplayName elige, entre nameFieldCandidates, el valor string no
// vacio MAS LARGO presente en data -- ver el comentario de mongoDoc sobre
// por que ("accountName" puede venir truncado, y no hay una unica fuente
// confiable segun el tipo).
func pickDisplayName(data map[string]interface{}) string {
	best := ""
	for _, campo := range nameFieldCandidates {
		v, ok := data[campo]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if len(s) > len(best) {
			best = s
		}
	}
	return best
}

func (d mongoDoc) toDomain(t domain.EntityType) domain.Document {
	return domain.Document{
		UCode:       d.UCode,
		ParentUCode: d.ParentUCode,
		Type:        t,
		Name:        pickDisplayName(d.Data),
		Attributes:  d.Data,
	}
}

type MongoStore struct {
	db *mongo.Database
}

// New conecta contra uri (ej. "mongodb://localhost:27017") y la base
// dbName (ej. "hermes-local"). Hace ping antes de devolver, para fallar
// rapido si Mongo no esta arriba en vez de fallar recien en el primer
// query.
func New(ctx context.Context, uri, dbName string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar a Mongo (%s): %w", uri, err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("Mongo no responde en %s: %w", uri, err)
	}
	return &MongoStore{db: client.Database(dbName)}, nil
}

func (m *MongoStore) FindByNameAndType(ctx context.Context, needle string, t domain.EntityType) ([]domain.Document, error) {
	collName, ok := collectionNames[t]
	if !ok {
		return nil, fmt.Errorf("tipo de entidad desconocido: %s", t)
	}
	// $or sobre todos los campos de nombre candidatos (ver nameFieldCandidates
	// y el comentario de mongoDoc) -- direccion no tiene ninguno de estos
	// campos, asi que para ese tipo esta busqueda siempre da 0 resultados a
	// proposito (no es un error: direccion se identifica por domicilio, no
	// por nombre de negocio).
	or := make(bson.A, 0, len(nameFieldCandidates))
	for _, campo := range nameFieldCandidates {
		or = append(or, bson.M{"data." + campo: bson.M{"$regex": needle, "$options": "i"}})
	}
	filter := bson.M{"$or": or}
	cur, err := m.db.Collection(collName).Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("error buscando en %s: %w", collName, err)
	}
	defer cur.Close(ctx)

	var out []domain.Document
	for cur.Next(ctx) {
		var d mongoDoc
		if err := cur.Decode(&d); err != nil {
			return nil, fmt.Errorf("documento invalido en %s: %w", collName, err)
		}
		out = append(out, d.toDomain(t))
	}
	return out, cur.Err()
}

// FindChildren consulta las 5 colecciones porque, dado el anidamiento
// posible (ver nota en internal/graph/graph.go), un hijo de un centro
// podria ser otro centro o un punto -- no se puede asumir de antemano en
// que coleccion esta.
func (m *MongoStore) FindChildren(ctx context.Context, parentUCode string) ([]domain.Document, error) {
	var out []domain.Document
	for _, t := range allTypes {
		collName := collectionNames[t]
		cur, err := m.db.Collection(collName).Find(ctx, bson.M{"parentUCode": parentUCode})
		if err != nil {
			return nil, fmt.Errorf("error buscando hijos en %s: %w", collName, err)
		}
		for cur.Next(ctx) {
			var d mongoDoc
			if err := cur.Decode(&d); err != nil {
				cur.Close(ctx)
				return nil, fmt.Errorf("documento invalido en %s: %w", collName, err)
			}
			out = append(out, d.toDomain(t))
		}
		cerr := cur.Err()
		cur.Close(ctx)
		if cerr != nil {
			return nil, cerr
		}
	}
	return out, nil
}

func (m *MongoStore) Get(ctx context.Context, ucode string) (domain.Document, bool, error) {
	// Se intenta primero adivinar la coleccion por el prefijo del ucode
	// (mas rapido); si el prefijo no matchea ninguno conocido, se cae a
	// buscar en las 5 por las dudas.
	if len(ucode) >= 4 {
		prefix := ucode[2:4]
		if t, ok := prefixToType[prefix]; ok {
			doc, found, err := m.getFrom(ctx, collectionNames[t], t, ucode)
			if err != nil || found {
				return doc, found, err
			}
		}
	}
	for _, t := range allTypes {
		doc, found, err := m.getFrom(ctx, collectionNames[t], t, ucode)
		if err != nil {
			return domain.Document{}, false, err
		}
		if found {
			return doc, true, nil
		}
	}
	return domain.Document{}, false, nil
}

// RunPipeline implementa store.PipelineRunner -- ver esa interfaz para el
// contrato completo. Es un wrapper de RunPipelineStream que acumula todo en
// un slice; usar RunPipelineStream directamente cuando el resultado puede
// ser grande y se va a escribir a un archivo (ver internal/reporting.
// StreamWriter) -- no tiene sentido cargar todo aca para volver a iterarlo
// del otro lado.
func (m *MongoStore) RunPipeline(ctx context.Context, t domain.EntityType, pipeline []map[string]interface{}) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	err := m.RunPipelineStream(ctx, t, pipeline, func(doc map[string]interface{}) error {
		out = append(out, doc)
		return nil
	})
	return out, err
}

// RunPipelineStream implementa store.PipelineRunner -- ver esa interfaz
// para el contrato completo (incluido store.ErrStopStream). IMPORTANTE: el
// llamador (internal/tools) tiene que haber corrido store.ValidatePipeline
// ANTES de llegar aca; este metodo no vuelve a validar, solo ejecuta.
//
// Dos cosas para las que TAvo pidio explicitamente disenar esto
// (2026-09-11): (1) lectura paginada -- SetBatchSize controla cuantos
// documentos trae cada "getMore" del cursor, en vez de que el driver decida
// un tamaño de lote implicito; (2) timeout -- si ctx trae un deadline (ver
// internal/tools, se arma con context.WithTimeout usando el mismo valor de
// configuracion que el limite de pantalla), se lo pasamos tambien a Mongo
// via SetMaxTime como defensa en profundidad: aunque algo hiciera que el
// driver no respete la cancelacion del context de Go, el servidor de Mongo
// igual corta la operacion del lado de el.
//
// Decodifica via bson.M y despues rebota por JSON (MarshalExtJSON ->
// json.Unmarshal) en vez de decodificar directo a map[string]interface{}:
// el driver de Mongo, al decodificar un documento embebido dentro de un
// map[string]interface{} generico, arma bson.D/primitive.A anidados en vez
// de map[string]interface{}/[]interface{} nativos -- lo cual despues
// complica serializar a JSON en internal/reporting sin logica
// adicional. Pasar por ExtJSON normaliza todo a los tipos nativos de
// encoding/json de una sola vez, documento por documento (no de a lote), asi
// que el costo es proporcional a UN documento por vez, no al resultado
// completo.
func (m *MongoStore) RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []map[string]interface{}, onDoc func(map[string]interface{}) error) error {
	collName, ok := collectionNames[t]
	if !ok {
		return fmt.Errorf("tipo de entidad desconocido: %s", t)
	}

	mongoPipeline := make([]bson.M, len(pipeline))
	for i, stage := range pipeline {
		mongoPipeline[i] = bson.M(stage)
	}

	opts := options.Aggregate().SetBatchSize(pipelineBatchSize)
	if deadline, ok := ctx.Deadline(); ok {
		if restante := time.Until(deadline); restante > 0 {
			opts.SetMaxTime(restante)
		}
	}

	cur, err := m.db.Collection(collName).Aggregate(ctx, mongoPipeline, opts)
	if err != nil {
		return fmt.Errorf("error ejecutando pipeline sobre %s: %w", collName, err)
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		var raw bson.M
		if err := cur.Decode(&raw); err != nil {
			return fmt.Errorf("documento invalido en resultado de pipeline sobre %s: %w", collName, err)
		}
		extJSON, err := bson.MarshalExtJSON(raw, false, false)
		if err != nil {
			return fmt.Errorf("no se pudo serializar un documento del resultado: %w", err)
		}
		var doc map[string]interface{}
		if err := json.Unmarshal(extJSON, &doc); err != nil {
			return fmt.Errorf("no se pudo normalizar un documento del resultado: %w", err)
		}
		if err := onDoc(doc); err != nil {
			if errors.Is(err, store.ErrStopStream) {
				return nil // corte limpio a pedido del caller, no es una falla
			}
			return err
		}
	}
	if err := cur.Err(); err != nil {
		return fmt.Errorf("error leyendo resultado de pipeline sobre %s (¿se agoto el timeout?): %w", collName, err)
	}
	return nil
}

func (m *MongoStore) getFrom(ctx context.Context, collName string, t domain.EntityType, ucode string) (domain.Document, bool, error) {
	var d mongoDoc
	err := m.db.Collection(collName).FindOne(ctx, bson.M{"ucode": ucode}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return domain.Document{}, false, nil
	}
	if err != nil {
		return domain.Document{}, false, fmt.Errorf("error buscando %s en %s: %w", ucode, collName, err)
	}
	return d.toDomain(t), true, nil
}
