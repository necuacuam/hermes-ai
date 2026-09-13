// Package store define la interfaz que separa la logica de negocio (grafo,
// reporting) de donde viven los datos realmente. Hoy la unica
// implementacion es un mock en memoria (mockstore) porque todavia no hay
// acceso a Mongo de UAT. Cuando llegue el acceso, la idea es escribir una
// implementacion mongostore.New(uri) que satisfaga esta misma interfaz --
// el resto del codigo (graph, reporting, los tools) no deberia necesitar
// ningun cambio.
package store

import (
	"context"
	"errors"

	"hermes-mcp/internal/domain"
)

type Store interface {
	// FindByNameAndType busca documentos de un tipo dado cuyo Name
	// contenga needle (sin distinguir mayusculas). En Mongo real esto
	// seria un $regex case-insensitive (o mejor, un indice de texto) sobre
	// el campo de nombre de negocio (longName/accountName/customerName
	// segun la entidad).
	FindByNameAndType(ctx context.Context, needle string, t domain.EntityType) ([]domain.Document, error)

	// FindChildren devuelve los documentos cuyo ParentUCode == parentUCode,
	// de cualquier tipo. En Mongo real: find({parentUCode: X}) en la
	// coleccion correspondiente -- OJO que en Mongo real cada tipo tiene su
	// propia coleccion, asi que la implementacion real tiene que consultar
	// varias colecciones (o mantener un indice invertido); el mock, al
	// tener todo en una sola lista en memoria, no tiene ese problema.
	FindChildren(ctx context.Context, parentUCode string) ([]domain.Document, error)

	// Get busca un documento puntual por ucode. Devuelve found=false si no
	// existe.
	Get(ctx context.Context, ucode string) (doc domain.Document, found bool, err error)
}

// PipelineRunner es una capacidad OPCIONAL y separada de Store: ejecuta un
// pipeline de agregacion Mongo ya armado por el subagente (con ayuda de
// "diccionario_de_campos" y "relacion_entre_entidades"), arrancando en una
// coleccion dada. Existe aparte de Store porque conceptualmente es otra
// cosa -- Store resuelve/camina la jerarquia de entidades con metodos
// fijos y acotados; PipelineRunner ejecuta lo que el subagente decida, dato
// que se acordo explicitamente con TAvo (2026-09-11): la tool de reporting
// ya no exige un rootUcode resuelto de antemano, el pipeline puede filtrar
// por lo que haga falta directamente.
//
// SOLO mongostore la implementa -- mockstore no, porque interpretar un
// subconjunto de la sintaxis de agregacion de Mongo sobre structs Go en
// memoria no vale la pena para este alcance (los tests de "reporting" que
// necesiten un pipeline real corren contra un Mongo de prueba, no contra
// mockstore). El caller (ver internal/tools) debe hacer un type assertion
// contra esta interfaz y devolver un error claro si el Store activo no la
// implementa.
type PipelineRunner interface {
	// RunPipeline ejecuta pipeline (lista de etapas, cada una un mapa de un
	// solo operador, ej. {"$match": {...}}) arrancando en la coleccion
	// Mongo que corresponda a t (ver domain.CollectionName). Devuelve los
	// documentos resultantes como mapas genericos (bson.M serializado a
	// JSON), no como domain.Document -- el pipeline puede proyectar
	// cualquier forma de salida, no necesariamente la forma fija de una
	// entidad. El caller es responsable de validar el pipeline con
	// ValidatePipeline ANTES de llamar a este metodo -- RunPipeline no
	// vuelve a validar.
	RunPipeline(ctx context.Context, t domain.EntityType, pipeline []map[string]interface{}) ([]map[string]interface{}, error)

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
	RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []map[string]interface{}, onDoc func(map[string]interface{}) error) error
}

// ErrStopStream es el error centinela que onDoc puede devolver para pedirle
// a RunPipelineStream que corte la iteracion sin que eso cuente como una
// falla real -- ver la nota en PipelineRunner.
var ErrStopStream = errors.New("store: stream detenido a pedido del caller")
