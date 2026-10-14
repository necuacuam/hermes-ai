// Package mongostore ejecuta los pipelines de reporting
// (store.PipelineRunner) contra MongoDB. La forma de los documentos (que
// campos estan en la raiz y cuales bajo "data") no se asume aqui: la describe
// el fichero de conocimiento (estructuraDeCampos).
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

// pipelineBatchSize es el tamaño de lote que se pide a Mongo en cada
// "getMore" del cursor: el resultado se lee por partes en vez de todo de una
// vez. No es configurable a proposito: es un detalle de bajo nivel.
const pipelineBatchSize = 500

// claveEnURI reconoce la contraseña de una URI de Mongo
// (mongodb://usuario:clave@host...).
var claveEnURI = regexp.MustCompile(`(://[^:/@]*:)[^@/]*@`)

// URISinClave devuelve la URI con la contraseña tapada, para registros y
// mensajes de error: Claude Desktop guarda en texto plano todo lo que el
// servidor escribe en su registro.
func URISinClave(uri string) string {
	return claveEnURI.ReplaceAllString(uri, "${1}****@")
}

// collectionNames mapea cada tipo a su coleccion. Es el registro
// (domain.CollectionName), compartido con relacion_entre_entidades para no
// duplicar la tabla.
var collectionNames = domain.CollectionName

// MongoStore implementa store.PipelineRunner contra la base dbName.
type MongoStore struct {
	db *mongo.Database
}

// New conecta contra uri y la base dbName. Hace ping antes de devolver,
// para fallar al arrancar si Mongo no responde, y no en la primera
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

// RunPipeline implementa store.PipelineRunner: acumula en memoria lo que
// entrega RunPipelineStream. Para resultados grandes que van a archivo,
// usar RunPipelineStream directamente.
func (m *MongoStore) RunPipeline(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	err := m.RunPipelineStream(ctx, t, pipeline, func(doc map[string]interface{}) error {
		out = append(out, doc)
		return nil
	})
	return out, err
}

// pipelineDesdeExtJSON convierte cada etapa a BSON leyendola como Extended
// JSON. Asi un pipeline puede usar tipos que el JSON plano no tiene
// ({"$uuid": "..."}, {"$binary": {...}}, {"$date": "..."}, {"$oid": "..."})
// y llegan a Mongo como UUID, binario, fecha u ObjectId reales; por ejemplo,
// para filtrar salesPerson por uuid a partir de un agentId. Cada etapa se
// convierte desde su texto original a bson.D, que conserva el orden de las
// claves: un $sort por varios campos llega en el orden en que se escribio.
func pipelineDesdeExtJSON(pipeline []json.RawMessage) ([]bson.D, error) {
	out := make([]bson.D, len(pipeline))
	for i, raw := range pipeline {
		if err := bson.UnmarshalExtJSON(raw, false, &out[i]); err != nil {
			return nil, fmt.Errorf("etapa %d: Extended JSON invalido (revisar $uuid/$binary/$date/$oid): %w", i, err)
		}
	}
	return out, nil
}

// RunPipelineStream implementa store.PipelineRunner. No valida el pipeline:
// quien llama tiene que haber corrido store.ValidatePipeline antes.
//
//   - Lee el cursor por lotes (pipelineBatchSize).
//   - Si ctx tiene un plazo, se lo pasa tambien a Mongo (maxTimeMS): aunque el
//     driver no respetara la cancelacion, el servidor de Mongo corta la
//     consulta.
//   - Cada documento se decodifica a bson.M y se normaliza pasando por
//     Extended JSON, para que los documentos anidados queden como mapas y
//     listas nativos de Go (el driver los dejaria como bson.D/primitive.A). Se
//     hace documento por documento, asi que el costo no depende del tamaño del
//     resultado.
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
				return nil // corte pedido por quien llama: no es una falla
			}
			return err
		}
	}
	if err := cur.Err(); err != nil {
		return fmt.Errorf("error leyendo resultado de pipeline sobre %s (¿se agoto el timeout?): %w", collName, err)
	}
	return nil
}
