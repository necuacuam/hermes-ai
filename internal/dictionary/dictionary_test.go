package dictionary_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
)

// validRelations es un RelationsData completo (todos los campos
// obligatorios llenos, incluido Campos desde 2026-09-20) -- base para los
// tests de abajo, que solo tocan lo que quieren probar.
func validRelations() dictionary.RelationsData {
	var rel dictionary.RelationsData
	rel.OrigenDatos = "subconjunto restaurado de UAT, sept. 2026"
	rel.MecanismoDeRelacion = "mecanismo de prueba"
	rel.EstructuraDeCampos = "estructura de prueba"
	rel.Direccion = "nota de direccion de prueba"
	rel.OtrasRelaciones = "otras relaciones de prueba"
	rel.Anidamiento.SegunCodigoHermes = "segun codigo de prueba"
	rel.Anidamiento.SegunDocumentacionDeNegocio = "segun documentacion de prueba"
	rel.Anidamiento.Reglas = "reglas de prueba"
	rel.PrefijosUCode = map[domain.EntityType]string{domain.Cliente: "02", domain.Punto: "04"}
	rel.Campos = map[domain.EntityType][]dictionary.FieldInfo{
		domain.Cliente: {
			{Campo: "data.customerName", Descripcion: "Razon social de prueba.", Fuente: "fixture de test"},
		},
		domain.Centro: {
			{Campo: "data.customerName", Descripcion: "Razon social de prueba.", Fuente: "fixture de test"},
		},
	}
	return rel
}

func writeRelationsFile(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "relaciones.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("escribiendo fixture: %v", err)
	}
	return path
}

// --- Hierarchy(): mismos tests que antes (ahora via RelationsData en vez
// de un string suelto) -- cubren la rama de OrigenDatos configurado/vacio
// (ver TAvo, 2026-09-16: un agente en vivo califico datos de prueba como
// "de esta restauracion UAT" solo porque el prompt lo decia como hecho
// fijo).

func TestHierarchy_OrigenDatosConfigurado_SeDevuelveTalCual(t *testing.T) {
	rel := validRelations()
	info := dictionary.Hierarchy(rel)
	if info.OrigenDatos != "subconjunto restaurado de UAT, sept. 2026" {
		t.Errorf("OrigenDatos = %q, se esperaba el valor configurado tal cual, sin modificar", info.OrigenDatos)
	}
}

func TestHierarchy_OrigenDatosVacio_DevuelveInstruccionDeNoAdivinar(t *testing.T) {
	rel := validRelations()
	rel.OrigenDatos = ""
	info := dictionary.Hierarchy(rel)
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

// Jerarquia/ColeccionMongo siguen viniendo de internal/domain (no de
// RelationsData); PrefijosUCode viene del fichero desde 2026-10-07 -- este test es una red de seguridad barata: si
// alguna vez se agrega un campo nuevo al struct sin llenarlo, salta como
// vacio aca.
func TestHierarchy_CamposEstructuralesFijos_NoQuedanVacios(t *testing.T) {
	info := dictionary.Hierarchy(validRelations())
	if len(info.Jerarquia) != 7 {
		t.Errorf("se esperaban 7 tipos en Jerarquia (las 6 entidades tipo cuenta + salesPerson), hay %d", len(info.Jerarquia))
	}
	if info.MecanismoDeRelacion == "" || info.EstructuraDeCampos == "" || info.Direccion == "" || info.OtrasRelaciones == "" {
		t.Error("ningun campo de texto estructural deberia quedar vacio")
	}
	if info.Anidamiento.Reglas == "" {
		t.Error("Anidamiento.Reglas no deberia quedar vacio")
	}
	if info.PrefijosUCode[domain.Punto] != "04" {
		t.Errorf("PrefijosUCode tiene que venir del fichero de relaciones: %v", info.PrefijosUCode)
	}
}

// --- LoadRelations(): el mecanismo nuevo de carga externa (2026-09-19) ---

func TestLoadRelations_ArchivoValido_CargaTodo(t *testing.T) {
	path := writeRelationsFile(t, validRelations())
	rel, err := dictionary.LoadRelations(path)
	if err != nil {
		t.Fatalf("LoadRelations: %v", err)
	}
	if rel.OrigenDatos != "subconjunto restaurado de UAT, sept. 2026" {
		t.Errorf("OrigenDatos = %q, no coincide con el fixture", rel.OrigenDatos)
	}
	if rel.MecanismoDeRelacion != "mecanismo de prueba" {
		t.Errorf("MecanismoDeRelacion = %q, no coincide con el fixture", rel.MecanismoDeRelacion)
	}
}

func TestLoadRelations_OrigenDatosVacio_NoFalla(t *testing.T) {
	rel := validRelations()
	rel.OrigenDatos = ""
	path := writeRelationsFile(t, rel)
	loaded, err := dictionary.LoadRelations(path)
	if err != nil {
		t.Fatalf("LoadRelations no deberia fallar con origenDatos vacio: %v", err)
	}
	if loaded.OrigenDatos != "" {
		t.Errorf("OrigenDatos = %q, se esperaba vacio tal cual vino del archivo", loaded.OrigenDatos)
	}
}

func TestLoadRelations_ArchivoInexistente_Falla(t *testing.T) {
	_, err := dictionary.LoadRelations(filepath.Join(t.TempDir(), "no-existe.json"))
	if err == nil {
		t.Fatal("se esperaba un error -- el archivo no existe")
	}
}

func TestLoadRelations_JSONInvalido_Falla(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relaciones.json")
	if err := os.WriteFile(path, []byte("{ esto no es json valido"), 0o644); err != nil {
		t.Fatalf("escribiendo fixture: %v", err)
	}
	if _, err := dictionary.LoadRelations(path); err == nil {
		t.Fatal("se esperaba un error -- el JSON esta mal formado")
	}
}

func TestLoadRelations_ClaveDesconocida_Falla(t *testing.T) {
	raw := `{
		"origenDatos": "x",
		"mecanismoDeRelacion": "x",
		"estructuraDeCampos": "x",
		"direccion": "x",
		"otrasRelaciones": "x",
		"anidamiento": {"segunCodigoHermes": "x", "segunDocumentacionDeNegocio": "x", "reglas": "x"},
		"prefijosUcode": {"cliente": "02"},
		"campos": {"cliente": [{"campo": "x", "descripcion": "x", "fuente": "x"}]},
		"campoQueNoExiste": "typo"
	}`
	path := filepath.Join(t.TempDir(), "relaciones.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("escribiendo fixture: %v", err)
	}
	if _, err := dictionary.LoadRelations(path); err == nil {
		t.Fatal("se esperaba un error -- hay una clave que RelationsData no conoce (typo)")
	}
}

func TestLoadRelations_CampoObligatorioVacio_Falla(t *testing.T) {
	casos := []struct {
		nombre string
		mutar  func(*dictionary.RelationsData)
	}{
		{"mecanismoDeRelacion", func(r *dictionary.RelationsData) { r.MecanismoDeRelacion = "" }},
		{"estructuraDeCampos", func(r *dictionary.RelationsData) { r.EstructuraDeCampos = "" }},
		{"direccion", func(r *dictionary.RelationsData) { r.Direccion = "" }},
		{"otrasRelaciones", func(r *dictionary.RelationsData) { r.OtrasRelaciones = "" }},
		{"anidamiento.reglas", func(r *dictionary.RelationsData) { r.Anidamiento.Reglas = "" }},
		{"prefijosUcode", func(r *dictionary.RelationsData) { r.PrefijosUCode = nil }},
		{"campos", func(r *dictionary.RelationsData) { r.Campos = nil }},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			rel := validRelations()
			c.mutar(&rel)
			path := writeRelationsFile(t, rel)
			if _, err := dictionary.LoadRelations(path); err == nil {
				t.Fatalf("se esperaba un error -- %q vacio no deberia pasar la validacion", c.nombre)
			}
		})
	}
}

// --- Lookup(): el diccionario de campos, desde 2026-09-20 leido de
// RelationsData.Campos (ya no hardcodeado en este paquete) -- ver TAvo,
// "TENEMOS UN FICHERO EXTERNO CON EL CONTENIDO DE LAS ESTRUCTURAS?".

func TestLookup_TipoVacio_DevuelveTodosLosTipos(t *testing.T) {
	rel := validRelations()
	result := dictionary.Lookup(rel.Campos, "")
	if len(result) != len(rel.Campos) {
		t.Errorf("Lookup con tipo vacio devolvio %d tipos, se esperaban %d (todos)", len(result), len(rel.Campos))
	}
}

func TestLookup_TipoConocido_DevuelveSoloEseTipo(t *testing.T) {
	rel := validRelations()
	result := dictionary.Lookup(rel.Campos, domain.Cliente)
	if len(result) != 1 {
		t.Fatalf("Lookup(cliente) devolvio %d tipos, se esperaba 1", len(result))
	}
	fields, ok := result[domain.Cliente]
	if !ok || len(fields) == 0 {
		t.Error("Lookup(cliente) no trajo los campos del fixture")
	}
}

func TestLookup_TipoDesconocido_DevuelveVacio(t *testing.T) {
	rel := validRelations()
	result := dictionary.Lookup(rel.Campos, domain.EntityType("no-existe"))
	if len(result) != 0 {
		t.Errorf("Lookup con un tipo inexistente deberia devolver vacio, devolvio %d entradas", len(result))
	}
}
