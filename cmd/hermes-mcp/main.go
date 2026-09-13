// Comando hermes-mcp: servidor MCP de entidades Hermes (holding, grupo,
// cliente, centro, punto de servicio, direccion), pensado para correr
// localmente (laptop corporativo) contra el Mongo local ya cargado con los
// datos reales que dio el jefe, y conectarse desde Claude Desktop.
//
// Backend de datos: usa Mongo real (internal/store/mongostore) -- ESA base
// nunca se toca en escritura (ver internal/store.ValidatePipeline: solo se
// ejecutan agregaciones de lectura). Si HERMES_MCP_MONGO_URI no esta
// seteada en absoluto, se usa por default la URI del Mongo local de este
// mismo repo (ver docker-compose.yml -- usuario/clave fijos de una
// instancia de desarrollo que no sale de la laptop, ya visibles en ese
// archivo versionado, asi que no es un secreto nuevo). Con eso alcanza
// para el uso normal: no hace falta configurar nada aparte en Claude
// Desktop. Si se cambia la clave en docker-compose.yml (recomendado antes
// de exponer el puerto a la LAN), actualizar tambien el default de aca --
// unica fuente de verdad. Si se necesita otra URI (otro Mongo, otro
// puerto/host), se puede seguir seteando la variable explicitamente para
// pisar el default.
//
// Para forzar mockstore (datos hardcodeados en Go, SIN pipelines de
// agregacion soportados -- solo util para correr sin Mongo a mano, ej. los
// tests de Go), setear HERMES_MCP_MONGO_URI a una cadena vacia
// explicitamente (no simplemente omitirla).
//
// Transporte: siempre stdio (lo que Claude Desktop espera de un MCP local).
// Hasta el 2026-09-10 este binario tambien podia servir por TCP en dos
// modos (HERMES_MCP_LISTEN_ADDR / HERMES_MCP_DIAL_ADDR) para un tunel SSH
// inverso entre dos laptops -- se saco (ver internal/bridge) porque ya no
// aplica: el Mongo local de esta v1 tiene todo lo necesario, todo corre en
// una sola maquina.
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

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

func main() {
	// Los logs van a stderr -- stdout esta reservado exclusivamente para
	// los mensajes JSON-RPC del protocolo MCP. Escribir cualquier otra cosa
	// a stdout rompe el protocolo.
	logger := log.New(os.Stderr, "[hermes-mcp] ", log.LstdFlags)

	salidasDir := os.Getenv("HERMES_MCP_SALIDAS_DIR")
	if salidasDir == "" {
		salidasDir = "./salidas"
	}

	// LookupEnv (no Getenv) porque hace falta distinguir "la variable no
	// esta seteada en absoluto" (usar el default de localhost) de "esta
	// seteada explicitamente a vacio" (forzar mockstore a proposito).
	const mongoURIPorDefecto = "mongodb://hermes:cambiame-antes-de-exponer@localhost:27017/?authSource=admin"
	mongoURI, uriSeteada := os.LookupEnv("HERMES_MCP_MONGO_URI")
	if !uriSeteada {
		mongoURI = mongoURIPorDefecto
	}

	var st store.Store
	if mongoURI != "" {
		dbName := os.Getenv("HERMES_MCP_MONGO_DB")
		if dbName == "" {
			dbName = "hermes-local"
		}
		ms, err := mongostore.New(context.Background(), mongoURI, dbName)
		if err != nil {
			logger.Fatalf("no se pudo conectar a Mongo (%s, db %s): %v", mongoURI, dbName, err)
		}
		st = ms
		logger.Printf("arrancando contra Mongo real en %s (db %s)", mongoURI, dbName)
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

	timeout := timeoutPorDefecto
	if raw := os.Getenv("HERMES_MCP_MONGO_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		} else {
			logger.Printf("HERMES_MCP_MONGO_TIMEOUT=%q invalida (formato tipo \"30s\", \"2m\") -- usando el default %s", raw, timeout)
		}
	}

	server := mcp.NewServer("hermes-mcp", "0.3.0-pipeline", logger)
	tools.RegisterAll(server, g, salidasDir, limitePantalla, timeout)

	logger.Printf("salidas en %s -- limite de pantalla por defecto %d filas, timeout de pipeline %s", salidasDir, limitePantalla, timeout)

	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		logger.Fatalf("error fatal: %v", err)
	}
}
