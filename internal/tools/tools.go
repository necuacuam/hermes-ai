// Package tools conecta el protocolo MCP (paquete mcp) con dictionary,
// reporting y store. Registra tres herramientas: diccionario_de_campos y
// relacion_entre_entidades (no consultan la base) y reporting (ejecuta un
// pipeline de agregacion), el unico camino a los datos. Las descripciones
// de las herramientas son el contrato tecnico; como conversar con el
// usuario lo definen los prompts de los agentes.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/mcp"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store"
)

// RegisterAll registra las tres herramientas. limitePantalla y
// pipelineTimeout (HERMES_MCP_LIMITE_PANTALLA y HERMES_MCP_MONGO_TIMEOUT)
// son los dos topes de recursos de toda ejecucion de un pipeline.
func RegisterAll(server *mcp.Server, runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration, rel dictionary.RelationsData) {
	registerDictionary(server, rel)
	registerRelation(server, rel)
	registerReporting(server, runner, salidasDir, limitePantalla, pipelineTimeout)
}

func jsonSchema(schema string) json.RawMessage {
	return json.RawMessage(schema)
}

// tiposEnumJSON arma el "enum" de los schemas desde domain.AllTypes, para
// que sumar una entidad al registro no exija tocar las herramientas.
// conVacio agrega "" al principio (en diccionario_de_campos, vacio = todos).
func tiposEnumJSON(conVacio bool) string {
	vals := []string{}
	if conVacio {
		vals = append(vals, "")
	}
	for _, t := range domain.AllTypes {
		vals = append(vals, string(t))
	}
	b, _ := json.Marshal(vals)
	return string(b)
}

// tiposTexto lista los tipos para una descripcion: "a, b, ... o z".
func tiposTexto() string {
	n := len(domain.AllTypes)
	nombres := make([]string, n)
	for i, t := range domain.AllTypes {
		nombres[i] = string(t)
	}
	if n < 2 {
		return strings.Join(nombres, "")
	}
	return strings.Join(nombres[:n-1], ", ") + " o " + nombres[n-1]
}

// --- Diccionario de campos ---

type dictionaryArgs struct {
	Tipo string `json:"tipo"` // un tipo de domain.AllTypes, o "" (todos)
}

// dictionarySchema arma el schema de diccionario_de_campos con los tipos
// del registro (domain.AllTypes).
func dictionarySchema() json.RawMessage {
	return jsonSchema(strings.NewReplacer(
		"{{TIPOS_TEXTO}}", tiposTexto(),
		"{{TIPOS_ENUM}}", tiposEnumJSON(true),
	).Replace(`{
		"type": "object",
		"properties": {
			"tipo": {
				"type": "string",
				"description": "Tipo de entidad a consultar: {{TIPOS_TEXTO}}. Vacio para ver todos.",
				"enum": {{TIPOS_ENUM}}
			}
		}
	}`))
}

func registerDictionary(server *mcp.Server, rel dictionary.RelationsData) {
	server.RegisterTool(
		"diccionario_de_campos",
		"Devuelve, por tipo de entidad, los campos con su nombre tecnico y su significado, segun el fichero de conocimiento cargado. No consulta la base.",
		dictionarySchema(),
		dictionaryHandler(rel),
	)
}

// campoParaAgente es lo que el agente recibe de cada campo: nombre y
// significado. Fuente no se entrega: es metadata interna de mantenimiento,
// y si el agente la recibiera, solo una instruccion del prompt impediria
// que la repitiera.
type campoParaAgente struct {
	Campo       string `json:"campo"`
	Descripcion string `json:"descripcion"`
}

// dictionaryHandler es el handler de diccionario_de_campos, aparte para
// poder testearlo.
func dictionaryHandler(rel dictionary.RelationsData) mcp.ToolHandler {
	return func(raw json.RawMessage) mcp.CallToolResult {
		var args dictionaryArgs
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				return mcp.ErrorResult("argumentos invalidos: " + err.Error())
			}
		}
		result := map[domain.EntityType][]campoParaAgente{}
		for t, campos := range dictionary.Lookup(rel.Campos, domain.EntityType(args.Tipo)) {
			out := make([]campoParaAgente, len(campos))
			for i, c := range campos {
				out[i] = campoParaAgente{Campo: c.Campo, Descripcion: c.Descripcion}
			}
			result[t] = out
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return mcp.TextResult(string(b))
	}
}

// --- Relacion entre entidades ---
//
// No recibe argumentos ni consulta la base. Combina el registro tecnico
// (internal/domain: tipos, colecciones, tablas auxiliares) con el fichero de
// conocimiento cargado (relaciones, estructura de campos, origen de los
// datos).
func registerRelation(server *mcp.Server, rel dictionary.RelationsData) {
	server.RegisterTool(
		"relacion_entre_entidades",
		"Devuelve la estructura de las entidades: tipos, coleccion Mongo de cada tipo y tablas auxiliares habilitadas (del registro tecnico del servidor), y la lista de relaciones (padre, anidamiento, referencia y catalogo, cada una con sus campos de cruce, estado y reglas), la estructura de los documentos, los prefijos de ucode y el origen de los datos de la instalacion ('origenDatos') (del fichero de conocimiento cargado). No consulta la base.",
		jsonSchema(`{"type": "object", "properties": {}}`),
		func(raw json.RawMessage) mcp.CallToolResult {
			b, _ := json.MarshalIndent(dictionary.Hierarchy(rel), "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Reporting ---
//
// Ejecuta el pipeline que recibe, sin armar ni completar nada por su
// cuenta. Antes de ejecutar lo valida store.ValidatePipeline (etapas
// permitidas, colecciones del alcance y tablas auxiliares habilitadas).
//
// modo "conteo": el pipeline tiene que terminar en $count o un $group
// equivalente. 0 documentos equivale a cantidad 0; mas de un documento es
// un error. Nunca genera archivo.
//
// modo "listado": pantalla (recortada a limitePantalla filas, con aviso
// explicito si el total es mayor; el limite lo fija la configuracion del
// servidor y el agente no lo puede cambiar) y/o archivo JSON con el
// resultado completo. El nombre final del archivo (con sufijo _1, _2... si
// ya existia) se conoce despues de escribirlo. totalFilas siempre se
// informa, incluido 0. Sin documentos, no se crea archivo.
//
// El handler elige uno de tres caminos de ejecucion segun lo que se pidio,
// para no traer a memoria mas de lo necesario:
//   - modo "conteo": pipeline completo (siempre es 1 documento chico).
//   - "listado" solo a pantalla (sin guardarEnArchivo): se le agrega
//     {"$limit": limitePantalla+1} al pipeline antes de ejecutarlo: Mongo
//     corta ahi y no viaja nada de mas. El +1 permite avisar "hay mas" sin
//     un $count aparte.
//   - "listado" con guardarEnArchivo: RunPipelineStream + reporting.
//     StreamWriter: el archivo se escribe a medida que llegan los
//     documentos del cursor, sin acumular el resultado en memoria.
//
// Todos los caminos corren con un context.WithTimeout(pipelineTimeout): un
// pipeline mal armado (por ejemplo, un $lookup sin indice contra una
// coleccion grande) no puede colgar el servidor.
//
// Pipeline se recibe como texto crudo por etapa (json.RawMessage) y se
// ejecuta desde ese texto: decodificarlo a un map perderia el orden de las
// claves (ej. un $sort por dos campos llegaria a Mongo en orden alfabetico).
// Solo la validacion usa una copia decodificada a map, porque ahi el orden
// no importa.
type reportingArgs struct {
	ColeccionInicial  string            `json:"coleccionInicial"`
	Pipeline          []json.RawMessage `json:"pipeline"`
	Modo              string            `json:"modo"`
	MostrarEnPantalla bool              `json:"mostrarEnPantalla"`
	GuardarEnArchivo  bool              `json:"guardarEnArchivo"`
	NombreSugerido    string            `json:"nombreSugerido"`
}

// etapasParaValidar decodifica cada etapa a map solo para
// store.ValidatePipeline. Lo que se ejecuta sigue siendo el texto original.
func etapasParaValidar(pipeline []json.RawMessage) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, len(pipeline))
	for i, raw := range pipeline {
		if err := json.Unmarshal(raw, &out[i]); err != nil {
			return nil, fmt.Errorf("etapa %d: no es un objeto JSON valido: %w", i, err)
		}
	}
	return out, nil
}

// reportingSchema arma el schema de la tool con el limite de pantalla
// real, para que el agente lo vea en la descripcion de mostrarEnPantalla, y
// con los tipos de coleccionInicial tomados del registro (domain.AllTypes).
func reportingSchema(limitePantalla int) json.RawMessage {
	return jsonSchema(strings.NewReplacer(
		"{{LIMITE}}", strconv.Itoa(limitePantalla),
		"{{TIPOS_ENUM}}", tiposEnumJSON(false),
	).Replace(`{
		"type": "object",
		"required": ["coleccionInicial", "pipeline", "modo"],
		"properties": {
			"coleccionInicial": {
				"type": "string",
				"enum": {{TIPOS_ENUM}},
				"description": "Tipo de entidad cuya coleccion es el punto de partida del pipeline (ver 'coleccionMongo' en relacion_entre_entidades). Las tablas auxiliares habilitadas pueden usarse como destino de $lookup/$graphLookup, no como punto de partida."
			},
			"pipeline": {
				"type": "array",
				"items": {"type": "object"},
				"description": "Etapas de agregacion Mongo en orden, cada una un objeto de un solo operador (ej. {\"$match\": {...}}). Se lee como Extended JSON: admite $uuid, $binary, $date y $oid. Antes de ejecutar, el servidor valida que use solo etapas de consulta permitidas y colecciones del alcance o tablas auxiliares habilitadas; si no, devuelve un error que indica la etapa rechazada."
			},
			"modo": {
				"type": "string",
				"enum": ["listado", "conteo"],
				"description": "'conteo': el pipeline debe terminar en $count o un $group equivalente; 0 documentos equivale a cantidad 0 y mas de un documento es un error; nunca genera archivo. 'listado': devuelve documentos, en pantalla y/o en archivo."
			},
			"mostrarEnPantalla": {
				"type": "boolean",
				"description": "true para devolver las filas en la respuesta: como maximo {{LIMITE}} (limite fijo del servidor, no se puede cambiar), con aviso explicito si el total es mayor. Para el resultado completo, guardarEnArchivo."
			},
			"guardarEnArchivo": {
				"type": "boolean",
				"description": "true para guardar el resultado completo en un archivo JSON local, sin el limite de pantalla. Ignorado si modo=conteo. Si el pipeline no devuelve documentos, no se crea archivo: la respuesta trae totalFilas=0 y archivoEscrito vacio."
			},
			"nombreSugerido": {
				"type": "string",
				"description": "Nombre base del archivo. Si ya existe uno con ese nombre en el dia, se le agrega _1, _2, etc. El nombre final viene en la respuesta."
			}
		}
	}`))
}

func registerReporting(server *mcp.Server, runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration) {

	server.RegisterTool(
		"reporting",
		"Ejecuta un pipeline de agregacion Mongo tal cual lo recibe, partiendo de la coleccion de 'coleccionInicial'. Antes de ejecutar valida que use solo etapas de consulta permitidas y colecciones del alcance o tablas auxiliares habilitadas; cada consulta tiene un tiempo maximo. modo='conteo' devuelve un numero (nunca archivo); modo='listado' devuelve en pantalla como maximo "+strconv.Itoa(limitePantalla)+" filas (limite fijo del servidor, con aviso explicito si hay mas) y/o un archivo JSON con el resultado completo.",
		reportingSchema(limitePantalla),
		reportingHandler(runner, salidasDir, limitePantalla, pipelineTimeout),
	)
}

// reportingHandler es el handler de la tool reporting, aparte de
// registerReporting para poder testearlo con un PipelineRunner falso.
// limitePantalla es el maximo de filas en pantalla: lo fija la
// configuracion (HERMES_MCP_LIMITE_PANTALLA) y ningun argumento lo cambia;
// un "limitePantalla" que mande el agente se ignora.
func reportingHandler(runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration) mcp.ToolHandler {
	if limitePantalla <= 0 {
		limitePantalla = reporting.LimitePantallaPorDefecto
	}
	return func(raw json.RawMessage) mcp.CallToolResult {
		var args reportingArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return mcp.ErrorResult("argumentos invalidos: " + err.Error())
		}
		if args.ColeccionInicial == "" || len(args.Pipeline) == 0 || args.Modo == "" {
			return mcp.ErrorResult("faltan campos obligatorios: 'coleccionInicial', 'pipeline' y 'modo'.")
		}
		modo := reporting.Modo(args.Modo)
		if modo != reporting.ModoConteo && modo != reporting.ModoListado {
			return mcp.ErrorResult(fmt.Sprintf("modo %q invalido -- usar \"conteo\" o \"listado\"", args.Modo))
		}

		if err := store.ValidarClavesUnicas(args.Pipeline); err != nil {
			return mcp.ErrorResult("pipeline rechazado: " + err.Error())
		}
		etapas, err := etapasParaValidar(args.Pipeline)
		if err != nil {
			return mcp.ErrorResult("pipeline rechazado: " + err.Error())
		}
		if err := store.ValidatePipeline(etapas); err != nil {
			return mcp.ErrorResult("pipeline rechazado: " + err.Error())
		}

		coleccion := domain.EntityType(args.ColeccionInicial)

		ctx, cancel := context.WithTimeout(context.Background(), pipelineTimeout)
		defer cancel()

		var result reporting.Result

		switch {
		case modo == reporting.ModoConteo:
			// Un pipeline de conteo bien armado (terminado en $count o
			// un $group equivalente) devuelve un unico documento chico:
			// no hace falta limitar ni leer por partes.
			var docs []map[string]interface{}
			docs, err = runner.RunPipeline(ctx, coleccion, args.Pipeline)
			if err == nil {
				result, err = reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
			}

		case args.MostrarEnPantalla && !args.GuardarEnArchivo:
			// Solo pantalla: el limite lo aplica Mongo, en vez de traer
			// todo. +1 para poder avisar "hay mas" sin un $count aparte.
			pipelineConLimite := make([]json.RawMessage, len(args.Pipeline), len(args.Pipeline)+1)
			copy(pipelineConLimite, args.Pipeline)
			pipelineConLimite = append(pipelineConLimite, json.RawMessage(fmt.Sprintf(`{"$limit": %d}`, limitePantalla+1)))

			var docs []map[string]interface{}
			docs, err = runner.RunPipeline(ctx, coleccion, pipelineConLimite)
			if err == nil {
				result, err = reporting.Run(docs, reporting.Input{
					Modo:              reporting.ModoListado,
					MostrarEnPantalla: true,
					LimitePantalla:    limitePantalla,
				})
			}

		case args.GuardarEnArchivo:
			// Lectura y escritura por partes: ni el cursor ni el archivo
			// acumulan el resultado en memoria (ver reporting.StreamWriter).
			// Siempre JSON.
			var sw *reporting.StreamWriter
			sw, err = reporting.NewStreamWriter(salidasDir, args.NombreSugerido, args.MostrarEnPantalla, limitePantalla)
			if err == nil {
				streamErr := runner.RunPipelineStream(ctx, coleccion, args.Pipeline, sw.Add)
				if streamErr != nil {
					sw.Abort() // no dejar un archivo a medio escribir con pinta de resultado valido
					err = streamErr
				} else {
					result, err = sw.Finish()
				}
			}

		default:
			return mcp.ErrorResult("no se especifico ningun destino de salida -- hace falta mostrarEnPantalla y/o guardarEnArchivo")
		}

		if err != nil {
			return mcp.ErrorResult("error ejecutando el pipeline (¿se agoto el timeout de " + pipelineTimeout.String() + "?): " + err.Error())
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		return mcp.TextResult(string(b))
	}
}
