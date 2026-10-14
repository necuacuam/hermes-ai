// Comando hermes-mcp: el MCP Server de Hermes AI. Se comunica por stdio
// (lo que espera Claude Desktop de un servidor MCP local) y solo ejecuta
// consultas de lectura contra MongoDB (ver store.ValidatePipeline).
//
// Configuracion (detalle en docs/03-instalacion-y-configuracion.md):
//   - HERMES_MCP_MONGO_URI, obligatoria, y el resto de las variables de
//     conexion y limites van en un ".env" al lado del ejecutable (ver
//     loadDotEnv). No hay valores por defecto para la conexion en el codigo.
//   - HERMES_MCP_RELACIONES_PATH, obligatoria, va en el bloque "env" de
//     claude_desktop_config.json: dice que fichero de conocimiento usa cada
//     instalacion, asi que no pertenece a un ".env" que podria compartirse.
//     Para arrancar sin Claude Desktop, se define en la sesion.
package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/mcp"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store/mongostore"
	"hermes-mcp/internal/tools"
)

// timeoutPorDefecto es el tiempo maximo de toda ejecucion de un pipeline si
// HERMES_MCP_MONGO_TIMEOUT no esta definida.
const timeoutPorDefecto = 30 * time.Second

// resolverRutaConfig busca una ruta de configuracion (por ejemplo
// HERMES_MCP_RELACIONES_PATH) con el mismo criterio que loadDotEnv: si es
// absoluta se usa tal cual; si es relativa, se prueba primero contra el
// directorio de trabajo y despues contra el del ejecutable, porque Claude
// Desktop lo lanza con un directorio de trabajo que no se controla. Si no
// existe en ninguno, devuelve el nombre sin resolver, para que el error de
// LoadRelations muestre la ruta pedida.
func resolverRutaConfig(nombre string) string {
	if filepath.IsAbs(nombre) {
		return nombre
	}
	if cwd, err := os.Getwd(); err == nil {
		candidato := filepath.Join(cwd, nombre)
		if _, err := os.Stat(candidato); err == nil {
			return candidato
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidato := filepath.Join(filepath.Dir(exe), nombre)
		if _, err := os.Stat(candidato); err == nil {
			return candidato
		}
	}
	return nombre
}

// loadDotEnv carga ".env" en las variables de entorno del proceso, antes de
// que main las lea. Asi el .env es el unico lugar de la configuracion de
// conexion, se arranque el servidor como se arranque.
//
// Busca ".env" en el directorio de trabajo (arranque con "go run" desde la
// raiz del repo) y en el del ejecutable (arranque desde Claude Desktop).
// Una variable ya definida en el entorno real siempre gana: el loader solo
// completa las que faltan. Formato simple: clave=valor, sin comillas ni
// multilinea, y "#" al inicio de linea es comentario. Si no hay ".env", no
// es un error.
func loadDotEnv(logger *log.Logger) {
	var candidatos []string
	if cwd, err := os.Getwd(); err == nil {
		candidatos = append(candidatos, filepath.Join(cwd, ".env"))
	}
	if exe, err := os.Executable(); err == nil {
		candidatos = append(candidatos, filepath.Join(filepath.Dir(exe), ".env"))
	}

	for _, ruta := range candidatos {
		f, err := os.Open(ruta)
		if err != nil {
			continue // no existe en este candidato -- probar el siguiente, o seguir sin .env
		}
		aplicarDotEnv(f, logger, ruta)
		f.Close()
	}
}

func aplicarDotEnv(f *os.File, logger *log.Logger, ruta string) {
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		linea := strings.TrimSpace(scanner.Text())
		if linea == "" || strings.HasPrefix(linea, "#") {
			continue
		}
		partes := strings.SplitN(linea, "=", 2)
		if len(partes) != 2 {
			continue
		}
		clave := strings.TrimSpace(partes[0])
		valor := strings.TrimSpace(partes[1])
		if clave == "" {
			continue
		}
		if _, yaSeteada := os.LookupEnv(clave); yaSeteada {
			continue // una variable real del entorno siempre gana sobre .env
		}
		os.Setenv(clave, valor)
	}
	if err := scanner.Err(); err != nil {
		logger.Printf("aviso: error leyendo %s: %v", ruta, err)
	}
}

func main() {
	// Los registros van a stderr: stdout es exclusivo de los mensajes
	// JSON-RPC del protocolo MCP, y escribir otra cosa lo rompe.
	logger := log.New(os.Stderr, "[hermes-mcp] ", log.LstdFlags)

	loadDotEnv(logger)

	salidasDir := os.Getenv("HERMES_MCP_SALIDAS_DIR")
	if salidasDir == "" {
		salidasDir = "./salidas"
	}

	// Obligatoria y sin valor por defecto: la configuracion vive en el .env,
	// no en el codigo. No puede quedar vacia: reporting siempre necesita una
	// base real.
	mongoURI := os.Getenv("HERMES_MCP_MONGO_URI")
	if mongoURI == "" {
		logger.Fatalf("HERMES_MCP_MONGO_URI no esta seteada -- no hay default embebido en el codigo. Configurala en .env (ver .env.example) con la URI real.")
	}

	// HERMES_MCP_RELACIONES_PATH: ruta al fichero de conocimiento.
	// Obligatoria: sin el fichero no hay nada util que responder, y es mejor
	// cortar al arrancar con un error claro. Va en claude_desktop_config.json,
	// no en el .env (ver el comentario de cabecera).

	relacionesPath, relacionesPathSeteada := os.LookupEnv("HERMES_MCP_RELACIONES_PATH")
	if !relacionesPathSeteada || relacionesPath == "" {
		logger.Fatalf("HERMES_MCP_RELACIONES_PATH no esta seteada -- hace falta la ruta al fichero de relaciones a cargar. Configurala en el bloque \"env\" de claude_desktop_config.json (ver README.md, \"Configurar Claude Desktop\"), o en la sesion de shell si corres localmente sin Claude Desktop. Copia relaciones/relaciones.example.json a esa ruta antes, completando origenDatos.")
	}
	rel, err := dictionary.LoadRelations(resolverRutaConfig(relacionesPath))
	if err != nil {
		logger.Fatalf("no se pudo cargar el fichero de relaciones: %v", err)
	}

	dbName := os.Getenv("HERMES_MCP_MONGO_DB")
	if dbName == "" {
		dbName = "hermes-local"
	}
	ms, err := mongostore.New(context.Background(), mongoURI, dbName)
	if err != nil {
		logger.Fatalf("no se pudo conectar a Mongo (%s, db %s): %v", mongostore.URISinClave(mongoURI), dbName, err)
	}
	logger.Printf("arrancando contra Mongo real en %s (db %s)", mongostore.URISinClave(mongoURI), dbName)

	limitePantalla := reporting.LimitePantallaPorDefecto
	if raw := os.Getenv("HERMES_MCP_LIMITE_PANTALLA"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limitePantalla = v
		} else {
			logger.Printf("HERMES_MCP_LIMITE_PANTALLA=%q invalida (tiene que ser un entero > 0) -- usando el default %d", raw, limitePantalla)
		}
	}

	if raw := os.Getenv("HERMES_MCP_ZONA_HORARIA"); raw != "" {
		if err := reporting.SetZonaHoraria(raw); err != nil {
			logger.Printf("HERMES_MCP_ZONA_HORARIA=%q invalida (%v) -- usando %s", raw, err, reporting.ZonaHorariaPorDefecto)
		}
	}

	timeout := timeoutPorDefecto
	if raw := os.Getenv("HERMES_MCP_MONGO_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		} else {
			logger.Printf("HERMES_MCP_MONGO_TIMEOUT=%q invalida (formato tipo \"30s\", \"2m\") -- usando el default %s", raw, timeout)
		}
	}

	server := mcp.NewServer("hermes-mcp", "0.3.0-pipeline", logger)
	tools.RegisterAll(server, ms, salidasDir, limitePantalla, timeout, rel)

	logger.Printf("salidas en %s -- limite de pantalla %d filas (fijo, el agente no lo cambia), timeout de consulta %s", salidasDir, limitePantalla, timeout)

	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		logger.Fatalf("error fatal: %v", err)
	}
}
