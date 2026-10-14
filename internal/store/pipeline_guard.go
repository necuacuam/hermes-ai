// Validacion de los pipelines de agregacion que arma el agente. Como el
// pipeline lo escribe un LLM, antes de ejecutarlo se comprueba:
//
//  1. Lista blanca de etapas de solo lectura (allowedStages). Cualquier otra
//     se rechaza, en cualquier nivel.
//  2. Operadores prohibidos (forbiddenKeys) en cualquier posicion, aunque
//     esten anidados dentro de una etapa permitida (por ejemplo, un $where
//     dentro de un $match): los que escriben, ejecutan codigo en el servidor
//     o exponen su estado interno.
//  3. El "from" de todo $lookup/$graphLookup tiene que ser una coleccion del
//     registro: una entidad o una tabla auxiliar. Sin esto, un $lookup podria
//     leer cualquier coleccion de la base.
//  4. Los sub-pipelines (ramas de $facet y "pipeline" de $lookup) se validan
//     con las mismas reglas que el pipeline principal.
//  5. Ningun objeto puede repetir una clave (ValidarClavesUnicas): la
//     validacion trabaja sobre el pipeline decodificado, donde una clave
//     repetida queda con su ultimo valor, pero Mongo ejecuta el texto
//     original con las dos. Sin esta regla, {"from": "<otra>", "from":
//     "servicePlace"} pasaria la validacion.
//
// Limitacion conocida: es una lista blanca por nombre de etapa y operador,
// no un analisis del costo de la consulta (un $lookup sin indice, un
// $graphLookup sin maxDepth). Lo acota el tiempo maximo por consulta
// (HERMES_MCP_MONGO_TIMEOUT), que tambien recibe Mongo como maxTimeMS. Si el
// servidor se expone a muchos usuarios, conviene revisar este punto.
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
// (domain.TablasAuxiliares): solo pueden ser destino de un $lookup.
var coleccionesAuxiliaresPermitidas = func() map[string]bool {
	m := map[string]bool{}
	for _, coll := range domain.TablasAuxiliares {
		m[coll] = true
	}
	return m
}()

// coleccionesPermitidas son los unicos nombres de coleccion a los que un
// pipeline puede referirse como "from" de $lookup/$graphLookup: las
// colecciones de las entidades y las tablas auxiliares del registro.
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
// registro, la lista que se muestra en el error de "from" no permitido.
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

// allowedStages son las etapas de agregacion permitidas. Todas son de
// lectura: ninguna escribe en la base ni ejecuta codigo en el servidor.
// $skip no esta a proposito: solo sirve para paginar, y el agente no debe
// juntar mas filas que el limite de pantalla pidiendolas por partes.
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

// forbiddenKeys son operadores prohibidos en cualquier posicion del
// pipeline, aunque esten anidados dentro de una etapa permitida: ejecutan
// codigo en el servidor, escriben datos o exponen su estado interno.
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
// siempre antes de ejecutar un pipeline.
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
// de una etapa: las ramas de $facet y el "pipeline" de $lookup.
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
// $graphLookup sea una coleccion permitida. Si "from" no viene o no es un
// texto, no se rechaza aqui: Mongo lo rechaza al ejecutar, y este validador
// no duplica esa validacion de forma.
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
