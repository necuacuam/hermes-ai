// Comando hermes-mcp: el MCP Server de Hermes AI. Solo ejecuta consultas de
// lectura contra MongoDB (ver store.ValidatePipeline). Se comunica por stdio
// (lo que espera Claude Desktop de un servidor MCP local) o por HTTP
// (HERMES_MCP_TRANSPORTE=http, para clientes remotos como Open WebUI).
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
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/onedrive"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store/mongostore"
	"hermes-mcp/internal/tools"
)

// timeoutPorDefecto es el tiempo maximo de toda ejecucion de un pipeline si
// HERMES_MCP_MONGO_TIMEOUT no esta definida.
const timeoutPorDefecto = 30 * time.Second

// direccionHTTPPorDefecto es donde escucha el transporte HTTP si
// HERMES_MCP_HTTP_DIRECCION no esta definida.
const direccionHTTPPorDefecto = ":8080"

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

	s := server.NewMCPServer("hermes-mcp", "0.3.0-pipeline",
		server.WithToolCapabilities(false),
		server.WithInstructions(tools.Instrucciones),
	)
	transporte := os.Getenv("HERMES_MCP_TRANSPORTE")
	destino := destinoOneDrive(logger, transporte)
	tools.RegisterAll(s, ms, salidasDir, limitePantalla, timeout, rel, destino)

	logger.Printf("salidas en %s -- limite de pantalla %d filas (fijo, el agente no lo cambia), timeout de consulta %s", salidasDir, limitePantalla, timeout)

	switch transporte {
	case "", "stdio":
		err = server.ServeStdio(s, server.WithErrorLogger(logger))
	case "http":
		// El token del usuario todavia no se valida: se acepta cualquier
		// llamada para consultar. No exponer fuera de una red de confianza
		// (ver docs/03-instalacion-y-configuracion.md).
		direccion := os.Getenv("HERMES_MCP_HTTP_DIRECCION")
		if direccion == "" {
			direccion = direccionHTTPPorDefecto
		}
		diagnostico := os.Getenv("HERMES_MCP_DIAGNOSTICO_AUTH") == "true"
		// El token del usuario viaja en el contexto de cada llamada, para
		// guardar los archivos en su nombre.
		conToken := server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			cabecera := r.Header.Get("Authorization")
			if diagnostico {
				// Solo para el lab: si llega el token y para quien fue
				// emitido, sin el token ni datos personales.
				logger.Printf("diagnostico auth: %s", describirAutorizacion(cabecera))
			}
			return onedrive.ConToken(ctx, onedrive.TokenDeCabecera(cabecera))
		})
		logger.Printf("MCP por HTTP en %s/mcp", direccion)
		err = server.NewStreamableHTTPServer(s, conToken).Start(direccion)
	default:
		logger.Fatalf("HERMES_MCP_TRANSPORTE=%q invalida -- usar \"stdio\" o \"http\"", transporte)
	}
	if err != nil {
		logger.Fatalf("error fatal: %v", err)
	}
}

// destinoOneDrive arma el destino de los archivos de reporting en el
// OneDrive del usuario si estan HERMES_MCP_ONEDRIVE_CLIENT_ID y
// HERMES_MCP_ONEDRIVE_SECRETO (registro de hermes-mcp en Entra ID).
// HERMES_MCP_ONEDRIVE_TENANTS (opcional) limita los tenants de los usuarios,
// separados por comas. Solo aplica al transporte HTTP, que es el que recibe
// el token del usuario; sin destino, los archivos quedan en la carpeta de
// salidas.
func destinoOneDrive(logger *log.Logger, transporte string) tools.DestinoArchivos {
	clientID := os.Getenv("HERMES_MCP_ONEDRIVE_CLIENT_ID")
	secreto := os.Getenv("HERMES_MCP_ONEDRIVE_SECRETO")
	if clientID == "" && secreto == "" {
		return nil
	}
	if clientID == "" || secreto == "" {
		logger.Fatalf("OneDrive: faltan variables -- hacen falta HERMES_MCP_ONEDRIVE_CLIENT_ID y HERMES_MCP_ONEDRIVE_SECRETO")
	}
	var tenants []string
	if raw := os.Getenv("HERMES_MCP_ONEDRIVE_TENANTS"); raw != "" {
		tenants = strings.Split(raw, ",")
	}
	if transporte != "http" {
		logger.Printf("OneDrive configurado pero el transporte no es HTTP: no hay token del usuario, los archivos quedan en la carpeta de salidas")
		return nil
	}
	d, err := onedrive.New(onedrive.Config{
		ClientID:          clientID,
		Secreto:           secreto,
		TenantsPermitidos: tenants,
	})
	if err != nil {
		logger.Fatalf("OneDrive: %v", err)
	}
	logger.Printf("archivos de reporting: OneDrive del usuario (carpeta de la aplicacion)")
	return d
}
