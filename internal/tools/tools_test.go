package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/reporting"
)

// llamar ejecuta un handler con los argumentos como texto, igual que llegan
// en un mensaje, y devuelve el texto del resultado y si es un error.
func llamar(t *testing.T, h server.ToolHandlerFunc, args string) (string, bool) {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.RawArguments = json.RawMessage(args)
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("el handler devolvio un error de protocolo: %v", err)
	}
	texto, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("el resultado no es texto: %#v", res.Content[0])
	}
	return texto.Text, res.IsError
}

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

// El limite de pantalla lo fija el servidor: aunque el agente
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
		texto, esError := llamar(t, reportingHandler(r, t.TempDir(), 10, time.Minute, nil), args)
		if esError {
			t.Fatalf("%s: error inesperado: %s", nombre, texto)
		}
		var out reporting.Result
		if err := json.Unmarshal([]byte(texto), &out); err != nil {
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

// diccionario_de_campos no entrega 'fuente' al agente: es metadata
// interna de mantenimiento.
func TestDiccionario_NoEntregaFuente(t *testing.T) {
	rel := dictionary.RelationsData{Campos: map[domain.EntityType][]dictionary.FieldInfo{
		domain.Punto: {{Campo: "data.longName", Descripcion: "Nombre del punto.", Fuente: "NOTA INTERNA: chat de Teams"}},
	}}
	for _, args := range []string{`{}`, `{"tipo":"punto"}`} {
		texto, esError := llamar(t, dictionaryHandler(rel), args)
		if esError || strings.Contains(texto, "fuente") || strings.Contains(texto, "NOTA INTERNA") {
			t.Errorf("%s: la respuesta incluye fuente: %s", args, texto)
		}
		if !strings.Contains(texto, "Nombre del punto.") {
			t.Errorf("%s: falta la descripcion: %s", args, texto)
		}
	}
}

// Los enums de tipos salen de domain.AllTypes: si se suma una entidad al
// registro, las tools la aceptan sin tocarlas a mano.
func TestSchemasTiposDesdeRegistro(t *testing.T) {
	enumDe := func(t *testing.T, schema json.RawMessage, prop string) []string {
		t.Helper()
		var s struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(schema, &s); err != nil {
			t.Fatalf("schema no es JSON valido: %v", err)
		}
		return s.Properties[prop].Enum
	}
	var esperado []string
	for _, tp := range domain.AllTypes {
		esperado = append(esperado, string(tp))
	}

	rep := enumDe(t, reportingSchema(10), "coleccionInicial")
	if strings.Join(rep, ",") != strings.Join(esperado, ",") {
		t.Errorf("enum de coleccionInicial = %v, esperado %v", rep, esperado)
	}
	dic := enumDe(t, dictionarySchema(), "tipo")
	if strings.Join(dic, ",") != ","+strings.Join(esperado, ",") {
		t.Errorf("enum de tipo = %v, esperado \"\" + %v", dic, esperado)
	}
}

// Por el protocolo real (mcp-go sobre tuberias): el servidor se presenta con
// sus instrucciones, lista las tres herramientas, y reporting recibe el
// pipeline con el orden de claves tal como lo escribio el agente.
func TestProtocolo_HerramientasYOrdenDeClaves(t *testing.T) {
	r := &runnerFalso{total: 3}
	s := server.NewMCPServer("prueba", "0", server.WithToolCapabilities(false), server.WithInstructions(Instrucciones))
	RegisterAll(s, r, t.TempDir(), 10, time.Minute, dictionary.RelationsData{}, nil)

	entradaR, entradaW := io.Pipe()
	salidaR, salidaW := io.Pipe()
	stdio := server.NewStdioServer(s)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))
	go func() { _ = stdio.Listen(context.Background(), entradaR, salidaW) }()
	go func() {
		for _, m := range []string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"reporting","arguments":{"coleccionInicial":"punto","modo":"listado","mostrarEnPantalla":true,"pipeline":[{"$sort":{"puntos":-1,"centroUcode":1}}]}}}`,
		} {
			_, _ = io.WriteString(entradaW, m+"\n")
		}
	}()

	resp := map[int]map[string]interface{}{}
	listo := make(chan struct{})
	go func() {
		defer close(listo)
		lector := bufio.NewScanner(salidaR)
		for lector.Scan() {
			var m map[string]interface{}
			if json.Unmarshal(lector.Bytes(), &m) != nil {
				continue
			}
			if id, ok := m["id"].(float64); ok {
				resp[int(id)] = m
			}
			if len(resp) == 3 {
				return
			}
		}
	}()
	select {
	case <-listo:
	case <-time.After(5 * time.Second):
		t.Fatalf("sin respuesta a tiempo; recibidas: %v", resp)
	}
	_ = entradaW.Close()

	ini, _ := resp[1]["result"].(map[string]interface{})
	if instr, _ := ini["instructions"].(string); !strings.Contains(instr, "salesPerson") {
		t.Errorf("initialize no trae las instrucciones: %v", resp[1])
	}
	lista, _ := resp[2]["result"].(map[string]interface{})
	if herramientas, _ := lista["tools"].([]interface{}); len(herramientas) != 3 {
		t.Errorf("tools/list deberia traer 3 herramientas: %v", resp[2])
	}
	if res, _ := resp[3]["result"].(map[string]interface{}); res["isError"] == true {
		t.Fatalf("reporting devolvio error: %v", resp[3])
	}
	if primera := string(r.ultimo[0]); !strings.Contains(primera, `{"puntos":-1,"centroUcode":1}`) {
		t.Errorf("el $sort no llego con el orden original: %s", primera)
	}
}

// destinoFalso registra lo que recibe y comprueba que el archivo local
// exista en el momento de subirlo.
type destinoFalso struct {
	carpeta, nombre string
	existia         bool
	falla           bool
}

func (d *destinoFalso) Guardar(_ context.Context, carpeta, nombre, ruta string) (string, error) {
	d.carpeta, d.nombre = carpeta, nombre
	_, err := os.Stat(ruta)
	d.existia = err == nil
	if d.falla {
		return "", errors.New("mysite not found")
	}
	return "https://onedrive.example/" + nombre, nil
}

// Con destino, el archivo se sube, la copia local se borra y el agente
// recibe el enlace; si la subida falla, recibe un error claro.
func TestReporting_ArchivoAlDestino(t *testing.T) {
	args := `{"coleccionInicial":"punto","modo":"listado","guardarEnArchivo":true,"nombreSugerido":"clases","pipeline":[{"$match":{}}]}`
	for _, falla := range []bool{false, true} {
		dir := t.TempDir()
		d := &destinoFalso{falla: falla}
		texto, esError := llamar(t, reportingHandler(&runnerFalso{total: 3}, dir, 10, time.Minute, d), args)

		if !d.existia || d.nombre != "clases.json" || len(d.carpeta) != len("2006-01-02") {
			t.Errorf("falla=%v: el destino recibio carpeta=%q nombre=%q existia=%v", falla, d.carpeta, d.nombre, d.existia)
		}
		if quedan, _ := os.ReadDir(filepath.Join(dir, d.carpeta)); len(quedan) != 0 {
			t.Errorf("falla=%v: la copia local no se borro: %v", falla, quedan)
		}
		if falla {
			if !esError || !strings.Contains(texto, "OneDrive") {
				t.Errorf("se esperaba un error de OneDrive: %s", texto)
			}
			continue
		}
		var out reporting.Result
		if esError || json.Unmarshal([]byte(texto), &out) != nil || out.ArchivoEscrito != "https://onedrive.example/clases.json" {
			t.Errorf("el resultado tendria que traer el enlace: %s", texto)
		}
	}
}
