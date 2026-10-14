package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/reporting"
)

// runnerFalso simula Mongo: tiene total documentos y respeta un $limit al
// final del pipeline, como haria la base.
type runnerFalso struct {
	total  int
	ultimo []json.RawMessage
}

func (r *runnerFalso) RunPipeline(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage) ([]map[string]interface{}, error) {
	r.ultimo = pipeline
	n := r.total
	var etapa map[string]int
	if err := json.Unmarshal(pipeline[len(pipeline)-1], &etapa); err == nil {
		if l, ok := etapa["$limit"]; ok && l < n {
			n = l
		}
	}
	docs := make([]map[string]interface{}, n)
	for i := range docs {
		docs[i] = map[string]interface{}{"i": i}
	}
	return docs, nil
}

func (r *runnerFalso) RunPipelineStream(ctx context.Context, t domain.EntityType, pipeline []json.RawMessage, onDoc func(map[string]interface{}) error) error {
	docs, _ := r.RunPipeline(ctx, t, pipeline)
	for _, d := range docs {
		if err := onDoc(d); err != nil {
			return err
		}
	}
	return nil
}

// El limite de pantalla lo fija el servidor (2026-10-07): aunque el agente
// mande un limitePantalla mayor, recibe como maximo el configurado y el
// aviso de que hay mas.
func TestReporting_LimiteDePantallaFijoDelServidor(t *testing.T) {
	casos := map[string]string{
		"sin limite":              `{"coleccionInicial":"punto","modo":"listado","mostrarEnPantalla":true,"pipeline":[{"$match":{}}]}`,
		"agente pide 30":          `{"coleccionInicial":"punto","modo":"listado","mostrarEnPantalla":true,"limitePantalla":30,"pipeline":[{"$match":{}}]}`,
		"agente pide 30 y $limit": `{"coleccionInicial":"punto","modo":"listado","mostrarEnPantalla":true,"limitePantalla":30,"pipeline":[{"$match":{}},{"$limit":30}]}`,
	}
	for nombre, args := range casos {
		r := &runnerFalso{total: 25}
		res := reportingHandler(r, t.TempDir(), 10, time.Minute)(json.RawMessage(args))
		if res.IsError {
			t.Fatalf("%s: error inesperado: %s", nombre, res.Content[0].Text)
		}
		var out reporting.Result
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("%s: respuesta invalida: %v", nombre, err)
		}
		if len(out.Filas) != 10 || !out.FilasTruncadas {
			t.Errorf("%s: %d filas, truncadas=%v -- se esperaban 10 con aviso", nombre, len(out.Filas), out.FilasTruncadas)
		}
		if ultima := string(r.ultimo[len(r.ultimo)-1]); !strings.Contains(ultima, `"$limit": 11`) {
			t.Errorf("%s: la ultima etapa enviada a Mongo es %s, se esperaba el $limit del servidor (11)", nombre, ultima)
		}
	}
}

// La descripcion que ve el agente dice el limite real y que no se cambia.
func TestReporting_SinParametroDeLimiteEnElSchema(t *testing.T) {
	if strings.Contains(string(reportingSchema(10)), `"limitePantalla"`) {
		t.Error("el schema de reporting todavia ofrece el parametro limitePantalla")
	}
	if !strings.Contains(string(reportingSchema(10)), "como maximo 10") {
		t.Error("el schema no informa el limite fijo de 10 filas")
	}
}

// diccionario_de_campos no entrega 'fuente' al agente (2026-10-07): es
// metadata interna y antes solo la protegia una instruccion del prompt.
func TestDiccionario_NoEntregaFuente(t *testing.T) {
	rel := dictionary.RelationsData{Campos: map[domain.EntityType][]dictionary.FieldInfo{
		domain.Punto: {{Campo: "data.longName", Descripcion: "Nombre del punto.", Fuente: "NOTA INTERNA: chat de Teams"}},
	}}
	for _, args := range []string{`{}`, `{"tipo":"punto"}`} {
		res := dictionaryHandler(rel)(json.RawMessage(args))
		texto := res.Content[0].Text
		if res.IsError || strings.Contains(texto, "fuente") || strings.Contains(texto, "NOTA INTERNA") {
			t.Errorf("%s: la respuesta incluye fuente: %s", args, texto)
		}
		if !strings.Contains(texto, "Nombre del punto.") {
			t.Errorf("%s: falta la descripcion: %s", args, texto)
		}
	}
}
