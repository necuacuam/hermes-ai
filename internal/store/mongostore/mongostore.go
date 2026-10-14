// Package mongostore ejecuta los pipelines de reporting
// (store.PipelineRunner) contra un Mongo (local para probar, o UAT/PRO).
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
//     (ver diccionario_de_campos de cada tipo).
//   - "data" mezcla strings, booleanos (billingCenter, financialEntity,
//     merged) y arrays (agents, contacts).
//   - "direccion" (serviceAddress) tiene una forma totalmente distinta a
//     las otras 5: no tiene ningun campo de nombre de negocio ni control/
//     countryList -- ver dictionary.go para sus campos reales
//     (streetName, number, postalCode, descriptionLevel1/2, level1/2/3).
//
// Pendiente, NO resuelto todavia: "data.pointOfServiceType" y
// "data.centerType" son CODIGOS CORTOS, no texto descriptivo -- todavia no
// se encontro la tabla que traduce estos codigos a su significado de
// negocio (no esta en el Excel EntidadesOTC -- ni MDMList ni
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

// collectionNames mapea cada tipo a su coleccion Mongo. Vive en domain
// (domain.CollectionName) para que dictionary.Hierarchy() pueda exponer el
// mismo mapeo al subagente (necesita los nombres reales de coleccion para
// armar $lookup en un pipeline) sin duplicar la tabla. Ver advertencia del
// paquete sobre por que "serviceAddress" en particular es un supuesto, no
// una confirmacion contra UAT.
var collectionNames = domain.CollectionName

// MongoStore implementa store.PipelineRunner contra la base dbName.
type MongoStore struct {
	db *mongo.Database
}

// New conecta contra uri (ej. "mongodb://localhost:27017") y la base
// dbName (ej. "hermes-local"). Hace ping antes de devolver, para fallar
// rapido si Mongo no esta arriba en vez de fallar recien en la primera
// consulta.
func New(ctx context.Context, uri, dbName string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar a Mongo (%s): %w", URISinClave(uri), err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("Mongo no responde en %s: %w", URISinClave(uri), err)
	}
	return &MongoStore{db: client.Database(dbName)}, nil
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
