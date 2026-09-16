// Guardrail de seguridad para pipelines de agregacion Mongo armados por un
// LLM (el subagente) en vez de por codigo fijo -- ver la decision del
// 2026-09-11 (conversacion con TAvo) de que "reporting" ya no acepta solo
// parametros estructurados sino un pipeline mas libre. Eso agranda la
// superficie de lo que se puede ejecutar contra la base real del jefe (que
// "no se toca en ningun momento"), asi que antes de correr CUALQUIER
// pipeline se valida contra dos reglas:
//
//  1. Lista blanca de etapas de SOLO LECTURA -- cualquier etapa que no este
//     en esta lista se rechaza de entrada. En particular $out y $merge
//     (que ESCRIBEN en una coleccion) estan explicitamente prohibidas, sea
//     cual sea su posicion en el pipeline.
//  2. Barrido recursivo de claves prohibidas en TODO el arbol del pipeline
//     (no solo el nombre de la etapa) -- operadores que ejecutan JS
//     arbitrario o inspeccionan el servidor ($where, $function,
//     $accumulator, $currentOp, etc.) se rechazan aunque aparezcan
//     anidados dentro de una etapa por lo demas permitida (ej. un $match
//     con un $where adentro).
//
// LIMITACION CONOCIDA (documentar para quien revise seguridad antes de
// escalar mas alla de esta v1 local): esto es una lista blanca por NOMBRE
// de etapa/operador, no un parser completo de la gramatica de agregacion
// de Mongo -- no se audita, por ejemplo, cuanto trabajo puede pedir un
// $lookup sin indice o un $graphLookup sin maxDepth, ni hay timeout
// (maxTimeMS) sobre la ejecucion. Para el uso previsto (un unico usuario
// local contra su propia base ya restaurada) se considera un compromiso
// razonable; si este servidor se expone a mas usuarios o a una base
// compartida, esto hay que revisarlo primero.
//
// TERCERA REGLA (agregada en la revision del 2026-09-11, autocritica antes
// de entregar): coleccionInicial ya limitaba donde EMPIEZA el pipeline a
// las 6 entidades tipo cuenta, pero nada impedia que un $lookup o
// $graphLookup dentro del pipeline apuntara con su "from" a CUALQUIER OTRA
// coleccion de la misma base Mongo -- incluyendo colecciones fuera del
// alcance acordado (contract, serviceOrder, activity, etc., o cualquier
// coleccion de sistema). Eso hubiera dejado un pipeline "leer" datos fuera
// del alcance sin que ValidatePipeline lo notara, porque solo miraba
// nombres de etapa/operador, nunca el VALOR de "from". Se agrega
// validarColeccionDestino para cerrar ese agujero: todo "from" de $lookup/
// $graphLookup tiene que ser una de las 6 colecciones conocidas
// (domain.CollectionName) o una de las colecciones auxiliares agregadas
// explicitamente (ver coleccionesAuxiliaresPermitidas).
//
// EXCEPCION PUNTUAL (2026-09-15, decision de TAvo): se habilita "from":
// "operativeLevel" -- el catalogo auxiliar de clase/subclase de punto
// (ver dictionary.go, FieldInfo de punto, y grafo-entidades-hermes.md).
// NO se la agrega a domain.CollectionName/EntityType a proposito: sigue
// sin ser una entidad tipo cuenta, no tiene ucode/parentUCode, y NO puede
// usarse como coleccionInicial de un pipeline (eso la convertiria en una
// 7ma entidad completa, que es justo lo que se decidio no hacer) -- solo
// se permite como destino de un $lookup/$graphLookup de solo lectura
// desde alguna de las 6 colecciones reales, tipicamente servicePoint.
package store

import (
	"fmt"

	"hermes-mcp/internal/domain"
)

// coleccionesAuxiliaresPermitidas son colecciones que NO son entidades tipo
// cuenta (no tienen ucode/parentUCode, no pueden ser coleccionInicial) pero
// que se permite consultar como destino de un $lookup/$graphLookup de solo
// lectura -- catalogos de referencia externos al grafo de las 6 entidades.
// Ver la nota fechada 2026-09-15 en el comentario del paquete.
var coleccionesAuxiliaresPermitidas = map[string]bool{
	"operativeLevel": true,
}

// coleccionesPermitidas son los unicos nombres de coleccion Mongo a los que
// un pipeline puede referirse como "from" de $lookup/$graphLookup --
// las 6 entidades tipo cuenta del alcance de esta v1 mas las colecciones
// auxiliares agregadas explicitamente (hoy, solo operativeLevel).
var coleccionesPermitidas = func() map[string]bool {
	m := map[string]bool{}
	for _, coll := range domain.CollectionName {
		m[coll] = true
	}
	for coll := range coleccionesAuxiliaresPermitidas {
		m[coll] = true
	}
	return m
}()

// allowedStages son las etapas de agregacion que este proyecto permite
// ejecutar. Todas son de lectura -- ninguna escribe en la base ni ejecuta
// JS arbitrario del lado del servidor.
var allowedStages = map[string]bool{
	"$match":        true,
	"$project":      true,
	"$addFields":    true,
	"$set":          true,
	"$unset":        true,
	"$sort":         true,
	"$limit":        true,
	"$skip":         true,
	"$count":        true,
	"$group":        true,
	"$unwind":       true,
	"$lookup":       true,
	"$facet":        true,
	"$replaceRoot":  true,
	"$replaceWith":  true,
	"$graphLookup":  true,
	"$sample":       true,
	"$bucket":       true,
	"$bucketAuto":   true,
	"$sortByCount":  true,
}

// forbiddenKeys son operadores/comandos prohibidos en CUALQUIER posicion
// del pipeline, aunque esten anidados dentro de una etapa permitida --
// ejecutan JS arbitrario, escriben datos, o exponen estado del servidor
// que no tiene nada que ver con las 6 entidades de este proyecto.
var forbiddenKeys = map[string]bool{
	"$out":              true,
	"$merge":            true,
	"$where":            true,
	"$function":         true,
	"$accumulator":      true,
	"$currentOp":        true,
	"$indexStats":       true,
	"$collStats":        true,
	"$planCacheStats":   true,
	"$listSessions":     true,
	"$listLocalSessions": true,
	"$eval":             true,
}

// ValidatePipeline aplica las dos reglas del comentario del paquete. Se
// llama SIEMPRE antes de PipelineRunner.RunPipeline -- ver esa interfaz.
func ValidatePipeline(pipeline []map[string]interface{}) error {
	if len(pipeline) == 0 {
		return fmt.Errorf("el pipeline esta vacio")
	}
	for i, stage := range pipeline {
		if len(stage) != 1 {
			return fmt.Errorf("etapa %d invalida: se esperaba un unico operador de etapa (ej. {\"$match\": {...}}), se recibieron %d claves", i, len(stage))
		}
		for name, body := range stage {
			if !allowedStages[name] {
				return fmt.Errorf("etapa %d no permitida: %q -- solo se permiten etapas de lectura (ver store.allowedStages)", i, name)
			}
			if name == "$lookup" || name == "$graphLookup" {
				if err := validarColeccionDestino(body); err != nil {
					return fmt.Errorf("etapa %d (%s): %w", i, name, err)
				}
			}
		}
		if err := scanForbidden(stage); err != nil {
			return fmt.Errorf("etapa %d: %w", i, err)
		}
	}
	return nil
}

// validarColeccionDestino comprueba que el "from" de un $lookup/
// $graphLookup sea una de las 6 colecciones del alcance -- ver la nota del
// paquete sobre por que esto hacia falta ademas de la lista blanca de
// etapas. Si "from" no viene o no es un string, se deja pasar sin error
// (Mongo lo va a rechazar igual al ejecutar; no es este validador el que
// tiene que duplicar esa validacion de forma).
func validarColeccionDestino(stageBody interface{}) error {
	body, ok := stageBody.(map[string]interface{})
	if !ok {
		return nil
	}
	from, ok := body["from"]
	if !ok {
		return nil
	}
	fromStr, ok := from.(string)
	if !ok {
		return nil
	}
	if !coleccionesPermitidas[fromStr] {
		return fmt.Errorf("\"from\": %q no es una de las colecciones permitidas -- solo se puede hacer $lookup/$graphLookup hacia las 6 entidades tipo cuenta (serviceHolding, serviceGroup, serviceEntity, servicePlace, servicePoint, serviceAddress) o hacia una coleccion auxiliar habilitada explicitamente (operativeLevel)", fromStr)
	}
	return nil
}

// scanForbidden recorre recursivamente cualquier valor (mapa, slice, o
// escalar) buscando alguna de las claves de forbiddenKeys en cualquier
// nivel de anidamiento.
func scanForbidden(v interface{}) error {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, inner := range val {
			if forbiddenKeys[k] {
				return fmt.Errorf("operador no permitido: %q", k)
			}
			if err := scanForbidden(inner); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, inner := range val {
			if err := scanForbidden(inner); err != nil {
				return err
			}
		}
	case []map[string]interface{}:
		for _, inner := range val {
			if err := scanForbidden(inner); err != nil {
				return err
			}
		}
	}
	return nil
}
