package dictionary_test

import (
	"strings"
	"testing"

	"hermes-mcp/internal/dictionary"
)

// Estos tests cubren Hierarchy() por primera vez (no tenia ninguno) --
// puntualmente la rama nueva del campo OrigenDatos (ver TAvo, 2026-09-16):
// un agente en vivo califico datos de prueba como "de esta restauracion
// UAT" solo porque el prompt lo decia como hecho fijo. La correccion fue
// que Hierarchy reciba el origen como parametro (via HERMES_MCP_ORIGEN_DATOS
// en main.go) y, si no esta configurado, devuelva una instruccion explicita
// de no adivinar -- estos tests confirman las dos ramas.

func TestHierarchy_OrigenDatosConfigurado_SeDevuelveTalCual(t *testing.T) {
	info := dictionary.Hierarchy("subconjunto restaurado de UAT, sept. 2026")
	if info.OrigenDatos != "subconjunto restaurado de UAT, sept. 2026" {
		t.Errorf("OrigenDatos = %q, se esperaba el valor configurado tal cual, sin modificar", info.OrigenDatos)
	}
}

func TestHierarchy_OrigenDatosVacio_DevuelveInstruccionDeNoAdivinar(t *testing.T) {
	info := dictionary.Hierarchy("")
	if info.OrigenDatos == "" {
		t.Fatal("OrigenDatos no deberia quedar vacio -- tiene que traer una instruccion explicita de no asumir un entorno")
	}
	// No se fija el texto exacto (es prosa, puede reescribirse) -- se fija
	// la garantia real: que menciona explicitamente que no hay que asumir
	// un entorno por conocimiento previo.
	lower := strings.ToLower(info.OrigenDatos)
	if !strings.Contains(lower, "no asumas") && !strings.Contains(lower, "no configurado") {
		t.Errorf("OrigenDatos vacio deberia advertir explicitamente que no se asuma un entorno -- texto actual: %q", info.OrigenDatos)
	}
}

// Hierarchy no recibe mas parametros que OrigenDatos -- todo lo demas
// (jerarquia, prefijos, colecciones) es fijo. Este test es una red de
// seguridad barata: si alguna vez se agrega un campo nuevo al struct sin
// llenarlo, salta como vacio aca.
func TestHierarchy_CamposEstructuralesFijos_NoQuedanVacios(t *testing.T) {
	info := dictionary.Hierarchy("cualquier origen")
	if len(info.Jerarquia) != 6 {
		t.Errorf("se esperaban 6 tipos en Jerarquia, hay %d", len(info.Jerarquia))
	}
	if info.MecanismoDeRelacion == "" || info.EstructuraDeCampos == "" || info.Direccion == "" || info.OtrasRelaciones == "" {
		t.Error("ningun campo de texto estructural deberia quedar vacio")
	}
	if info.Anidamiento.EstadoActual == "" {
		t.Error("Anidamiento.EstadoActual no deberia quedar vacio")
	}
}
