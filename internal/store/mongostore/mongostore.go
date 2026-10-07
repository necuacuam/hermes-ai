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
//     y sin una unica fuente confiable (ver dictionary.RelationsData.
//     CamposDeNombre / pickDisplayName mas abajo para el detalle).
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
	"regexp"
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

// claveEnURI reconoce la contraseña de una URI de Mongo
// (mongodb://usuario:clave@host...).
var claveEnURI = regexp.MustCompile(`(://[^:/@]*:)[^@/]*@`)

// URISinClave devuelve la URI con la contraseña tapada, para logs y
// mensajes de error (2026-10-06): Claude Desktop guarda en texto plano todo
// lo que el servidor escribe en su log.
func URISinClave(uri string) string {
	return claveEnURI.ReplaceAllString(uri, "${1}****@")
}

// tiempoRestante devuelve cuanto le queda al deadline de ctx, para pasarlo
// a Mongo como maxTimeMS: asi el servidor corta la consulta de su lado
// aunque el driver no respete la cancelacion del context.
func tiempoRestante(ctx context.Context) (time.Duration, bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	restante := time.Until(deadline)
	return restante, restante > 0
}

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

// camposDeNombre YA NO vive hardcodeado aca (ver comentario de
// RelationsData.CamposDeNombre en dictionary.go -- movido 2026-09-28,
// contenido de negocio curado a mano que no tiene que exigir recompilar el
// binario para cambiar). MongoStore lo recibe por tipo en New() y lo usa
// tanto para elegir que mostrar como Name (pickDisplayName) como para
// buscar por nombre (FindByNameAndType).

// pickDisplayName elige, entre candidatos, el valor string no vacio MAS
// LARGO presente en data -- ver el comentario de mongoDoc sobre por que
// ("accountName" puede venir truncado, y no hay una unica fuente confiable
// segun el tipo).
func pickDisplayName(data map[string]interface{}, candidatos []string) string {
	best := ""
	for _, campo := range candidatos {
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

func (d mongoDoc) toDomain(t domain.EntityType, camposDeNombre []string) domain.Document {
	return domain.Document{
		UCode:       d.UCode,
		ParentUCode: d.ParentUCode,
		Type:        t,
		Name:        pickDisplayName(d.Data, camposDeNombre),
		Attributes:  d.Data,
	}
}

type MongoStore struct {
	db             *mongo.Database
	camposDeNombre map[domain.EntityType][]string
	limiteSeek     int // ver SetLimiteSeek
}

// SetLimiteSeek fija cuantos candidatos trae como maximo FindByNameAndType
// (HERMES_MCP_LIMITE_SEEK). <= 0 usa store.LimiteSeekPorDefecto.
func (m *MongoStore) SetLimiteSeek(n int) {
	m.limiteSeek = n
}

// New conecta contra uri (ej. "mongodb://localhost:27017") y la base
// dbName (ej. "hermes-local"). Hace ping antes de devolver, para fallar
// rapido si Mongo no esta arriba en vez de fallar recien en el primer
// query. camposDeNombre viene del fichero de relaciones externo (ver
// dictionary.RelationsData.CamposDeNombre) -- por eso en main.go la carga
// de ese fichero ahora pasa ANTES de construir el MongoStore.
func New(ctx context.Context, uri, dbName string, camposDeNombre map[domain.EntityType][]string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar a Mongo (%s): %w", URISinClave(uri), err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("Mongo no responde en %s: %w", URISinClave(uri), err)
	}
	return &MongoStore{db: client.Database(dbName), camposDeNombre: camposDeNombre}, nil
}

func (m *MongoStore) FindByNameAndType(ctx context.Context, needle string, t domain.EntityType) ([]domain.Document, error) {
	collName, ok := collectionNames[t]
	if !ok {
		return nil, fmt.Errorf("tipo de entidad desconocido: %s", t)
	}
	candidatos := m.camposDeNombre[t]
	if len(candidatos) == 0 {
		// Sin campos de nombre configurados para este tipo (ej. direccion,
		// que se identifica por domicilio, no por nombre de negocio) -- 0
		// resultados a proposito, sin pegarle a Mongo con un $or vacio.
		return nil, nil
	}
	// $or sobre todos los campos de nombre candidatos de este tipo (ver
	// dictionary.RelationsData.CamposDeNombre y el comentario de mongoDoc).
	// needle es una expresion regular (PCRE de Mongo, sin distinguir
	// mayusculas): el agente puede buscar "^IKEA" o "PETROGAL|GALP". Un
	// patron que coincide con todo lo acota el tope de candidatos, y uno
	// lento, el tiempo maximo (2026-10-06).
	or := make(bson.A, 0, len(candidatos))
	for _, campo := range candidatos {
		or = append(or, bson.M{"data." + campo: bson.M{"$regex": needle, "$options": "i"}})
	}
	filter := bson.M{"$or": or}
	// Uno mas que el tope, para que quien llama sepa que hubo mas.
	limite := m.limiteSeek
	if limite <= 0 {
		limite = store.LimiteSeekPorDefecto
	}
	opts := options.Find().SetLimit(int64(limite + 1))
	if restante, ok := tiempoRestante(ctx); ok {
		opts.SetMaxTime(restante)
	}
	cur, err := m.db.Collection(collName).Find(ctx, filter, opts)
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
		out = append(out, d.toDomain(t, candidatos))
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
		opts := options.Find()
		if restante, ok := tiempoRestante(ctx); ok {
			opts.SetMaxTime(restante)
		}
		cur, err := m.db.Collection(collName).Find(ctx, bson.M{"parentUCode": parentUCode}, opts)
		if err != nil {
			return nil, fmt.Errorf("error buscando hijos en %s: %w", collName, err)
		}
		for cur.Next(ctx) {
			var d mongoDoc
			if err := cur.Decode(&d); err != nil {
				cur.Close(ctx)
				return nil, fmt.Errorf("documento invalido en %s: %w", collName, err)
			}
			out = append(out, d.toDomain(t, m.camposDeNombre[t]))
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
func (m *MongoStore) RunPipeline(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage) ([]map[string]interface{}, error) {
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
// pipelineDesdeExtJSON convierte cada etapa a BSON leyéndola como Extended
// JSON (2026-10-03, pedido de TAvo), en vez de pasar el map tal cual. Así
// un pipeline puede usar tipos que JSON plano no tiene -- {"$uuid": "..."},
// {"$binary": {...}}, {"$date": "..."}, {"$oid": "..."} -- y llegan a Mongo
// como UUID/binario/fecha/ObjectId reales. Antes llegaban como un
// sub-documento con una clave "$uuid" y Mongo los rechazaba. Caso que lo
// motivó: cruzar cliente.data.agentId (string) con salesPerson.uuid
// (binario) -- con esto se puede filtrar salesPerson por
// {"uuid": {"$in": [{"$uuid": "<agentId>"}]}} usando el agentId tal cual.
// Un pipeline sin estos tipos se convierte igual que antes.
//
// Cada etapa se convierte desde su texto original a bson.D, que conserva el
// orden de las claves (2026-10-03): antes pasaba por un map y un $sort por
// varios campos llegaba a Mongo en orden alfabetico, sin error.
func pipelineDesdeExtJSON(pipeline []json.RawMessage) ([]bson.D, error) {
	out := make([]bson.D, len(pipeline))
	for i, raw := range pipeline {
		if err := bson.UnmarshalExtJSON(raw, false, &out[i]); err != nil {
			return nil, fmt.Errorf("etapa %d: Extended JSON invalido (revisar $uuid/$binary/$date/$oid): %w", i, err)
		}
	}
	return out, nil
}

func (m *MongoStore) RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage, onDoc func(map[string]interface{}) error) error {
	collName, ok := collectionNames[t]
	if !ok {
		return fmt.Errorf("tipo de entidad desconocido: %s", t)
	}

	mongoPipeline, err := pipelineDesdeExtJSON(pipeline)
	if err != nil {
		return err
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
	opts := options.FindOne()
	if restante, ok := tiempoRestante(ctx); ok {
		opts.SetMaxTime(restante)
	}
	err := m.db.Collection(collName).FindOne(ctx, bson.M{"ucode": ucode}, opts).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return domain.Document{}, false, nil
	}
	if err != nil {
		return domain.Document{}, false, fmt.Errorf("error buscando %s en %s: %w", ucode, collName, err)
	}
	return d.toDomain(t, m.camposDeNombre[t]), true, nil
}
