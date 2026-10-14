// Package store define como se ejecutan las consultas contra la base
// (PipelineRunner, implementado por mongostore) y las valida antes de
// ejecutarlas (pipeline_guard.go). reporting es el unico camino a los datos
// (2026-10-07: se quito la tool seek, y con ella la interfaz Store de
// busqueda por nombre/hijos/ucode y su implementacion en memoria).
package store

import (
	"context"
	"encoding/json"
	"errors"

	"hermes-mcp/internal/domain"
)

// PipelineRunner ejecuta un pipeline de agregacion Mongo ya armado por el
// agente (con ayuda de "diccionario_de_campos" y
// "relacion_entre_entidades"), arrancando en una coleccion dada -- ver la
// decision del 2026-09-11: la tool de reporting no exige un rootUcode
// resuelto de antemano, el pipeline puede filtrar por lo que haga falta
// directamente. La implementa mongostore.
type PipelineRunner interface {
	// RunPipeline ejecuta pipeline (lista de etapas, cada una un mapa de un
	// solo operador, ej. {"$match": {...}}) arrancando en la coleccion
	// Mongo que corresponda a t (ver domain.CollectionName). Devuelve los
	// documentos resultantes como mapas genericos (bson.M serializado a
	// JSON) -- el pipeline puede proyectar
	// cualquier forma de salida, no necesariamente la forma fija de una
	// entidad. El caller es responsable de validar el pipeline con
	// ValidatePipeline ANTES de llamar a este metodo -- RunPipeline no
	// vuelve a validar.
	//
	// Cada etapa llega como texto JSON crudo y se ejecuta desde ese texto,
	// para conservar el orden de sus claves (ej. un $sort por varios campos).
	RunPipeline(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage) ([]map[string]interface{}, error)

	// RunPipelineStream es igual que RunPipeline pero entrega los
	// documentos DE A UNO via onDoc, en vez de acumular el resultado
	// completo en un slice antes de devolverlo -- pensado para el caso de
	// "guardarEnArchivo" (ver internal/reporting.StreamWriter): un pipeline
	// sin $limit puede devolver miles de documentos, y no hay motivo para
	// tenerlos todos en memoria de este lado si van a escribirse a un
	// archivo a medida que llegan. Igual que RunPipeline, el caller tiene
	// que validar el pipeline con ValidatePipeline ANTES de llamar a esto.
	//
	// Si onDoc devuelve ErrStopStream, la iteracion corta LIMPIO (sin
	// propagar error) -- para cuando el caller decide, por su cuenta, que
	// ya tiene lo que necesita y no hace falta seguir leyendo el cursor.
	// Cualquier otro error de onDoc se propaga tal cual y corta la
	// iteracion.
	RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage, onDoc func(map[string]interface{}) error) error
}

// ErrStopStream es el error centinela que onDoc puede devolver para pedirle
// a RunPipelineStream que corte la iteracion sin que eso cuente como una
// falla real -- ver la nota en PipelineRunner.
var ErrStopStream = errors.New("store: stream detenido a pedido del caller")
