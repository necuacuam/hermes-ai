// Package tools conecta el protocolo MCP (paquete mcp) con dictionary,
// graph, reporting y store. Registra 4 tools: diccionario_de_campos y
// relacion_entre_entidades (no consultan la base), seek (busqueda de
// entidades por nombre) y reporting (ejecucion de un pipeline de
// agregacion). Las descripciones de las tools son el contrato tecnico; como
// conversar con el usuario lo definen los prompts de los agentes.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hermes-mcp/internal/dictionary"
	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/graph"
	"hermes-mcp/internal/mcp"
	"hermes-mcp/internal/reporting"
	"hermes-mcp/internal/store"
)

// RegisterAll da de alta las 4 tools en el servidor. limitePantallaDefault y
// pipelineTimeout vienen de cmd/hermes-mcp (HERMES_MCP_LIMITE_PANTALLA_DEFAULT
// / HERMES_MCP_MONGO_TIMEOUT) -- se configuran juntos a proposito (ver
// registerReporting): son los dos topes de recursos de cualquier ejecucion
// de pipeline.
func RegisterAll(server *mcp.Server, g *graph.Graph, salidasDir string, limitePantallaDefault, limiteSeek int, pipelineTimeout time.Duration, rel dictionary.RelationsData) {
	registerDictionary(server, rel)
	registerRelation(server, rel)
	registerSeek(server, g, rel, limiteSeek, pipelineTimeout)
	registerReporting(server, g, salidasDir, limitePantallaDefault, pipelineTimeout)
}

func jsonSchema(schema string) json.RawMessage {
	return json.RawMessage(schema)
}

// --- Diccionario de campos ---

type dictionaryArgs struct {
	Tipo string `json:"tipo"` // "cliente" | "centro" | "punto" | "grupo" | "holding" | "direccion" | "salesPerson" | "" (todos)
}

func registerDictionary(server *mcp.Server, rel dictionary.RelationsData) {
	schema := `{
		"type": "object",
		"properties": {
			"tipo": {
				"type": "string",
				"description": "Tipo de entidad a consultar: cliente, centro, punto, grupo, holding, direccion o salesPerson. Vacio para ver todos.",
				"enum": ["", "cliente", "centro", "punto", "grupo", "holding", "direccion", "salesPerson"]
			}
		}
	}`

	server.RegisterTool(
		"diccionario_de_campos",
		"Devuelve, por tipo de entidad, los campos con su nombre tecnico y su significado, segun el fichero de conocimiento cargado. No consulta la base.",
		jsonSchema(schema),
		func(raw json.RawMessage) mcp.CallToolResult {
			var args dictionaryArgs
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return mcp.ErrorResult("argumentos invalidos: " + err.Error())
				}
			}
			result := dictionary.Lookup(rel.Campos, domain.EntityType(args.Tipo))
			b, _ := json.MarshalIndent(result, "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Relacion entre entidades ---
//
// No recibe argumentos ni consulta la base. Combina el registro tecnico
// (internal/domain: jerarquia, prefijos, colecciones) con el fichero de
// conocimiento cargado (relaciones, anidamiento, origen de los datos).
func registerRelation(server *mcp.Server, rel dictionary.RelationsData) {
	server.RegisterTool(
		"relacion_entre_entidades",
		"Devuelve la estructura de las entidades: jerarquia de tipos, prefijos de ucode y coleccion Mongo de cada tipo (del registro tecnico del servidor), y el mecanismo de relacion, otras relaciones, anidamiento y origen de los datos de la instalacion ('origenDatos') (del fichero de conocimiento cargado). No consulta la base.",
		jsonSchema(`{"type": "object", "properties": {}}`),
		func(raw json.RawMessage) mcp.CallToolResult {
			b, _ := json.MarshalIndent(dictionary.Hierarchy(rel), "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Seek ---
//
// Consulta la base. Busca por nombre aproximado en todos los tipos de
// entidad (o en uno, si se da "tipo"), o lista los hijos directos de un
// ucode. Nunca devuelve error por 0 o varios resultados: devuelve la lista
// de candidatos, cada uno con su tipo real y un breadcrumb legible.
type seekArgs struct {
	Needle     string `json:"needle"`     // texto a buscar en el nombre; vacio = listar TODOS los hijos de padreUcode
	PadreUcode string `json:"padreUcode"` // opcional: acota la busqueda a los hijos directos de este ucode
	Tipo       string `json:"tipo"`       // opcional: acota a un tipo de entidad
}

type seekCandidato struct {
	UCode     string                 `json:"ucode"`
	Tipo      domain.EntityType      `json:"tipo"`
	Nombre    string                 `json:"nombre"`
	Ubicacion string                 `json:"ubicacion"` // breadcrumb legible: nombres de los ancestros hasta la entidad
	Atributos map[string]interface{} `json:"atributos,omitempty"`
}

// atributosCuradosParaSeek devuelve solo los campos listados para ese tipo
// en el fichero de conocimiento (dictionary.RelationsData.CamposUtilesSeek),
// en vez del objeto "data" completo: seek devuelve candidatos livianos, no
// la entidad entera.
func atributosCuradosParaSeek(atributos map[string]interface{}, campos []string) map[string]interface{} {
	out := map[string]interface{}{}
	for _, campo := range campos {
		if v, ok := atributos[campo]; ok && v != "" {
			out[campo] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func registerSeek(server *mcp.Server, g *graph.Graph, rel dictionary.RelationsData, limite int, timeout time.Duration) {
	if limite <= 0 {
		limite = store.LimiteSeekPorDefecto
	}
	schema := `{
		"type": "object",
		"properties": {
			"needle": {
				"type": "string",
				"description": "Texto a buscar en el nombre de la entidad, sin distinguir mayusculas. Sin padreUcode es una expresion regular (ej. '^IKEA', 'PETROGAL|GALP'; los caracteres especiales como '.' o '(' se escapan con '\\' para buscarlos literales) y los acentos cuentan ('Lisboa' no encuentra 'Lisbóa'). Con padreUcode es texto literal y los acentos se ignoran. Si se omite y se da padreUcode, devuelve todos los hijos directos."
			},
			"padreUcode": {
				"type": "string",
				"description": "Si se da, limita la busqueda a los hijos directos de este ucode. Si se omite, busca en todos los tipos de entidad."
			},
			"tipo": {
				"type": "string",
				"enum": ["", "holding", "grupo", "cliente", "centro", "punto", "direccion", "salesPerson"],
				"description": "Opcional: limita la busqueda a un tipo de entidad. Si se omite, busca en todos los tipos."
			}
		}
	}`

	server.RegisterTool(
		"seek",
		fmt.Sprintf("Busca entidades por nombre aproximado, sin exigir el tipo ni un nombre exacto, o lista los hijos directos de una entidad dada. Consulta la base. Devuelve 0, 1 o varios candidatos con su ucode, su tipo real y su ubicacion (breadcrumb); nunca falla por ambiguedad. Devuelve como maximo %d candidatos: si hay mas, la respuesta trae recortado=true y hay que acotar la busqueda (needle mas preciso, tipo o padreUcode). Como hijos solo devuelve entidades vinculadas por parentUCode.", limite),
		jsonSchema(schema),
		func(raw json.RawMessage) mcp.CallToolResult {
			var args seekArgs
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return mcp.ErrorResult("argumentos invalidos: " + err.Error())
				}
			}
			if args.Needle == "" && args.PadreUcode == "" {
				return mcp.ErrorResult("hace falta 'needle' y/o 'padreUcode' -- buscar sin ninguno de los dos listaria toda la base, lo cual no tiene sentido para este uso.")
			}

			// Mismo tope de tiempo que reporting (2026-10-06): antes seek no
			// tenia ninguno.
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			tipo := domain.EntityType(args.Tipo)

			var docs []domain.Document
			var err error
			if args.PadreUcode != "" {
				docs, err = g.SearchChildren(ctx, args.PadreUcode, args.Needle, tipo)
			} else {
				docs, err = g.SearchGlobal(ctx, args.Needle, tipo)
			}
			if err != nil {
				return mcp.ErrorResult("error buscando: " + err.Error())
			}

			recortado := len(docs) > limite
			if recortado {
				docs = docs[:limite]
			}

			candidatos := make([]seekCandidato, 0, len(docs))
			for _, d := range docs {
				candidatos = append(candidatos, seekCandidato{
					UCode:     d.UCode,
					Tipo:      d.Type,
					Nombre:    d.Name,
					Ubicacion: g.Breadcrumb(ctx, d),
					Atributos: atributosCuradosParaSeek(d.Attributes, rel.CamposUtilesSeek[d.Type]),
				})
			}

			respuesta := map[string]interface{}{
				"total":      len(candidatos),
				"candidatos": candidatos,
			}
			if recortado {
				respuesta["recortado"] = true
				respuesta["aviso"] = fmt.Sprintf("hay mas de %d coincidencias y solo se devuelven %d -- acotar la busqueda con un needle mas preciso, tipo o padreUcode", limite, limite)
			}
			b, _ := json.MarshalIndent(respuesta, "", "  ")
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
// explicito si el total es mayor) y/o archivo JSON con el resultado
// completo. El nombre final del archivo (con sufijo _1/_2... si ya existia)
// se conoce recien despues de escribirlo. totalFilas siempre se informa,
// incluido 0. Sin documentos, no se crea archivo.
//
// El handler elige uno de tres caminos de ejecucion segun lo que se pidio,
// para no traer a memoria mas de lo necesario:
//   - modo "conteo": pipeline completo (siempre es 1 documento chico).
//   - "listado" solo a pantalla (sin guardarEnArchivo): se le agrega
//     {"$limit": limitePantalla+1} al pipeline ANTES de ejecutarlo -- Mongo
//     mismo corta ahi, nunca viaja de mas por la red. El +1 es para poder
//     avisar "hay mas" sin necesitar un $count aparte.
//   - "listado" con guardarEnArchivo: RunPipelineStream + reporting.
//     StreamWriter -- el archivo se escribe a medida que llegan los
//     documentos del cursor (paginado via mongostore.pipelineBatchSize),
//     sin acumular el resultado completo en memoria de este lado.
//
// Todos los caminos corren con un context.WithTimeout(pipelineTimeout) --
// un pipeline mal armado (ej. un $lookup sin indice contra una coleccion
// grande) no puede colgar el servidor indefinidamente.
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
	LimitePantalla    int               `json:"limitePantalla"`
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

func registerReporting(server *mcp.Server, g *graph.Graph, salidasDir string, limitePantallaDefault int, pipelineTimeout time.Duration) {
	schema := `{
		"type": "object",
		"required": ["coleccionInicial", "pipeline", "modo"],
		"properties": {
			"coleccionInicial": {
				"type": "string",
				"enum": ["holding", "grupo", "cliente", "centro", "punto", "direccion", "salesPerson"],
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
				"description": "true para devolver las filas en la respuesta, recortadas a 'limitePantalla', con aviso explicito si el total es mayor."
			},
			"limitePantalla": {
				"type": "integer",
				"description": "Filas maximas en la respuesta si mostrarEnPantalla=true. <=0 usa el default configurado en el servidor (HERMES_MCP_LIMITE_PANTALLA_DEFAULT)."
			},
			"guardarEnArchivo": {
				"type": "boolean",
				"description": "true para guardar el resultado completo en un archivo JSON local, sin importar limitePantalla. Ignorado si modo=conteo. Si el pipeline no devuelve documentos, no se crea archivo: la respuesta trae totalFilas=0 y archivoEscrito vacio."
			},
			"nombreSugerido": {
				"type": "string",
				"description": "Nombre base del archivo. Si ya existe uno con ese nombre en el dia, se le agrega _1, _2, etc. El nombre final viene en la respuesta."
			}
		}
	}`

	server.RegisterTool(
		"reporting",
		"Ejecuta un pipeline de agregacion Mongo tal cual lo recibe, partiendo de la coleccion de 'coleccionInicial'. Antes de ejecutar valida que use solo etapas de consulta permitidas y colecciones del alcance o tablas auxiliares habilitadas; cada consulta tiene un tiempo maximo. modo='conteo' devuelve un numero (nunca archivo); modo='listado' devuelve filas en pantalla (recortadas con aviso explicito) y/o un archivo JSON con el resultado completo.",
		jsonSchema(schema),
		func(raw json.RawMessage) mcp.CallToolResult {
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

			runner, ok := g.Store.(store.PipelineRunner)
			if !ok {
				return mcp.ErrorResult("el store activo no soporta pipelines de agregacion (esto pasa con mockstore, pensado solo para tests) -- conectar HERMES_MCP_MONGO_URI a un Mongo real.")
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

			limitePantalla := args.LimitePantalla
			if limitePantalla <= 0 {
				limitePantalla = limitePantallaDefault
			}

			ctx, cancel := context.WithTimeout(context.Background(), pipelineTimeout)
			defer cancel()

			var result reporting.Result

			switch {
			case modo == reporting.ModoConteo:
				// Un pipeline de conteo bien armado (terminado en $count o
				// un $group equivalente) siempre devuelve un unico
				// documento chico -- no hay necesidad de limitar ni de
				// streaming aca.
				var docs []map[string]interface{}
				docs, err = runner.RunPipeline(ctx, coleccion, args.Pipeline)
				if err == nil {
					result, err = reporting.Run(docs, reporting.Input{Modo: reporting.ModoConteo})
				}

			case args.MostrarEnPantalla && !args.GuardarEnArchivo:
				// Solo pantalla: empujar el limite A MONGO en vez de traer
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
				// Streaming de punta a punta -- ni el cursor de Mongo ni el
				// archivo acumulan el resultado completo en memoria de este
				// lado (ver reporting.StreamWriter y
				// mongostore.pipelineBatchSize). Siempre JSON.
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
		},
	)
}
