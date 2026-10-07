// Comando hermes-mcp: servidor MCP de entidades Hermes (holding, grupo,
// cliente, centro, punto de servicio, direccion), pensado para correr
// localmente (laptop corporativo) contra el Mongo local ya cargado con los
// datos reales que dio el jefe, y conectarse desde Claude Desktop.
//
// Configuracion: la mayoria de las variables HERMES_MCP_* (Mongo, salidas,
// topes de recursos) se setean en un archivo ".env" al lado del ejecutable
// (ver loadDotEnv mas abajo), sea cual sea la forma de arrancar el
// servidor (Claude Desktop, "go run", los scripts de scripts\).
//
// HERMES_MCP_RELACIONES_PATH es la excepcion: su lugar es el bloque "env"
// de claude_desktop_config.json, NO ".env" -- es la variable que dice cual
// instalacion de Claude Desktop usa cual fichero de relaciones, asi que
// vive en la config de esa instalacion particular, no en un ".env" que
// podria terminar compartido entre varias. Para desarrollo local (go run,
// scripts\run-hermes-mcp.cmd) sin pasar por Claude Desktop, setearla a
// mano en la sesion de shell antes de arrancar.
//
// Backend de datos: usa Mongo real (internal/store/mongostore) -- ESA base
// nunca se toca en escritura (ver internal/store.ValidatePipeline: solo se
// ejecutan agregaciones de lectura). HERMES_MCP_MONGO_URI es OBLIGATORIA --
// ya no hay ningun default embebido en el codigo (se saco a proposito el
// 2026-09-16, ver TAvo: habia dos lugares distintos con la misma
// informacion -- este default y .env -- y eso es justo lo que se querian
// evitar). Si la variable no esta seteada en absoluto (ni en el entorno
// real, ni en .env), el servidor no arranca y corta con un error pidiendo
// que se configure -- ver .env.example para la URI del Mongo local de
// este repo (docker-compose.yml).
//
// Para forzar mockstore (datos hardcodeados en Go, SIN pipelines de
// agregacion soportados -- solo util para correr sin Mongo a mano, ej. los
// tests de Go), setear HERMES_MCP_MONGO_URI a una cadena vacia
// explicitamente (no simplemente omitirla -- omitirla es el error de
// arriba, no mockstore).
//
// HERMES_MCP_RELACIONES_PATH tambien es OBLIGATORIA -- ruta al fichero de
// relaciones a cargar, dentro de la carpeta relaciones/ (ver
// internal/dictionary/dictionary.go, RelationsData/LoadRelations, y
// relaciones/relaciones.example.json) -- puede haber varios ahi (uno por
// entorno/release), esta variable dice cual usar. Trae tanto la
// estructura/notas de negocio de "relacion_entre_entidades" como
// origenDatos. Mismo motivo que MONGO_URI: sin esto no hay nada util que
// devolver. A DIFERENCIA de MONGO_URI, esta NO se configura en ".env" --
// va en el bloque "env" de claude_desktop_config.json (ver README.md),
// porque es la variable que distingue una instalacion de Claude Desktop
// de otra, no una config de conexion compartida.
//
// Transporte: siempre stdio (lo que Claude Desktop espera de un MCP local).
// Hasta el 2026-09-10 este binario tambien podia servir por TCP en dos
// modos (HERMES_MCP_LISTEN_ADDR / HERMES_MCP_DIAL_ADDR) para un tunel SSH
// inverso entre dos laptops -- se saco (ver internal/bridge) porque ya no
// aplica: el Mongo local de esta v1 tiene todo lo necesario, todo corre en
// una sola maquina.
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
	"hermes-mcp/internal/graph"
	"hermes-mcp/internal/mcp"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store"
	"hermes-mcp/internal/store/mockstore"
	"hermes-mcp/internal/store/mongostore"
	"hermes-mcp/internal/tools"
)

// timeoutPorDefecto es el tope de tiempo para CUALQUIER ejecucion de
// pipeline (conteo o listado, pantalla o archivo) si HERMES_MCP_MONGO_TIMEOUT
// no esta seteada -- ver la nota de TAvo (2026-09-11) de configurar el
// timeout junto con el limite de pantalla, como dos topes de recursos que
// se piensan y configuran juntos.
const timeoutPorDefecto = 30 * time.Second

// loadDotEnv carga ".env" a las variables de entorno reales del proceso,
// ANTES de que el resto de main() las lea. Se agrego el 2026-09-16 (ver
// TAvo) porque hasta entonces el binario compilado NUNCA leia ".env" --
// solo lo hacian los scripts run-hermes-mcp.cmd/.ps1 (parseandolo a mano y
// llamando "go run"), asi que cuando Claude Desktop lanza hermes-mcp.exe
// directo (el uso real, ver claude_desktop_config.json), ".env" quedaba
// completamente ignorado y la unica fuente de verdad terminaba siendo,
// segun la variable, el default hardcodeado de mas abajo o el bloque "env"
// de claude_desktop_config.json -- dos lugares distintos segun el caso,
// justo la confusion que esto resuelve. Con este cambio, ".env" es UN
// SOLO lugar valido para las variables de conexion (MONGO_URI, MONGO_DB,
// SALIDAS_DIR) sea cual sea la forma de arrancar el servidor.
// HERMES_MCP_RELACIONES_PATH queda deliberadamente afuera de esta lista --
// ver el comentario de mas arriba, en la cabecera del archivo, sobre por
// que esa variable vive en claude_desktop_config.json y no aca.
//
// Busca ".env" en dos lugares (en ese orden, se queda con el primero que
// encuentre por variable): el directorio de trabajo actual (cubre "go run"
// desde la raiz del repo) y el directorio del propio ejecutable (cubre
// Claude Desktop, que lanza el .exe con un cwd que no controlamos, pero SI
// controlamos donde vive el .exe -- al lado de este mismo ".env").
//
// Una variable YA seteada en el entorno real (por el SO, por Claude
// Desktop en su bloque "env", o a mano) SIEMPRE gana sobre ".env" -- este
// loader nunca pisa algo que ya este seteado, solo rellena lo que falta.
// Parser deliberadamente simple (split por el primer "=", como ya hace
// run-hermes-mcp.cmd): sin comillas, sin multilinea, "#" al inicio de
// linea es comentario. Si ".env" no existe en ninguno de los dos lugares,
// no es un error -- el servidor sigue con los defaults de siempre.

// resolverRutaConfig busca "nombre" (tal como vino de una variable de
// entorno, ej. HERMES_MCP_RELACIONES_PATH) con el mismo criterio que
// loadDotEnv busca ".env": si es una ruta absoluta se usa tal cual; si es
// relativa, se prueba primero contra el directorio de trabajo actual y
// despues contra el directorio del propio ejecutable -- para que Claude
// Desktop (que lanza el .exe con un cwd que no controlamos) igual lo
// encuentre si vive al lado del binario. Si ninguno de los dos candidatos
// existe, devuelve "nombre" tal cual (sin resolver) para que el error de
// LoadRelations sea claro sobre que ruta probo.
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
	// Los logs van a stderr -- stdout esta reservado exclusivamente para
	// los mensajes JSON-RPC del protocolo MCP. Escribir cualquier otra cosa
	// a stdout rompe el protocolo.
	logger := log.New(os.Stderr, "[hermes-mcp] ", log.LstdFlags)

	loadDotEnv(logger)

	salidasDir := os.Getenv("HERMES_MCP_SALIDAS_DIR")
	if salidasDir == "" {
		salidasDir = "./salidas"
	}

	// LookupEnv (no Getenv) porque hace falta distinguir "la variable no
	// esta seteada en absoluto" (error -- ya no hay default embebido aca,
	// ver TAvo 2026-09-16: .env es la unica fuente de verdad, no el
	// codigo) de "esta seteada explicitamente a vacio" (forzar mockstore a
	// proposito, eso si sigue soportado).
	mongoURI, uriSeteada := os.LookupEnv("HERMES_MCP_MONGO_URI")
	if !uriSeteada {
		logger.Fatalf("HERMES_MCP_MONGO_URI no esta seteada -- no hay default embebido en el codigo. Configurala en .env (ver .env.example) con la URI real, o seteala explicitamente a vacio ahi mismo para forzar mockstore (datos de prueba, sin pipelines de agregacion).")
	}

	// HERMES_MCP_RELACIONES_PATH: ruta al fichero de relaciones a cargar,
	// dentro de la carpeta relaciones/ -- ver dictionary.RelationsData/
	// LoadRelations y relaciones/relaciones.example.json. Ahi vive tanto la estructura/notas de
	// negocio de "relacion_entre_entidades" como origenDatos (texto libre,
	// decidido por quien levanta el servidor, para que el agente sepa de
	// que base/entorno viene lo conectado AHORA sin tener que asumirlo de
	// un texto fijo en el prompt -- eso fue justo el bug que motivo esto,
	// ver dictionary.go/origenDatosNoConfigurado). Obligatoria, mismo
	// criterio que HERMES_MCP_MONGO_URI: sin el fichero no hay nada util
	// que devolver, mejor cortar al arrancar con un error claro. A
	// diferencia de MONGO_URI, esta variable vive en el bloque "env" de
	// claude_desktop_config.json, NO en ".env" (ver el comentario de
	// cabecera del archivo y README.md) -- distingue una instalacion de
	// Claude Desktop de otra, no es config de conexion compartida.
	//
	// Se carga ACA, ANTES de construir el MongoStore (2026-09-28) -- porque
	// MongoStore ahora necesita rel.CamposDeNombre para saber, por tipo de
	// entidad, en que campos buscar por nombre (ver mongostore.New): ese
	// dato tambien es contenido de negocio externalizado, ya no un var
	// hardcodeado en mongostore.go.
	relacionesPath, relacionesPathSeteada := os.LookupEnv("HERMES_MCP_RELACIONES_PATH")
	if !relacionesPathSeteada || relacionesPath == "" {
		logger.Fatalf("HERMES_MCP_RELACIONES_PATH no esta seteada -- hace falta la ruta al fichero de relaciones a cargar. Configurala en el bloque \"env\" de claude_desktop_config.json (ver README.md, \"Configurar Claude Desktop\"), o en la sesion de shell si corres localmente sin Claude Desktop. Copia relaciones/relaciones.example.json a esa ruta antes, completando origenDatos.")
	}
	rel, err := dictionary.LoadRelations(resolverRutaConfig(relacionesPath))
	if err != nil {
		logger.Fatalf("no se pudo cargar el fichero de relaciones: %v", err)
	}

	var st store.Store
	if mongoURI != "" {
		dbName := os.Getenv("HERMES_MCP_MONGO_DB")
		if dbName == "" {
			dbName = "hermes-local"
		}
		ms, err := mongostore.New(context.Background(), mongoURI, dbName, rel.CamposDeNombre)
		if err != nil {
			logger.Fatalf("no se pudo conectar a Mongo (%s, db %s): %v", mongostore.URISinClave(mongoURI), dbName, err)
		}
		st = ms
		logger.Printf("arrancando contra Mongo real en %s (db %s)", mongostore.URISinClave(mongoURI), dbName)
	} else {
		st = mockstore.New(mockstore.Fixtures())
		logger.Printf("HERMES_MCP_MONGO_URI seteada explicitamente a vacio -- arrancando con datos de prueba en memoria (mockstore), SIN pipelines de agregacion. Ver README.md para levantar Mongo local.")
	}

	g := graph.New(st)

	limitePantalla := reporting.LimitePantallaPorDefecto
	if raw := os.Getenv("HERMES_MCP_LIMITE_PANTALLA_DEFAULT"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limitePantalla = v
		} else {
			logger.Printf("HERMES_MCP_LIMITE_PANTALLA_DEFAULT=%q invalida (tiene que ser un entero > 0) -- usando el default %d", raw, limitePantalla)
		}
	}

	limiteSeek := store.LimiteSeekPorDefecto
	if raw := os.Getenv("HERMES_MCP_LIMITE_SEEK"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limiteSeek = v
		} else {
			logger.Printf("HERMES_MCP_LIMITE_SEEK=%q invalida (tiene que ser un entero > 0) -- usando el default %d", raw, limiteSeek)
		}
	}
	if ms, ok := st.(*mongostore.MongoStore); ok {
		ms.SetLimiteSeek(limiteSeek)
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
	tools.RegisterAll(server, g, salidasDir, limitePantalla, limiteSeek, timeout, rel)

	logger.Printf("salidas en %s -- limite de pantalla por defecto %d filas, limite de seek %d candidatos, timeout de consulta %s", salidasDir, limitePantalla, limiteSeek, timeout)

	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		logger.Fatalf("error fatal: %v", err)
	}
}
