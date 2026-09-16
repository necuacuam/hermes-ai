package store_test

import (
	"testing"

	"hermes-mcp/internal/store"
)

// Estos tests no existian hasta la revision del 2026-09-11 -- se agregan
// porque ValidatePipeline es la unica barrera entre un pipeline armado por
// un LLM y la base real del jefe ("no se toca en ningun momento"). Es la
// pieza mas critica de seguridad de todo el redisenio de "reporting" y no
// tenia ni un test.

func TestValidatePipeline_EtapaDeLecturaPermitida(t *testing.T) {
	pipeline := []map[string]interface{}{
		{"$match": map[string]interface{}{"name": "Banco Santander"}},
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
