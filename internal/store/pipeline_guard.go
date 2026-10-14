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
// $lookup sin indice o un $graphLookup sin maxDepth. Lo que lo acota es el
// tope de tiempo (HERMES_MCP_MONGO_TIMEOUT), que tambien se le pasa a
// Mongo como maxTimeMS (ver mongostore.RunPipelineStream). Para el uso previsto (un unico usuario
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
//
// QUITADOS (2026-10-03, decision de TAvo): dos chequeos agregados el
// 2026-09-25 por errores de un LLM mas debil (OpenAI en Open WebUI): el que
// exigia el prefijo "data." en los campos del primer $match y el de un
// espacio antes del "$" en un operador. Ese LLM ya no se usa, y el primero
// bloqueaba campos reales de nivel raiz (uuid, countryCode, revision).
// Las reglas de seguridad de arriba se mantienen: no dependen del LLM.
//
// SUB-PIPELINES (2026-10-06): la lista blanca de etapas y el control de
// "from" se aplicaban solo al primer nivel; dentro de un $facet o del
// "pipeline" de un $lookup pasaba un $lookup o un $unionWith hacia
// cualquier coleccion de la base. Ahora los sub-pipelines se validan con
// las mismas reglas que el pipeline principal (validarSubPipelines): una
// etapa que no esta en allowedStages, como $unionWith, se rechaza en
// cualquier nivel.
//
// CLAVES REPETIDAS (2026-10-06): ValidatePipeline revisa cada etapa
// decodificada a map, donde una clave repetida queda con su ultimo valor,
// pero Mongo ejecuta el texto original con las dos. Con
// {"from": "<fuera de alcance>", "from": "servicePlace"} el validador veia
// solo servicePlace. ValidarClavesUnicas rechaza esos pipelines antes de
// validar, para que lo validado sea lo mismo que se ejecuta.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"hermes-mcp/internal/domain"
)

// coleccionesAuxiliaresPermitidas son las tablas auxiliares del registro
// (domain.TablasAuxiliares) -- ver la nota fechada 2026-09-15 en el
// comentario del paquete.
var coleccionesAuxiliaresPermitidas = func() map[string]bool {
	m := map[string]bool{}
	for _, coll := range domain.TablasAuxiliares {
		m[coll] = true
	}
	return m
}()

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

// coleccionesDeEntidadesTexto y coleccionesAuxiliaresTexto arman, desde el
// registro, la lista que se muestra en el error de "from" no permitido
// (2026-10-14: antes estaba escrita a mano en el mensaje).
var coleccionesDeEntidadesTexto = func() string {
	nombres := make([]string, len(domain.AllTypes))
	for i, t := range domain.AllTypes {
		nombres[i] = domain.CollectionName[t]
	}
	return strings.Join(nombres, ", ")
}()

var coleccionesAuxiliaresTexto = func() string {
	nombres := make([]string, 0, len(coleccionesAuxiliaresPermitidas))
	for coll := range coleccionesAuxiliaresPermitidas {
		nombres = append(nombres, coll)
	}
	sort.Strings(nombres)
	return strings.Join(nombres, ", ")
}()

// allowedStages son las etapas de agregacion que este proyecto permite
// ejecutar. Todas son de lectura -- ninguna escribe en la base ni ejecuta
// JS arbitrario del lado del servidor. $skip no esta (2026-10-07): solo
// sirve para paginar, y el agente no puede juntar mas filas de las que
// entran en pantalla pidiendolas por partes.
var allowedStages = map[string]bool{
	"$match":       true,
	"$project":     true,
	"$addFields":   true,
	"$set":         true,
	"$unset":       true,
	"$sort":        true,
	"$limit":       true,
	"$count":       true,
	"$group":       true,
	"$unwind":      true,
	"$lookup":      true,
	"$facet":       true,
	"$replaceRoot": true,
	"$replaceWith": true,
	"$graphLookup": true,
	"$sample":      true,
	"$bucket":      true,
	"$bucketAuto":  true,
	"$sortByCount": true,
}

// forbiddenKeys son operadores/comandos prohibidos en CUALQUIER posicion
// del pipeline, aunque esten anidados dentro de una etapa permitida --
// ejecutan JS arbitrario, escriben datos, o exponen estado del servidor
// que no tiene nada que ver con las 6 entidades de este proyecto.
var forbiddenKeys = map[string]bool{
	"$out":               true,
	"$merge":             true,
	"$where":             true,
	"$function":          true,
	"$accumulator":       true,
	"$currentOp":         true,
	"$indexStats":        true,
	"$collStats":         true,
	"$planCacheStats":    true,
	"$listSessions":      true,
	"$listLocalSessions": true,
	"$eval":              true,
}

// ValidatePipeline aplica las reglas del comentario del paquete. Se llama
// SIEMPRE antes de PipelineRunner.RunPipeline -- ver esa interfaz.
func ValidatePipeline(pipeline []map[string]interface{}) error {
	if len(pipeline) == 0 {
		return fmt.Errorf("el pipeline esta vacio")
	}
	etapas := make([]interface{}, len(pipeline))
	for i, stage := range pipeline {
		etapas[i] = stage
	}
	return validarEtapas(etapas, "")
}

// validarEtapas aplica las reglas a una lista de etapas: el pipeline
// principal o un sub-pipeline (ver validarSubPipeline). ruta antepone en los
// errores donde esta la etapa dentro del pipeline principal.
func validarEtapas(etapas []interface{}, ruta string) error {
	for i, e := range etapas {
		stage, ok := e.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%setapa %d invalida: se esperaba un objeto con un unico operador de etapa", ruta, i)
		}
		if len(stage) != 1 {
			return fmt.Errorf("%setapa %d invalida: se esperaba un unico operador de etapa (ej. {\"$match\": {...}}), se recibieron %d claves", ruta, i, len(stage))
		}
		for name, body := range stage {
			if !allowedStages[name] {
				return fmt.Errorf("%setapa %d no permitida: %q -- solo se permiten etapas de lectura (ver store.allowedStages)", ruta, i, name)
			}
			if name == "$lookup" || name == "$graphLookup" {
				if err := validarColeccionDestino(body); err != nil {
					return fmt.Errorf("%setapa %d (%s): %w", ruta, i, name, err)
				}
			}
			if err := validarSubPipelines(name, body, fmt.Sprintf("%setapa %d (%s) > ", ruta, i, name)); err != nil {
				return err
			}
		}
		if err := scanForbidden(stage); err != nil {
			return fmt.Errorf("%setapa %d: %w", ruta, i, err)
		}
	}
	return nil
}

// validarSubPipelines valida con las mismas reglas los pipelines anidados
// de una etapa: las ramas de $facet y el "pipeline" de $lookup
// (2026-10-06). Antes solo se barrian las claves prohibidas, y un $lookup
// hacia cualquier coleccion de la base pasaba si estaba dentro de un
// $facet o del sub-pipeline de otro $lookup.
func validarSubPipelines(name string, body interface{}, ruta string) error {
	switch name {
	case "$facet":
		ramas, ok := body.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%sse esperaba un objeto con las ramas", ruta)
		}
		for rama, sub := range ramas {
			if err := validarSubPipeline(sub, ruta+rama+" > "); err != nil {
				return err
			}
		}
	case "$lookup":
		b, ok := body.(map[string]interface{})
		if !ok {
			return nil
		}
		if sub, ok := b["pipeline"]; ok {
			return validarSubPipeline(sub, ruta+"pipeline > ")
		}
	}
	return nil
}

// validarSubPipeline exige que el sub-pipeline sea una lista de etapas: si
// no, no se puede validar y se rechaza. Puede estar vacio.
func validarSubPipeline(sub interface{}, ruta string) error {
	etapas, ok := sub.([]interface{})
	if !ok {
		return fmt.Errorf("%sse esperaba una lista de etapas", ruta)
	}
	return validarEtapas(etapas, ruta)
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
		return fmt.Errorf("\"from\": %q no es una de las colecciones permitidas -- solo se puede hacer $lookup/$graphLookup hacia las %d entidades del alcance (%s) o hacia una coleccion auxiliar habilitada explicitamente (%s)", fromStr, len(domain.AllTypes), coleccionesDeEntidadesTexto, coleccionesAuxiliaresTexto)
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

// ValidarClavesUnicas rechaza un pipeline si algun objeto, en cualquier
// nivel, repite una clave. Se llama antes de decodificar las etapas para
// ValidatePipeline -- ver la nota del paquete.
func ValidarClavesUnicas(pipeline []json.RawMessage) error {
	for i, raw := range pipeline {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := leerSinClavesRepetidas(dec); err != nil {
			return fmt.Errorf("etapa %d: %w", i, err)
		}
	}
	return nil
}

func leerSinClavesRepetidas(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("no es JSON valido: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil // valor simple
	}
	switch delim {
	case '{':
		vistas := map[string]bool{}
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return fmt.Errorf("no es JSON valido: %w", err)
			}
			clave, _ := t.(string)
			if vistas[clave] {
				return fmt.Errorf("la clave %q esta repetida en el mismo objeto", clave)
			}
			vistas[clave] = true
			if err := leerSinClavesRepetidas(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := leerSinClavesRepetidas(dec); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // cierre del objeto o la lista
		return fmt.Errorf("no es JSON valido: %w", err)
	}
	return nil
}
