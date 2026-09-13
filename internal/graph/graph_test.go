package graph_test

import (
	"context"
	"testing"

	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/graph"
	"hermes-mcp/internal/store/mockstore"
)

func newTestGraph() *graph.Graph {
	return graph.New(mockstore.New(mockstore.Fixtures()))
}

func TestResolvePath_CadenaAnidada(t *testing.T) {
	g := newTestGraph()
	steps, err := g.ResolvePath(context.Background(), "Macedonia", []string{"Sucursal 23", "Sala 3"})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("se esperaban 3 pasos (cliente + 2 centros), se obtuvieron %d", len(steps))
	}
	if steps[0].Doc.Type != domain.Cliente || steps[0].Doc.Name != "Macedonia" {
		t.Errorf("paso 0 incorrecto: %+v", steps[0].Doc)
	}
	if steps[1].Doc.Type != domain.Centro || steps[1].Doc.Name != "Sucursal 23 (Madrid)" {
		t.Errorf("paso 1 incorrecto: %+v", steps[1].Doc)
	}
	if steps[2].Doc.Type != domain.Centro || steps[2].Doc.Name != "Sala 3" {
		t.Errorf("paso 2 incorrecto (deberia ser el centro anidado 'Sala 3'): %+v", steps[2].Doc)
	}
}

func TestResolvePath_ClienteInexistente(t *testing.T) {
	g := newTestGraph()
	_, err := g.ResolvePath(context.Background(), "Cliente Que No Existe", nil)
	if err == nil {
		t.Fatal("se esperaba error por cliente inexistente")
	}
}

func TestResolvePath_CentroNoEncontradoDejaRastro(t *testing.T) {
	g := newTestGraph()
	steps, err := g.ResolvePath(context.Background(), "Macedonia", []string{"Sucursal 23", "Sala Que No Existe"})
	if err == nil {
		t.Fatal("se esperaba error por centro inexistente")
	}
	// Aunque falle, tiene que devolver lo que si se resolvio (no ocultar
	// progreso parcial) -- cliente + sucursal 23.
	if len(steps) != 2 {
		t.Fatalf("se esperaban 2 pasos resueltos antes del corte, se obtuvieron %d", len(steps))
	}
}

func TestCountByType_SoloCamarasDeSala3(t *testing.T) {
	g := newTestGraph()
	steps, err := g.ResolvePath(context.Background(), "Macedonia", []string{"Sucursal 23", "Sala 3"})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	root := steps[len(steps)-1].Doc

	camaras, err := g.CountByType(context.Background(), root.UCode, domain.Punto, "pointOfServiceType", "amara")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	// Sala 3 tiene 2 camaras y 1 sensor -- el filtro tiene que devolver
	// solo las 2 camaras, ni el sensor ni el puesto de vigilancia que
	// cuelga directo de Sucursal 23 (fuera de Sala 3).
	if len(camaras) != 2 {
		t.Fatalf("se esperaban 2 camaras en Sala 3, se obtuvieron %d: %+v", len(camaras), camaras)
	}
}

func TestCountByType_RecursivoSumaNiveles(t *testing.T) {
	// Bajo la Agencia de Banco Santander hay 2 camaras directas + 1 mas
	// dentro del centro anidado "Caja fuerte" -- el conteo recursivo tiene
	// que sumar las 3, no solo las 2 directas.
	g := newTestGraph()
	steps, err := g.ResolvePath(context.Background(), "Banco Santander", []string{"Agencia Plaza Conde de Casal 7"})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	root := steps[len(steps)-1].Doc

	camaras, err := g.CountByType(context.Background(), root.UCode, domain.Punto, "pointOfServiceType", "amara")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(camaras) != 3 {
		t.Fatalf("se esperaban 3 camaras en total (incluyendo la de Caja fuerte), se obtuvieron %d", len(camaras))
	}
}

func TestSearchGlobal_EncuentraSinAsumirTipoYRevelaAmbiguedadEntreTipos(t *testing.T) {
	// Este test documenta, con datos reales de la fixture, exactamente el
	// caso que motivo separar "seek" de una resolucion que asumia el tipo:
	// "Macedonia" matchea tanto el CLIENTE (Macedonia) como el GRUPO (Grupo
	// Macedonia Iberia) -- SearchGlobal no puede ni debe asumir cual de los
	// dos quiso decir el usuario, tiene que devolver ambos como candidatos.
	g := newTestGraph()
	docs, err := g.SearchGlobal(context.Background(), "Macedonia", "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("se esperaban 2 candidatos (cliente + grupo), se obtuvieron %d: %+v", len(docs), docs)
	}
	tipos := map[domain.EntityType]bool{}
	for _, d := range docs {
		tipos[d.Type] = true
	}
	if !tipos[domain.Cliente] || !tipos[domain.Grupo] {
		t.Fatalf("se esperaba un match de tipo cliente y otro de tipo grupo, se obtuvo: %+v", docs)
	}
}

func TestSearchGlobal_ConTipoDesambigua(t *testing.T) {
	// Acotar por tipo es una forma de que el agente resuelva la ambiguedad
	// del test anterior cuando el usuario (o el contexto) ya sabe que busca
	// especificamente un cliente.
	g := newTestGraph()
	docs, err := g.SearchGlobal(context.Background(), "Macedonia", domain.Cliente)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(docs) != 1 || docs[0].Type != domain.Cliente || docs[0].Name != "Macedonia" {
		t.Fatalf("se esperaba encontrar solo el cliente 'Macedonia', se obtuvo: %+v", docs)
	}
}

func TestSearchGlobal_SinMatchDevuelveVacioNoError(t *testing.T) {
	g := newTestGraph()
	docs, err := g.SearchGlobal(context.Background(), "Entidad Que No Existe", "")
	if err != nil {
		t.Fatalf("la ausencia de match no deberia ser un error: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("se esperaban 0 resultados, se obtuvieron %d", len(docs))
	}
}

func TestSearchChildren_ListaTodosSinNeedle(t *testing.T) {
	g := newTestGraph()
	// Simula al usuario que no sabe el nombre de la sala y necesita ver las
	// opciones bajo la Sucursal 23 para reconocerla.
	docs, err := g.SearchChildren(context.Background(), "ES03000001", "", "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	// Sucursal 23 tiene 2 hijos directos: Sala 3 (centro anidado) y Puesto
	// de vigilancia 1 (punto).
	if len(docs) != 2 {
		t.Fatalf("se esperaban 2 hijos directos de Sucursal 23, se obtuvieron %d: %+v", len(docs), docs)
	}
}

func TestSearchChildren_ConNeedleYTipoFiltra(t *testing.T) {
	g := newTestGraph()
	docs, err := g.SearchChildren(context.Background(), "ES03000001", "sala", domain.Centro)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(docs) != 1 || docs[0].Name != "Sala 3" {
		t.Fatalf("se esperaba encontrar solo 'Sala 3', se obtuvo: %+v", docs)
	}
}

func TestBreadcrumb_ArmaCadenaLegibleSubiendoElArbol(t *testing.T) {
	g := newTestGraph()
	// Sala 3 (ES03000002) cuelga de Sucursal 23 -> Macedonia -> Grupo
	// Macedonia Iberia -> Prosegur Global (holding) -- el breadcrumb tiene
	// que reflejar TODA la cadena hasta la raiz, no solo el padre directo.
	sala3, found, err := g.Store.Get(context.Background(), "ES03000002")
	if err != nil || !found {
		t.Fatalf("no se pudo obtener Sala 3 de la fixture: found=%v err=%v", found, err)
	}
	got := g.Breadcrumb(context.Background(), sala3)
	want := "Prosegur Global > Grupo Macedonia Iberia > Macedonia > Sucursal 23 (Madrid) > Sala 3"
	if got != want {
		t.Errorf("breadcrumb incorrecto: got %q, want %q", got, want)
	}
}

func TestCountByType_SoloCajaFuerte(t *testing.T) {
	g := newTestGraph()
	steps, err := g.ResolvePath(context.Background(), "Banco Santander", []string{"Agencia Plaza Conde de Casal 7", "Caja fuerte"})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	root := steps[len(steps)-1].Doc

	camaras, err := g.CountByType(context.Background(), root.UCode, domain.Punto, "pointOfServiceType", "amara")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(camaras) != 1 {
		t.Fatalf("se esperaba 1 camara dentro de Caja fuerte, se obtuvieron %d", len(camaras))
	}
}
