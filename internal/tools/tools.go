// Package tools registra las herramientas en el servidor MCP (libreria
// mark3labs/mcp-go) y las conecta con dictionary, reporting y store.
// Registra tres herramientas: diccionario_de_campos y
// relacion_entre_entidades (no consultan la base) y reporting (ejecuta un
// pipeline de agregacion), el unico camino a los datos. Las descripciones
// de las herramientas son el contrato tecnico; como conversar con el
// usuario lo definen los prompts de los agentes.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store"
)

// Instrucciones es lo que el servidor le dice al cliente al conectarse.
// Cambia segun si el guardado de archivos esta habilitado
// (HERMES_MCP_GUARDAR_ARCHIVOS).
func Instrucciones(guardarArchivos bool) string {
	base := "Servidor MCP de entidades de Hermes (las lista relacion_entre_entidades). Usar diccionario_de_campos y relacion_entre_entidades para armar el pipeline de 'reporting' -- nunca inventar un campo que no este documentado ahi. modo='conteo' devuelve un numero (nunca archivo); "
	if guardarArchivos {
		return base + "modo='listado' devuelve filas en pantalla (recortadas, con aviso explicito si se recorto) y/o un archivo completo."
	}
	return base + "modo='listado' devuelve filas en pantalla (recortadas, con aviso explicito si se recorto). " + avisoSinArchivos
}

// avisoSinArchivos es lo que el agente recibe cuando el guardado de archivos
// esta deshabilitado: en las descripciones y como error si igual lo pide.
const avisoSinArchivos = "El guardado de archivos esta deshabilitado en este servidor: si el usuario pide el resultado completo o un archivo, informarle que esta instalacion no lo permite y ofrecerle ver en pantalla las primeras filas o el total con modo='conteo'."

// DestinoArchivos guarda fuera del servidor el archivo que escribio
// reporting (por ejemplo, en el OneDrive del usuario) y devuelve su enlace.
type DestinoArchivos interface {
	Guardar(ctx context.Context, carpeta, nombre, rutaLocal string) (string, error)
}

// RegisterAll registra las tres herramientas. limitePantalla y
// pipelineTimeout (HERMES_MCP_LIMITE_PANTALLA y HERMES_MCP_MONGO_TIMEOUT)
// son los dos topes de recursos de toda ejecucion de un pipeline.
// guardarArchivos (HERMES_MCP_GUARDAR_ARCHIVOS) habilita guardarEnArchivo.
// destino puede ser nil: los archivos quedan en salidasDir.
func RegisterAll(s *server.MCPServer, runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration, guardarArchivos bool, rel dictionary.RelationsData, destino DestinoArchivos) {
	registerDictionary(s, rel)
	registerRelation(s, rel)
	registerReporting(s, runner, salidasDir, limitePantalla, pipelineTimeout, guardarArchivos, destino)
}

// soloLectura declara las anotaciones de una herramienta que solo lee: los
// clientes las usan, por ejemplo, para pedir confirmacion antes de ejecutar
// algo destructivo. Hay que declararlas: los valores por defecto de la
// libreria dicen lo contrario (destructiva, no de solo lectura).
func soloLectura() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	}
}

// tiposValores lista los tipos del registro (domain.AllTypes) para los
// "enum" de las herramientas, para que sumar una entidad no exija tocarlas.
// conVacio agrega "" al principio (en diccionario_de_campos, vacio = todos).
func tiposValores(conVacio bool) []string {
	vals := []string{}
	if conVacio {
		vals = append(vals, "")
	}
	for _, t := range domain.AllTypes {
		vals = append(vals, string(t))
	}
	return vals
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

// dictionaryTool define diccionario_de_campos, con los tipos del registro.
func dictionaryTool() mcp.Tool {
	return mcp.NewTool("diccionario_de_campos", append(soloLectura(),
		mcp.WithDescription("Devuelve, por tipo de entidad, los campos con su nombre tecnico y su significado, segun el fichero de conocimiento cargado. No consulta la base."),
		mcp.WithString("tipo",
			mcp.Description("Tipo de entidad a consultar: "+tiposTexto()+". Vacio para ver todos."),
			mcp.Enum(tiposValores(true)...),
		),
	)...)
}

func registerDictionary(s *server.MCPServer, rel dictionary.RelationsData) {
	s.AddTool(dictionaryTool(), dictionaryHandler(rel))
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
func dictionaryHandler(rel dictionary.RelationsData) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args dictionaryArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError("argumentos invalidos: " + err.Error()), nil
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
		return mcp.NewToolResultText(string(b)), nil
	}
}

// --- Relacion entre entidades ---
//
// No recibe argumentos ni consulta la base. Combina el registro tecnico
// (internal/domain: tipos, colecciones, tablas auxiliares) con el fichero de
// conocimiento cargado (relaciones, estructura de campos, origen de los
// datos).
func registerRelation(s *server.MCPServer, rel dictionary.RelationsData) {
	s.AddTool(mcp.NewTool("relacion_entre_entidades", append(soloLectura(),
		mcp.WithDescription("Devuelve la estructura de las entidades: tipos, coleccion Mongo de cada tipo y tablas auxiliares habilitadas (del registro tecnico del servidor), y la lista de relaciones (padre, anidamiento, referencia y catalogo, cada una con sus campos de cruce, estado y reglas), la estructura de los documentos, los prefijos de ucode y el origen de los datos de la instalacion ('origenDatos') (del fichero de conocimiento cargado). No consulta la base."),
	)...), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, _ := json.MarshalIndent(dictionary.Hierarchy(rel), "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})
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

// reportingTool define reporting: el limite de pantalla real aparece en las
// descripciones, y los tipos de coleccionInicial salen del registro. Si el
// guardado de archivos esta deshabilitado, las descripciones lo dicen.
func reportingTool(limitePantalla int, guardarArchivos bool) mcp.Tool {
	limite := strconv.Itoa(limitePantalla)
	salida := "y/o un archivo JSON con el resultado completo."
	descArchivo := "true para guardar el resultado completo en un archivo JSON local, sin el limite de pantalla. Ignorado si modo=conteo. Si el pipeline no devuelve documentos, no se crea archivo: la respuesta trae totalFilas=0 y archivoEscrito vacio."
	if !guardarArchivos {
		salida = "(sin archivo). " + avisoSinArchivos
		descArchivo = "No disponible. " + avisoSinArchivos
	}
	return mcp.NewTool("reporting", append(soloLectura(),
		mcp.WithDescription("Ejecuta un pipeline de agregacion Mongo tal cual lo recibe, partiendo de la coleccion de 'coleccionInicial'. Antes de ejecutar valida que use solo etapas de consulta permitidas y colecciones del alcance o tablas auxiliares habilitadas; cada consulta tiene un tiempo maximo. modo='conteo' devuelve un numero (nunca archivo); modo='listado' devuelve en pantalla como maximo "+limite+" filas (limite fijo del servidor, con aviso explicito si hay mas) "+salida),
		mcp.WithString("coleccionInicial",
			mcp.Required(),
			mcp.Enum(tiposValores(false)...),
			mcp.Description("Tipo de entidad cuya coleccion es el punto de partida del pipeline (ver 'coleccionMongo' en relacion_entre_entidades). Las tablas auxiliares habilitadas pueden usarse como destino de $lookup/$graphLookup, no como punto de partida."),
		),
		mcp.WithArray("pipeline",
			mcp.Required(),
			mcp.Items(map[string]any{"type": "object"}),
			mcp.Description("Etapas de agregacion Mongo en orden, cada una un objeto de un solo operador (ej. {\"$match\": {...}}). Se lee como Extended JSON: admite $uuid, $binary, $date y $oid. Antes de ejecutar, el servidor valida que use solo etapas de consulta permitidas y colecciones del alcance o tablas auxiliares habilitadas; si no, devuelve un error que indica la etapa rechazada."),
		),
		mcp.WithString("modo",
			mcp.Required(),
			mcp.Enum("listado", "conteo"),
			mcp.Description("'conteo': el pipeline debe terminar en $count o un $group equivalente; 0 documentos equivale a cantidad 0 y mas de un documento es un error; nunca genera archivo. 'listado': devuelve documentos, en pantalla y/o en archivo."),
		),
		mcp.WithBoolean("mostrarEnPantalla",
			mcp.Description("true para devolver las filas en la respuesta: como maximo "+limite+" (limite fijo del servidor, no se puede cambiar), con aviso explicito si el total es mayor. Para el resultado completo, guardarEnArchivo."),
		),
		mcp.WithBoolean("guardarEnArchivo",
			mcp.Description(descArchivo),
		),
		mcp.WithString("nombreSugerido",
			mcp.Description("Nombre base del archivo. Si ya existe uno con ese nombre en el dia, se le agrega _1, _2, etc. El nombre final viene en la respuesta."),
		),
	)...)
}

func registerReporting(s *server.MCPServer, runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration, guardarArchivos bool, destino DestinoArchivos) {
	s.AddTool(reportingTool(limitePantalla, guardarArchivos), reportingHandler(runner, salidasDir, limitePantalla, pipelineTimeout, guardarArchivos, destino))
}

// reportingHandler es el handler de la tool reporting, aparte de
// registerReporting para poder testearlo con un PipelineRunner falso.
// limitePantalla es el maximo de filas en pantalla: lo fija la
// configuracion (HERMES_MCP_LIMITE_PANTALLA) y ningun argumento lo cambia;
// un "limitePantalla" que mande el agente se ignora. Si guardarArchivos es
// false, un listado con guardarEnArchivo se rechaza sin ejecutar, con un
// mensaje para que el agente informe al usuario. Si hay destino, el archivo
// se sube ahi, se borra la copia local y el resultado trae el enlace en vez
// de la ruta.
func reportingHandler(runner store.PipelineRunner, salidasDir string, limitePantalla int, pipelineTimeout time.Duration, guardarArchivos bool, destino DestinoArchivos) server.ToolHandlerFunc {
	if limitePantalla <= 0 {
		limitePantalla = reporting.LimitePantallaPorDefecto
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// BindArguments decodifica el texto original de los argumentos, asi
		// que cada etapa del pipeline (json.RawMessage) conserva el orden de
		// sus claves.
		var args reportingArgs
		if err := req.BindArguments(&args); err != nil {
			return mcp.NewToolResultError("argumentos invalidos: " + err.Error()), nil
		}
		if args.ColeccionInicial == "" || len(args.Pipeline) == 0 || args.Modo == "" {
			return mcp.NewToolResultError("faltan campos obligatorios: 'coleccionInicial', 'pipeline' y 'modo'."), nil
		}
		modo := reporting.Modo(args.Modo)
		if modo != reporting.ModoConteo && modo != reporting.ModoListado {
			return mcp.NewToolResultError(fmt.Sprintf("modo %q invalido -- usar \"conteo\" o \"listado\"", args.Modo)), nil
		}
		if modo == reporting.ModoListado && args.GuardarEnArchivo && !guardarArchivos {
			return mcp.NewToolResultError("guardarEnArchivo no disponible. " + avisoSinArchivos), nil
		}

		if err := store.ValidarClavesUnicas(args.Pipeline); err != nil {
			return mcp.NewToolResultError("pipeline rechazado: " + err.Error()), nil
		}
		etapas, err := etapasParaValidar(args.Pipeline)
		if err != nil {
			return mcp.NewToolResultError("pipeline rechazado: " + err.Error()), nil
		}
		if err := store.ValidatePipeline(etapas); err != nil {
			return mcp.NewToolResultError("pipeline rechazado: " + err.Error()), nil
		}

		coleccion := domain.EntityType(args.ColeccionInicial)

		// La subida del archivo no cuenta para el tiempo maximo de la
		// consulta: usa el contexto de la llamada.
		ctxLlamada := ctx
		ctx, cancel := context.WithTimeout(ctx, pipelineTimeout)
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
			return mcp.NewToolResultError("no se especifico ningun destino de salida -- hace falta mostrarEnPantalla y/o guardarEnArchivo"), nil
		}

		if err != nil {
			return mcp.NewToolResultError("error ejecutando el pipeline (¿se agoto el timeout de " + pipelineTimeout.String() + "?): " + err.Error()), nil
		}

		if destino != nil && result.ArchivoEscrito != "" {
			ruta := result.ArchivoEscrito
			enlace, err := destino.Guardar(ctxLlamada, filepath.Base(filepath.Dir(ruta)), filepath.Base(ruta), ruta)
			_ = os.Remove(ruta) // la copia local no queda en el servidor, se haya subido o no
			if err != nil {
				return mcp.NewToolResultError("la consulta se ejecuto, pero no se pudo guardar el archivo en el OneDrive del usuario: " + err.Error()), nil
			}
			result.ArchivoEscrito = enlace
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}
