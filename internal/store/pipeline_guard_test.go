package store_test

import (
	"encoding/json"
	"strings"
	"testing"

	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/store"
)

// Estos tests no existian hasta la revision del 2026-09-11 -- se agregan
// porque ValidatePipeline es la unica barrera entre un pipeline armado por
// un LLM y la base real del jefe ("no se toca en ningun momento"). Es la
// pieza mas critica de seguridad de todo el redisenio de "reporting" y no
// tenia ni un test.

func TestValidatePipeline_EtapaDeLecturaPermitida(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$match": map[string]interface{}{"data.customerName": "Banco Santander"}},
		{"$limit": 5},
	}
	if err := store.ValidatePipeline(pipeline); err != nil {
		t.Errorf("no se esperaba error para un pipeline de solo lectura: %v", err)
	}
}

func TestValidatePipeline_PipelineVacio_Rechazado(t *testing.T) {
	if err := store.ValidatePipeline(nil); err == nil {
		t.Error("se esperaba error para un pipeline vacio")
	}
}

func TestValidatePipeline_EtapaConMasDeUnaClave_Rechazada(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$match": map[string]interface{}{}, "$limit": 5},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- una etapa tiene que ser un unico operador")
	}
}

func TestValidatePipeline_EtapaDeEscritura_Rechazada(t *testing.T) {
	for _, etapa := range []string{"$out", "$merge"} {
		pipeline := []map[string]interface{}{
			{etapa: "cualquierColeccion"},
		}
		if err := store.ValidatePipeline(pipeline); err == nil {
			t.Errorf("se esperaba error para la etapa de escritura %q", etapa)
		}
	}
}

func TestValidatePipeline_OperadorProhibidoAnidado_Rechazado(t *testing.T) {
	// $where escondido DENTRO de un $match, que por si solo es una etapa
	// permitida -- tiene que rechazarse igual por el barrido recursivo.
	pipeline := []map[string]interface{}{
		{"$match": map[string]interface{}{"$where": "function() { return true; }"}},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- $where anidado dentro de $match tiene que rechazarse")
	}
}

func TestValidatePipeline_ClaveDeOperadorSinEspacio_OK(t *testing.T) {
	// Un operador de lectura comun dentro de un $match no se rechaza.
	pipeline := []map[string]interface{}{
		{"$match": map[string]interface{}{"parentUCode": map[string]interface{}{"$exists": true}}},
	}
	if err := store.ValidatePipeline(pipeline); err != nil {
		t.Errorf("no se esperaba error para \"$exists\" bien formado: %v", err)
	}
}

func TestValidatePipeline_OperadorProhibidoDentroDeFacet_Rechazado(t *testing.T) {
	// $merge escondido dentro de una sub-lista de $facet -- prueba que el
	// barrido recursivo entra en listas, no solo en mapas.
	pipeline := []map[string]interface{}{
		{"$facet": map[string]interface{}{
			"ramaA": []interface{}{
				map[string]interface{}{"$merge": "otraColeccion"},
			},
		}},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- $merge anidado dentro de $facet tiene que rechazarse")
	}
}

func TestValidatePipeline_LookupHaciaColeccionPermitida_OK(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$lookup": map[string]interface{}{
			"from":         "servicePlace",
			"localField":   "ucode",
			"foreignField": "parentUCode",
			"as":           "centros",
		}},
	}
	if err := store.ValidatePipeline(pipeline); err != nil {
		t.Errorf("no se esperaba error para un $lookup hacia una coleccion del alcance: %v", err)
	}
}

func TestValidatePipeline_LookupHaciaOperativeLevel_OK(t *testing.T) {
	// operativeLevel es el catalogo auxiliar de clase/subclase de punto
	// (ver la nota fechada 2026-09-15 en pipeline_guard.go) -- NO es una de
	// las 6 entidades tipo cuenta, pero se habilito explicitamente como
	// destino de $lookup de solo lectura.
	pipeline := []map[string]interface{}{
		{"$lookup": map[string]interface{}{
			"from":     "operativeLevel",
			"pipeline": []interface{}{},
			"as":       "clases",
		}},
	}
	if err := store.ValidatePipeline(pipeline); err != nil {
		t.Errorf("no se esperaba error para un $lookup hacia operativeLevel: %v", err)
	}
}

func TestValidatePipeline_LookupHaciaColeccionFueraDeAlcance_Rechazado(t *testing.T) {
	// Este es el caso que motivo agregar validarColeccionDestino: sin esta
	// regla, un pipeline podia empezar en una coleccion del alcance
	// (coleccionInicial) y despues, via $lookup, leer cualquier otra
	// coleccion de la misma base -- incluyendo datos fuera del acuerdo de
	// alcance (contract, serviceOrder, colecciones de sistema, etc.).
	pipeline := []map[string]interface{}{
		{"$lookup": map[string]interface{}{
			"from":         "contract",
			"localField":   "ucode",
			"foreignField": "accountUniqueId",
			"as":           "contratos",
		}},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- $lookup hacia una coleccion fuera de las 6 entidades tipo cuenta tiene que rechazarse")
	}
}

func TestValidatePipeline_GraphLookupHaciaColeccionFueraDeAlcance_Rechazado(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$graphLookup": map[string]interface{}{
			"from":             "activity",
			"startWith":        "$ucode",
			"connectFromField": "ucode",
			"connectToField":   "parentUCode",
			"as":               "actividades",
		}},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- $graphLookup hacia una coleccion fuera de alcance tiene que rechazarse")
	}
}

// Sub-pipelines (2026-10-06): hasta esta fecha, la lista blanca de etapas y
// el control de "from" se aplicaban solo al primer nivel, y estos 4
// pipelines pasaban la validacion -- dejaban leer cualquier coleccion de la
// base.
func TestValidatePipeline_SubPipelinesFueraDeAlcance_Rechazados(t *testing.T) {
	lookupAjeno := map[string]interface{}{"$lookup": map[string]interface{}{
		"from": "contract", "localField": "ucode", "foreignField": "accountUniqueId", "as": "contratos",
	}}
	unionWith := map[string]interface{}{"$unionWith": "contract"}
	casos := map[string][]map[string]interface{}{
		"$lookup ajeno dentro de $facet": {
			{"$facet": map[string]interface{}{"ramaA": []interface{}{lookupAjeno}}},
		},
		"$unionWith dentro de $facet": {
			{"$facet": map[string]interface{}{"ramaA": []interface{}{unionWith}}},
		},
		"$lookup ajeno en el pipeline de un $lookup": {
			{"$lookup": map[string]interface{}{
				"from": "servicePlace", "pipeline": []interface{}{lookupAjeno}, "as": "centros",
			}},
		},
		"$unionWith en el pipeline de un $lookup": {
			{"$lookup": map[string]interface{}{
				"from": "servicePlace", "pipeline": []interface{}{unionWith}, "as": "centros",
			}},
		},
	}
	for nombre, pipeline := range casos {
		if err := store.ValidatePipeline(pipeline); err == nil {
			t.Errorf("%s: se esperaba error", nombre)
		}
	}
}

func TestValidatePipeline_SubPipelineDentroDelAlcance_OK(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$lookup": map[string]interface{}{
			"from": "servicePlace",
			"let":  map[string]interface{}{"p": "$parentUCode"},
			"pipeline": []interface{}{
				map[string]interface{}{"$match": map[string]interface{}{"$expr": map[string]interface{}{"$eq": []interface{}{"$ucode", "$$p"}}}},
				map[string]interface{}{"$lookup": map[string]interface{}{
					"from": "operativeLevel", "pipeline": []interface{}{}, "as": "clases",
				}},
			},
			"as": "centro",
		}},
		{"$facet": map[string]interface{}{
			"total":  []interface{}{map[string]interface{}{"$count": "n"}},
			"puntos": []interface{}{map[string]interface{}{"$limit": 10}},
		}},
	}
	if err := store.ValidatePipeline(pipeline); err != nil {
		t.Errorf("no se esperaba error para sub-pipelines dentro del alcance: %v", err)
	}
}

func TestValidatePipeline_SubPipelineQueNoEsLista_Rechazado(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$lookup": map[string]interface{}{
			"from": "servicePlace", "pipeline": map[string]interface{}{"$match": map[string]interface{}{}}, "as": "c",
		}},
	}
	if err := store.ValidatePipeline(pipeline); err == nil {
		t.Error("se esperaba error -- un sub-pipeline que no es una lista no se puede validar")
	}
}

// Claves repetidas (2026-10-06): el validador ve el ultimo valor de una
// clave repetida y Mongo ejecuta el texto original -- se rechazan antes.
func TestValidarClavesUnicas(t *testing.T) {
	casos := []struct {
		nombre, pipeline string
		ok               bool
	}{
		{"sin repetidas", `[{"$match":{"a":1,"b":{"c":2}}},{"$facet":{"x":[{"$limit":1}],"y":[{"$limit":2}]}}]`, true},
		{"misma clave en objetos distintos", `[{"$match":{"a":1}},{"$project":{"a":1}},{"$match":{"b":[{"a":1},{"a":2}]}}]`, true},
		{"from repetido en $lookup", `[{"$lookup":{"from":"contract","from":"servicePlace","pipeline":[],"as":"x"}}]`, false},
		{"etapa repetida", `[{"$match":{},"$match":{"a":1}}]`, false},
		{"repetida dentro de una lista", `[{"$facet":{"x":[{"$lookup":{"from":"contract","from":"servicePlace","as":"c"}}]}}]`, false},
	}
	for _, c := range casos {
		var p []json.RawMessage
		if err := json.Unmarshal([]byte(c.pipeline), &p); err != nil {
			t.Fatalf("%s: %v", c.nombre, err)
		}
		err := store.ValidarClavesUnicas(p)
		if c.ok && err != nil {
			t.Errorf("%s: no se esperaba error: %v", c.nombre, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: se esperaba error", c.nombre)
		}
	}
}

// $skip se rechaza en cualquier nivel (2026-10-07): solo sirve para paginar.
func TestValidatePipeline_Skip_Rechazado(t *testing.T) {
	casos := [][]map[string]interface{}{
		{{"$match": map[string]interface{}{}}, {"$skip": 10}},
		{{"$facet": map[string]interface{}{"x": []interface{}{map[string]interface{}{"$skip": 10}}}}},
	}
	for i, p := range casos {
		if err := store.ValidatePipeline(p); err == nil {
			t.Errorf("caso %d: se esperaba error por $skip", i)
		}
	}
}

// El error de "from" no permitido lista las colecciones del registro, no
// una lista escrita a mano.
func TestFromNoPermitidoListaColeccionesDelRegistro(t *testing.T) {
	err := store.ValidatePipeline([]map[string]interface{}{
		{"$lookup": map[string]interface{}{"from": "otraColeccion", "localField": "a", "foreignField": "b", "as": "c"}},
	})
	if err == nil {
		t.Fatal("se esperaba error por from no permitido")
	}
	for _, t2 := range domain.AllTypes {
		if !strings.Contains(err.Error(), domain.CollectionName[t2]) {
			t.Errorf("el error no menciona %s: %v", domain.CollectionName[t2], err)
		}
	}
	if !strings.Contains(err.Error(), "operativeLevel") {
		t.Errorf("el error no menciona operativeLevel: %v", err)
	}
}
