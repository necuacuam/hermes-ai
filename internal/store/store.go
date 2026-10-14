// Package store define como se ejecutan las consultas contra la base
// (PipelineRunner, implementado por mongostore) y las valida antes de
// ejecutarlas (pipeline_guard.go). reporting es el unico camino a los
// datos.
package store

import (
	"context"
	"encoding/json"
	"errors"

	"hermes-mcp/internal/domain"
)

// PipelineRunner ejecuta un pipeline de agregacion ya armado por el agente,
// arrancando en la coleccion de una entidad. El pipeline filtra por lo que
// haga falta; no hay parametros fijos. La implementa mongostore.
type PipelineRunner interface {
	// RunPipeline ejecuta el pipeline (lista de etapas de un solo operador,
	// por ejemplo {"$match": {...}}) en la coleccion de t y devuelve los
	// documentos como mapas genericos: el pipeline puede proyectar cualquier
	// forma. Quien llama tiene que validar antes el pipeline con
	// ValidatePipeline; RunPipeline no vuelve a validar.
	//
	// Cada etapa llega como texto JSON crudo y se ejecuta desde ese texto,
	// para conservar el orden de sus claves (ej. un $sort por varios campos).
	RunPipeline(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage) ([]map[string]interface{}, error)

	// RunPipelineStream es igual que RunPipeline, pero entrega los
	// documentos de a uno a onDoc en vez de acumularlos: un resultado que va
	// a archivo puede tener miles de documentos y no hace falta tenerlos
	// todos en memoria (ver reporting.StreamWriter). Quien llama tiene que
	// validar antes el pipeline.
	//
	// Si onDoc devuelve ErrStopStream, la iteracion termina sin error.
	// Cualquier otro error de onDoc corta la iteracion y se devuelve.
	RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage, onDoc func(map[string]interface{}) error) error
}

// ErrStopStream es el error que onDoc puede devolver para que
// RunPipelineStream termine la iteracion sin que cuente como una falla.
var ErrStopStream = errors.New("store: stream detenido a pedido del caller")
