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

// validRelations es un RelationsData completo, con todos los campos
// obligatorios: base para los tests, que solo cambian lo que prueban.
func validRelations() dictionary.RelationsData {
	var rel dictionary.RelationsData
	rel.OrigenDatos = "subconjunto restaurado de UAT, sept. 2026"
	rel.EstructuraDeCampos = "estructura de prueba"
	rel.PrefijosUCode = map[domain.EntityType]string{domain.Cliente: "02", domain.Punto: "04"}
	rel.Relaciones = []dictionary.Relacion{
		{Tipo: "padre", Desde: []domain.EntityType{domain.Centro}, Hacia: "cliente", CampoOrigen: "parentUCode", CampoDestino: "ucode", Estado: "confirmado", Reglas: "reglas de prueba", Fuente: "fuente interna de prueba"},
		{Tipo: "anidamiento", Desde: []domain.EntityType{domain.Cliente, domain.Centro}, CampoOrigen: "data.mainAccountNumber", CampoDestino: "ucode", Estado: "confirmado", Reglas: "reglas de prueba"},
		{Tipo: "catalogo", Desde: []domain.EntityType{domain.Punto}, Hacia: "operativeLevel", CampoOrigen: "data.additionalFields.itemClass", CampoDestino: "services.level1Code", Estado: "confirmado", Reglas: "reglas de prueba"},
	}
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

// --- Hierarchy(): origenDatos configurado o vacio. Si viene vacio, el
// agente no tiene que suponer un entorno.

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

// Tipos/ColeccionMongo/TablasAuxiliares vienen de internal/domain (no de
// RelationsData); el resto, del fichero. Si se agrega un campo nuevo al
// struct sin llenarlo, este test lo detecta.
func TestHierarchy_CamposEstructurales_NoQuedanVacios(t *testing.T) {
	info := dictionary.Hierarchy(validRelations())
	if len(info.Tipos) != len(domain.AllTypes) {
		t.Errorf("se esperaban %d tipos, hay %d", len(domain.AllTypes), len(info.Tipos))
	}
	if len(info.TablasAuxiliares) != len(domain.TablasAuxiliares) {
		t.Errorf("TablasAuxiliares = %v, se esperaba el registro %v", info.TablasAuxiliares, domain.TablasAuxiliares)
	}
	if info.EstructuraDeCampos == "" {
		t.Error("EstructuraDeCampos no deberia quedar vacio")
	}
	if len(info.Relaciones) != 3 {
		t.Errorf("se esperaban las 3 relaciones del fixture, hay %d", len(info.Relaciones))
	}
	if info.PrefijosUCode[domain.Punto] != "04" {
		t.Errorf("PrefijosUCode tiene que venir del fichero de relaciones: %v", info.PrefijosUCode)
	}
}

// La fuente de una relacion es metadata de mantenimiento: no le llega al
// agente (igual que la de los campos).
func TestHierarchy_RelacionesSinFuente(t *testing.T) {
	b, err := json.Marshal(dictionary.Hierarchy(validRelations()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "fuente") {
		t.Errorf("la salida de relacion_entre_entidades no deberia traer 'fuente': %s", b)
	}
}

// --- LoadRelations(): carga y validacion del fichero ---

func TestLoadRelations_ArchivoValido_CargaTodo(t *testing.T) {
	path := writeRelationsFile(t, validRelations())
	rel, err := dictionary.LoadRelations(path)
	if err != nil {
		t.Fatalf("LoadRelations: %v", err)
	}
	if rel.OrigenDatos != "subconjunto restaurado de UAT, sept. 2026" {
		t.Errorf("OrigenDatos = %q, no coincide con el fixture", rel.OrigenDatos)
	}
	if len(rel.Relaciones) != 3 || rel.Relaciones[0].Hacia != "cliente" {
		t.Errorf("Relaciones = %+v, no coincide con el fixture", rel.Relaciones)
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
		"estructuraDeCampos": "x",
		"relaciones": [{"tipo": "padre", "desde": ["centro"], "hacia": "cliente", "campoOrigen": "parentUCode", "campoDestino": "ucode", "estado": "confirmado", "reglas": "x"}],
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
		{"estructuraDeCampos", func(r *dictionary.RelationsData) { r.EstructuraDeCampos = "" }},
		{"relaciones", func(r *dictionary.RelationsData) { r.Relaciones = nil }},
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

// prefijosUcode y general son opcionales.
func TestLoadRelations_SinPrefijosNiGeneral_NoFalla(t *testing.T) {
	rel := validRelations()
	rel.PrefijosUCode = nil
	rel.General = ""
	if _, err := dictionary.LoadRelations(writeRelationsFile(t, rel)); err != nil {
		t.Fatalf("prefijosUcode y general son opcionales: %v", err)
	}
}

// Cada relacion se valida contra el registro: un error de tipeo en un tipo,
// una tabla, el tipo de relacion o el estado impide arrancar.
func TestLoadRelations_RelacionInvalida_Falla(t *testing.T) {
	casos := []struct {
		nombre string
		mutar  func(*dictionary.Relacion)
	}{
		{"desde desconocido", func(r *dictionary.Relacion) { r.Desde = []domain.EntityType{"centros"} }},
		{"desde vacio", func(r *dictionary.Relacion) { r.Desde = nil }},
		{"hacia desconocido", func(r *dictionary.Relacion) { r.Hacia = "clientes" }},
		{"hacia tabla auxiliar en padre", func(r *dictionary.Relacion) { r.Hacia = "operativeLevel" }},
		{"tipo desconocido", func(r *dictionary.Relacion) { r.Tipo = "hijo" }},
		{"estado desconocido", func(r *dictionary.Relacion) { r.Estado = "dudoso" }},
		{"sin campoOrigen", func(r *dictionary.Relacion) { r.CampoOrigen = "" }},
		{"sin campoDestino", func(r *dictionary.Relacion) { r.CampoDestino = "" }},
		{"sin reglas", func(r *dictionary.Relacion) { r.Reglas = "" }},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			rel := validRelations()
			c.mutar(&rel.Relaciones[0])
			if _, err := dictionary.LoadRelations(writeRelationsFile(t, rel)); err == nil {
				t.Fatalf("se esperaba un error -- %s", c.nombre)
			}
		})
	}
}

func TestLoadRelations_AnidamientoConHacia_Falla(t *testing.T) {
	rel := validRelations()
	rel.Relaciones[1].Hacia = "cliente"
	if _, err := dictionary.LoadRelations(writeRelationsFile(t, rel)); err == nil {
		t.Fatal("se esperaba un error -- un anidamiento siempre es al mismo tipo, hacia va vacio")
	}
}

func TestLoadRelations_CatalogoHaciaEntidad_Falla(t *testing.T) {
	rel := validRelations()
	rel.Relaciones[2].Hacia = "cliente"
	if _, err := dictionary.LoadRelations(writeRelationsFile(t, rel)); err == nil {
		t.Fatal("se esperaba un error -- un catalogo tiene que ir a una tabla auxiliar habilitada")
	}
}

// Los ficheros versionados (plantillas) tienen que cargar con el formato
// actual del servidor.
func TestLoadRelations_PlantillaVersionada_Carga(t *testing.T) {
	if _, err := dictionary.LoadRelations(filepath.Join("..", "..", "relaciones", "relaciones.example.json")); err != nil {
		t.Fatalf("relaciones.example.json no carga: %v", err)
	}
}

// --- Lookup(): el diccionario de campos, leido de RelationsData.Campos.

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
