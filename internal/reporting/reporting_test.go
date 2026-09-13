package reporting_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermes-mcp/internal/reporting"
)

func sampleDocs() []map[string]interface{} {
	return []map[string]interface{}{
		{"ucode": "ES04000001", "nombre": "Cámara 1"},
		{"ucode": "ES04000002", "nombre": "Cámara 2"},
	}
}

func hoyEnEspana(t *testing.T) string {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

func TestRun_ModoConteo_DevuelveCantidad(t *testing.T) {
	docs := []map[string]interface{}{{"cantidad": float64(3)}}
	result, err := reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if result.Cantidad == nil || *result.Cantidad != 3 {
		t.Fatalf("cantidad esperada 3, obtuvo %v", result.Cantidad)
	}
	if result.ArchivoEscrito != "" || result.Formato != "" {
		t.Errorf("modo conteo nunca escribe archivo: %+v", result)
	}
}

func TestRun_ModoConteo_MasDeUnDocumentoEsError(t *testing.T) {
	docs := []map[string]interface{}{{"cantidad": float64(1)}, {"cantidad": float64(2)}}
	_, err := reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
	if err == nil {
		t.Fatal("se esperaba error -- el pipeline de conteo tiene que devolver a lo sumo un documento")
	}
}

func TestRun_ModoConteo_CeroDocumentosEsCantidadCero(t *testing.T) {
	// $count (y un $group equivalente) no emiten NINGUN documento cuando no
	// hay matches -- eso no es un pipeline mal armado, es la respuesta "0".
	result, err := reporting.Run(nil, reporting.Input{Modo: reporting.ModoConteo})
	if err != nil {
		t.Fatalf("0 documentos en modo conteo no deberia ser error: %v", err)
	}
	if result.Cantidad == nil || *result.Cantidad != 0 {
		t.Fatalf("cantidad esperada 0, obtuvo %v", result.Cantidad)
	}
}

func TestRun_ModoConteo_SinCampoNumericoEsError(t *testing.T) {
	docs := []map[string]interface{}{{"nombre": "no soy un numero"}}
	_, err := reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
	if err == nil {
		t.Fatal("se esperaba error -- no hay ningun campo numerico para reportar")
	}
}

func TestRun_ModoConteo_MasDeUnCampoNumericoEsError(t *testing.T) {
	// Elegir "el primero que aparezca" iterando un map de Go seria no
	// determinista (orden de mapas). En cambio se rechaza explicitamente.
	docs := []map[string]interface{}{{"cantidad": float64(5), "promedio": float64(3)}}
	_, err := reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
	if err == nil {
		t.Fatal("se esperaba error -- hay 2 campos numericos, no se puede elegir uno sin ambiguedad")
	}
}

func TestRun_ModoConteo_IgnoraIdYUsaElUnicoCampoRestante(t *testing.T) {
	docs := []map[string]interface{}{{"_id": "algo", "total": float64(7)}}
	result, err := reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if result.Cantidad == nil || *result.Cantidad != 7 {
		t.Fatalf("cantidad esperada 7 (ignorando _id), obtuvo %v", result.Cantidad)
	}
}

func TestRun_ModoListado_Pantalla_SinTruncar(t *testing.T) {
	result, err := reporting.Run(sampleDocs(), reporting.Input{
		Modo:              reporting.ModoListado,
		MostrarEnPantalla: true,
		LimitePantalla:    10,
	})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(result.Filas) != 2 || result.FilasTruncadas {
		t.Errorf("se esperaban 2 filas sin truncar, se obtuvo: %+v", result)
	}
	if result.ArchivoEscrito != "" || result.Formato != "" {
		t.Errorf("Run nunca escribe archivo (eso es StreamWriter): %+v", result)
	}
}

func TestRun_ModoListado_PantallaTruncaYAvisaExplicitamente(t *testing.T) {
	result, err := reporting.Run(sampleDocs(), reporting.Input{
		Modo:              reporting.ModoListado,
		MostrarEnPantalla: true,
		LimitePantalla:    1,
	})
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(result.Filas) != 1 {
		t.Fatalf("se esperaba 1 fila mostrada (limite), se obtuvieron %d", len(result.Filas))
	}
	if !result.FilasTruncadas {
		t.Error("se esperaba FilasTruncadas=true -- el recorte tiene que quedar explicito, nunca silencioso")
	}
	if result.TotalFilas == nil || *result.TotalFilas != 2 {
		t.Errorf("TotalFilas deberia reflejar el total real (2), se obtuvo %v", result.TotalFilas)
	}
}

func TestRun_ModoListado_SinPantalla_DevuelveError(t *testing.T) {
	_, err := reporting.Run(sampleDocs(), reporting.Input{Modo: reporting.ModoListado})
	if err == nil {
		t.Fatal("se esperaba error -- modo listado sin MostrarEnPantalla no tiene ningun efecto en Run")
	}
}

// --- StreamWriter (guardado a archivo -- siempre JSON, paginado) ---

func TestStreamWriter_EscribeArchivoJSONValidoAMedidaQueLlegan(t *testing.T) {
	dir := t.TempDir()
	sw, err := reporting.NewStreamWriter(dir, "reporte-streaming", false, 0)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	for _, d := range sampleDocs() {
		if err := sw.Add(d); err != nil {
			t.Fatalf("no se esperaba error en Add: %v", err)
		}
	}
	result, err := sw.Finish()
	if err != nil {
		t.Fatalf("no se esperaba error en Finish: %v", err)
	}
	if result.TotalFilas == nil || *result.TotalFilas != 2 {
		t.Errorf("TotalFilas esperado 2, obtuvo %v", result.TotalFilas)
	}
	if result.Formato != "json" {
		t.Errorf("formato esperado json, obtuvo %q", result.Formato)
	}

	raw, err := os.ReadFile(result.ArchivoEscrito)
	if err != nil {
		t.Fatalf("no se pudo leer el archivo escrito: %v", err)
	}
	var payload struct {
		Detalle  []map[string]interface{} `json:"detalle"`
		Cantidad int                      `json:"cantidad"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("el archivo escrito en streaming no es JSON valido: %v -- contenido: %s", err, raw)
	}
	if len(payload.Detalle) != 2 || payload.Cantidad != 2 {
		t.Errorf("contenido incorrecto: %+v", payload)
	}
}

func TestStreamWriter_SinResultados_NoCreaArchivoNiCarpeta(t *testing.T) {
	dir := t.TempDir()
	sw, err := reporting.NewStreamWriter(dir, "reporte-vacio", true, 10)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	// Nunca se llama a Add -- el pipeline no devolvio ningun documento.
	result, err := sw.Finish()
	if err != nil {
		t.Fatalf("no se esperaba error en Finish: %v", err)
	}
	if result.ArchivoEscrito != "" || result.Formato != "" {
		t.Errorf("con 0 resultados no se tiene que haber escrito ningun archivo: %+v", result)
	}
	if result.TotalFilas == nil || *result.TotalFilas != 0 {
		t.Errorf("TotalFilas se tiene que informar explicitamente en 0, no omitirse: %v", result.TotalFilas)
	}
	if len(result.Filas) != 0 || result.FilasTruncadas {
		t.Errorf("sin documentos no deberia haber filas ni truncado: %+v", result)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("error leyendo el directorio base: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("no se tendria que haber creado ni la carpeta del dia -- quedo: %v", entries)
	}
}

func TestStreamWriter_ConPantalla_BufferAcotadoYAvisaTruncado(t *testing.T) {
	dir := t.TempDir()
	sw, err := reporting.NewStreamWriter(dir, "reporte", true, 1) // limite de pantalla = 1
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	for _, d := range sampleDocs() { // 2 documentos, limite 1 -- tiene que truncar
		if err := sw.Add(d); err != nil {
			t.Fatalf("no se esperaba error: %v", err)
		}
	}
	result, err := sw.Finish()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if len(result.Filas) != 1 {
		t.Fatalf("se esperaba 1 fila en pantalla (limite), se obtuvieron %d", len(result.Filas))
	}
	if !result.FilasTruncadas {
		t.Error("se esperaba FilasTruncadas=true")
	}
	// El archivo, en cambio, tiene que llevar los 2 documentos completos --
	// el recorte es solo para la vista previa en memoria.
	raw, err := os.ReadFile(result.ArchivoEscrito)
	if err != nil {
		t.Fatalf("no se pudo leer el archivo: %v", err)
	}
	var payload struct {
		Detalle []map[string]interface{} `json:"detalle"`
	}
	json.Unmarshal(raw, &payload)
	if len(payload.Detalle) != 2 {
		t.Errorf("el archivo tiene que llevar los 2 documentos completos, tiene %d", len(payload.Detalle))
	}
}

func TestStreamWriter_CarpetaDelDiaEnHoraDeEspana(t *testing.T) {
	dir := t.TempDir()
	sw, err := reporting.NewStreamWriter(dir, "reporte", false, 0)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	sw.Add(sampleDocs()[0])
	result, err := sw.Finish()
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	esperado := filepath.Join(dir, hoyEnEspana(t))
	if filepath.Dir(result.ArchivoEscrito) != esperado {
		t.Errorf("se esperaba que el archivo quedara en %s, quedo en %s", esperado, filepath.Dir(result.ArchivoEscrito))
	}
}

func TestStreamWriter_ColisionDeNombreAgregaSufijo(t *testing.T) {
	dir := t.TempDir()
	nuevo := func() string {
		sw, err := reporting.NewStreamWriter(dir, "reporte-santander", false, 0)
		if err != nil {
			t.Fatalf("no se esperaba error: %v", err)
		}
		sw.Add(sampleDocs()[0])
		result, err := sw.Finish()
		if err != nil {
			t.Fatalf("no se esperaba error: %v", err)
		}
		return result.ArchivoEscrito
	}

	r1 := nuevo()
	r2 := nuevo()
	r3 := nuevo()

	if r1 == r2 || r2 == r3 {
		t.Fatalf("cada corrida tendria que haber generado un nombre distinto: %s / %s / %s", r1, r2, r3)
	}
	if !strings.HasSuffix(r2, "_1.json") {
		t.Errorf("se esperaba que la segunda corrida agregara el sufijo _1, se obtuvo: %s", r2)
	}
	if !strings.HasSuffix(r3, "_2.json") {
		t.Errorf("se esperaba que la tercera corrida agregara el sufijo _2, se obtuvo: %s", r3)
	}
}

func TestStreamWriter_Abort_BorraElArchivoParcial(t *testing.T) {
	dir := t.TempDir()
	sw, err := reporting.NewStreamWriter(dir, "reporte-interrumpido", false, 0)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if err := sw.Add(sampleDocs()[0]); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	sw.Abort()

	entries, err := os.ReadDir(filepath.Join(dir, hoyEnEspana(t)))
	if err != nil {
		if os.IsNotExist(err) {
			return // la carpeta ni se creo con contenido -- tambien valido
		}
		t.Fatalf("error leyendo la carpeta de salida: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Abort tendria que haber borrado el archivo parcial, quedo: %v", entries)
	}
}
