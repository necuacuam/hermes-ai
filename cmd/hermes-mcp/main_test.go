package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// testLogger devuelve un *log.Logger que descarta todo -- estos tests no
// verifican nada de lo que se loguea, solo el efecto en el entorno.
func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// Cubre aplicarDotEnv (agregado 2026-09-16, ver TAvo): antes de esto el
// binario compilado nunca leia ".env" en absoluto, asi que Claude Desktop
// (que lanza el .exe directo) terminaba dependiendo de defaults
// hardcodeados o del bloque "env" de claude_desktop_config.json segun la
// variable -- dos lugares distintos para la misma pregunta ("que Mongo
// esta usando esto ahora"). Estos tests confirman las dos garantias que
// importan: que .env realmente rellena el entorno, y que nunca pisa algo
// que ya estaba seteado.

func escribirEnvTemporal(t *testing.T, contenido string) *os.File {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(ruta, []byte(contenido), 0o600); err != nil {
		t.Fatalf("no se pudo escribir .env de prueba: %v", err)
	}
	f, err := os.Open(ruta)
	if err != nil {
		t.Fatalf("no se pudo abrir .env de prueba: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestAplicarDotEnv_SeteaVariablesNuevas(t *testing.T) {
	os.Unsetenv("HERMES_MCP_TEST_NUEVA")
	defer os.Unsetenv("HERMES_MCP_TEST_NUEVA")

	f := escribirEnvTemporal(t, "HERMES_MCP_TEST_NUEVA=valor de prueba\n")
	aplicarDotEnv(f, testLogger(), "test.env")

	if got := os.Getenv("HERMES_MCP_TEST_NUEVA"); got != "valor de prueba" {
		t.Errorf("HERMES_MCP_TEST_NUEVA = %q, se esperaba %q", got, "valor de prueba")
	}
}

func TestAplicarDotEnv_NuncaPisaUnaVariableYaSeteada(t *testing.T) {
	os.Setenv("HERMES_MCP_TEST_YA_SETEADA", "valor real del entorno")
	defer os.Unsetenv("HERMES_MCP_TEST_YA_SETEADA")

	f := escribirEnvTemporal(t, "HERMES_MCP_TEST_YA_SETEADA=valor del archivo .env\n")
	aplicarDotEnv(f, testLogger(), "test.env")

	if got := os.Getenv("HERMES_MCP_TEST_YA_SETEADA"); got != "valor real del entorno" {
		t.Errorf("una variable ya seteada en el entorno real no deberia pisarse por .env -- quedo %q", got)
	}
}

func TestAplicarDotEnv_IgnoraComentariosYLineasVacias(t *testing.T) {
	os.Unsetenv("HERMES_MCP_TEST_CON_COMENTARIOS")
	defer os.Unsetenv("HERMES_MCP_TEST_CON_COMENTARIOS")

	f := escribirEnvTemporal(t, "# esto es un comentario\n\nHERMES_MCP_TEST_CON_COMENTARIOS=ok\n")
	aplicarDotEnv(f, testLogger(), "test.env")

	if got := os.Getenv("HERMES_MCP_TEST_CON_COMENTARIOS"); got != "ok" {
		t.Errorf("HERMES_MCP_TEST_CON_COMENTARIOS = %q, se esperaba %q", got, "ok")
	}
}

func TestAplicarDotEnv_ValorConEspaciosYBarrasSinComillas(t *testing.T) {
	// Caso real de este proyecto: una ruta de Windows con espacio en el
	// nombre de carpeta ("Hermes AI"), sin comillas -- tiene que sobrevivir
	// tal cual, el parser solo separa por el primer "=".
	os.Unsetenv("HERMES_MCP_TEST_RUTA")
	defer os.Unsetenv("HERMES_MCP_TEST_RUTA")

	valorEsperado := `D:\sharepoint\mulesoft\Hermes AI\mcp-server\generatedfiles`
	f := escribirEnvTemporal(t, "HERMES_MCP_TEST_RUTA="+valorEsperado+"\n")
	aplicarDotEnv(f, testLogger(), "test.env")

	if got := os.Getenv("HERMES_MCP_TEST_RUTA"); got != valorEsperado {
		t.Errorf("HERMES_MCP_TEST_RUTA = %q, se esperaba %q", got, valorEsperado)
	}
}
