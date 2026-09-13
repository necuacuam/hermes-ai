// Package tools conecta el protocolo MCP (paquete mcp) con la logica de
// negocio (dictionary, graph, reporting, store). Registra 4 tools:
// Diccionario de campos y Relacion entre entidades (estaticas, esquema),
// Seek (busqueda viva de entidades reales, con desambiguacion), y
// Reporting (desde 2026-09-11: ejecuta un pipeline de agregacion Mongo ya
// armado por el subagente -- ya NO exige un rootUcode resuelto de
// antemano, ver la nota en registerReporting).
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
func RegisterAll(server *mcp.Server, g *graph.Graph, salidasDir string, limitePantallaDefault int, pipelineTimeout time.Duration) {
	registerDictionary(server)
	registerRelation(server)
	registerSeek(server, g)
	registerReporting(server, g, salidasDir, limitePantallaDefault, pipelineTimeout)
}

func jsonSchema(schema string) json.RawMessage {
	return json.RawMessage(schema)
}

// --- Diccionario de campos ---

type dictionaryArgs struct {
	Tipo string `json:"tipo"` // "cliente" | "centro" | "punto" | "grupo" | "holding" | "" (todos)
}

func registerDictionary(server *mcp.Server) {
	schema := `{
		"type": "object",
		"properties": {
			"tipo": {
				"type": "string",
				"description": "Tipo de entidad a consultar: cliente, centro, punto, grupo, holding o direccion. Vacio para ver todos.",
				"enum": ["", "cliente", "centro", "punto", "grupo", "holding", "direccion"]
			}
		}
	}`

	server.RegisterTool(
		"diccionario_de_campos",
		"Devuelve el significado de negocio de los campos de holding/grupo/cliente/centro/punto/direccion de Hermes -- que campo tecnico corresponde a un concepto de negocio (ej. NIF, tipo de punto, ciudad). Usar antes de armar una consulta cuando no se sabe en que campo vive un dato -- si el concepto que pide el usuario no aparece aca, no inventar un nombre de campo: avisar que falta esa informacion.",
		jsonSchema(schema),
		func(raw json.RawMessage) mcp.CallToolResult {
			var args dictionaryArgs
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return mcp.ErrorResult("argumentos invalidos: " + err.Error())
				}
			}
			result := dictionary.Lookup(domain.EntityType(args.Tipo))
			b, _ := json.MarshalIndent(result, "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Relacion entre entidades ---
//
// A proposito esta tool es ESTATICA -- no recibe argumentos de negocio ni
// toca Mongo. Responde "como se relacionan los TIPOS de entidad entre si"
// (jerarquia, prefijos de ucode, el mecanismo de ParentUCode, el punto
// abierto del anidamiento), sourced de la misma info hardcodeada que
// "diccionario_de_campos" -- el equivalente en chico del "Grafo de
// entidades Hermes" que en la arquitectura completa mantiene el Agente
// Generador. Resolver una entidad REAL por nombre (que cliente, que
// sucursal) es trabajo de la tool "seek", que si toca datos vivos.
func registerRelation(server *mcp.Server) {
	server.RegisterTool(
		"relacion_entre_entidades",
		"Devuelve como se relacionan ESTRUCTURALMENTE los tipos de entidad de Hermes (holding/grupo/cliente/centro/punto/direccion): jerarquia, prefijos de ucode, el nombre real de coleccion Mongo de cada tipo (util para armar $lookup en un pipeline de 'reporting'), el mecanismo generico de ParentUCode, la nota sobre por que 'direccion' no encaja en esa cadena, y el estado de la duda sobre anidamiento centro-en-centro/punto-en-punto. No busca entidades reales por nombre -- para eso usar 'seek'.",
		jsonSchema(`{"type": "object", "properties": {}}`),
		func(raw json.RawMessage) mcp.CallToolResult {
			b, _ := json.MarshalIndent(dictionary.Hierarchy(), "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Seek ---
//
// Esta es la tool que SI toca datos vivos (Mongo). Reemplaza lo que antes
// hacia relacion_entre_entidades con ResolvePath, pero redisenada para
// resolucion de a un nivel por vez con desambiguacion explicita -- ver la
// conversacion con TAvo: no se puede asumir que el usuario sabe de memoria
// los nombres exactos de cada nivel, ni que el primer nombre que da es
// necesariamente del tipo que uno esperaria (podria haber mas de un
// "Macedonia", o no ser un cliente).
//
// Por eso esta tool NO devuelve error ante 0 o varios matches -- devuelve
// la lista de candidatos (0, 1 o N), con un "breadcrumb" legible para que
// el agente se lo muestre al usuario y este reconozca o elija.
//
// NO es un paso obligatorio antes de "reporting". El camino por defecto es
// armar el pipeline directamente con el nombre y los filtros ya acordados
// con el usuario en la conversacion (ej. "cliente Pepito" -> $match por
// nombre en serviceEntity) y ejecutarlo -- llamar a "seek" antes "por las
// dudas" en cada consulta es un viaje de mas que casi nunca hace falta.
// "seek" sirve para DESPUES: si "reporting" da 0 resultados o algo raro
// (ver el comentario de esa tool sobre como reaccionar a un resultado
// vacio), o si el usuario mismo pide ver las opciones antes de elegir.
//
// Cuando se usa (para diagnosticar, o porque el usuario lo pidio): el tipo
// que el usuario menciono junto al nombre (ej. "el cliente Pepito") es una
// expectativa, no un hecho confirmado -- podria tratarse en realidad de un
// centro o un grupo. Por eso esa llamada va SIN el parametro "tipo",
// buscando en las 6 entidades; recien se acota por tipo en una segunda
// llamada si hizo falta desambiguar. Si el candidato encontrado no coincide
// con el tipo que dijo el usuario, no se asume ni que el usuario se
// equivoco ni que el dato encontrado es el correcto -- se le pregunta.
type seekArgs struct {
	Needle     string `json:"needle"`     // texto a buscar en el nombre; vacio = listar TODOS los hijos de padreUcode
	PadreUcode string `json:"padreUcode"` // opcional: acota la busqueda a los hijos directos de este ucode
	Tipo       string `json:"tipo"`       // opcional: acota a un tipo de entidad
}

type seekCandidato struct {
	UCode     string                 `json:"ucode"`
	Tipo      domain.EntityType      `json:"tipo"`
	Nombre    string                 `json:"nombre"`
	Ubicacion string                 `json:"ubicacion"` // breadcrumb legible, ej. "Macedonia > Sucursal 23 (Madrid)"
	Atributos map[string]interface{} `json:"atributos,omitempty"`
}

// atributosCuradosParaSeek se queda con un puñado de campos utiles para
// desambiguar (NIF, ubicacion, tipo de centro/punto, nombre del grupo/
// holding padre) en vez de devolver el objeto "data" completo -- en datos
// reales tiene 60+ campos (ver dictionary.go), y "seek" es una tool de
// desambiguacion liviana, no un volcado completo de la entidad (para eso
// esta "reporting").
var camposUtilesParaSeek = []string{
	"taxpayerId", "addressCityName", "addressProvinceName", "addressCountry",
	"centerType", "pointOfServiceType", "customerGroupName", "customerHoldingName",
	"parentName", "descriptionLevel1", "descriptionLevel2", "streetName", "postalCode",
}

func atributosCuradosParaSeek(atributos map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, campo := range camposUtilesParaSeek {
		if v, ok := atributos[campo]; ok && v != "" {
			out[campo] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func registerSeek(server *mcp.Server, g *graph.Graph) {
	schema := `{
		"type": "object",
		"properties": {
			"needle": {
				"type": "string",
				"description": "Texto a buscar en el nombre de la entidad (sin distinguir mayusculas/acentos). Si se omite y se da padreUcode, se listan TODOS los hijos directos -- usar esto cuando el usuario no tiene ninguna pista de nombre y necesita ver las opciones para reconocer la que busca."
			},
			"padreUcode": {
				"type": "string",
				"description": "Si se da, acota la busqueda a los hijos DIRECTOS de este ucode (para bajar un nivel ya confirmado). Si se omite, busca en TODAS las entidades sin asumir nivel -- usar esto para el primer paso, cuando todavia no se sabe si el nombre que dio el usuario es un cliente, un centro, etc."
			},
			"tipo": {
				"type": "string",
				"enum": ["", "holding", "grupo", "cliente", "centro", "punto", "direccion"],
				"description": "Opcional: acota la busqueda a un tipo de entidad particular. NO usar esto en la primera busqueda solo porque el usuario nombro un tipo (ej. 'el cliente Pepito') -- esa etiqueta es una expectativa del usuario, no un hecho confirmado, y podria ser un centro o un grupo en la practica. Dejar 'tipo' vacio en el primer intento; si aparece exactamente 1 candidato mostrar tambien su tipo real (si difiere del que dijo el usuario, preguntarle si es a ese al que se referia, no asumir en ningun sentido). Usar 'tipo' recien para acotar una segunda busqueda si hizo falta desambiguar."
			}
		}
	}`

	server.RegisterTool(
		"seek",
		"Busca entidades REALES de Hermes por nombre aproximado (o lista los hijos directos de una ya resuelta), sin asumir de antemano el tipo ni exigir un nombre exacto. NO es un paso obligatorio antes de 'reporting' -- el camino por defecto es armar el pipeline directamente con el nombre y los filtros ya acordados con el usuario y ejecutarlo. Usar 'seek' para DESPUES: cuando 'reporting' dio 0 resultados o algo inesperado y conviene averiguar por que (¿la entidad no existe con ese nombre? ¿existe pero de otro tipo?), o cuando el usuario mismo pide ver las opciones antes de elegir. Si se usa, la primera llamada va sin 'tipo' (busca en las 6 entidades, porque el tipo que dijo el usuario es su expectativa, no un hecho) -- 'tipo' se usa recien despues para acotar. Devuelve 0, 1 o varios candidatos con su ucode, su tipo real y un 'ubicacion' legible (breadcrumb) -- nunca falla por ambiguedad, esa es informacion para mostrarle al usuario.",
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

			ctx := context.Background()
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

			candidatos := make([]seekCandidato, 0, len(docs))
			for _, d := range docs {
				candidatos = append(candidatos, seekCandidato{
					UCode:     d.UCode,
					Tipo:      d.Type,
					Nombre:    d.Name,
					Ubicacion: g.Breadcrumb(ctx, d),
					Atributos: atributosCuradosParaSeek(d.Attributes),
				})
			}

			b, _ := json.MarshalIndent(map[string]interface{}{
				"total":      len(candidatos),
				"candidatos": candidatos,
			}, "", "  ")
			return mcp.TextResult(string(b))
		},
	)
}

// --- Reporting ---
//
// Ejecuta un pipeline de agregacion Mongo YA ARMADO por el subagente, con
// ayuda de "diccionario_de_campos" y "relacion_entre_entidades" para saber
// en que campo/coleccion vive cada cosa -- esta tool no arma nada por su
// cuenta, solo valida y ejecuta lo que le llega. No exige un ucode raiz
// resuelto de antemano: el camino por defecto es que el pipeline resuelva
// el cliente/centro que haga falta con un $match por nombre directamente,
// usando el nombre y los filtros ya acordados con el usuario en la
// conversacion -- no hace falta desambiguar con "seek" antes de cada
// llamada "por las dudas", eso seria un viaje de mas la mayoria de las
// veces.
//
// Lo que importa es como se reacciona al RESULTADO, no adelantarse:
//   - 0 documentos: no informar un simple "no se encontro nada" y cortar
//     ahi. Proponerle al usuario una lista breve de motivos posibles (ej.
//     "no hay ningun cliente llamado 'Pepito'", "el cliente existe pero no
//     tiene centros", "tiene centros pero ninguno cumple el resto del
//     filtro", "el filtro es demasiado estricto") y dejar que el usuario
//     elija por cual seguir, o pida otra consulta. Para investigar una de
//     esas lineas (ej. confirmar si "Pepito" existe con otro nombre o tipo)
//     recien ahi tiene sentido usar "seek" o una version mas laxa del
//     pipeline -- como diagnostico posterior, no como paso previo.
//   - Documentos que mezclan mas de una entidad de nombre similar pero
//     distinta (ej. el $match por "Santander" trajo resultados de dos
//     clientes reales distintos sin querer): si el pipeline proyecta el
//     nombre/ucode del cliente en cada fila, esto se nota en el resultado
//     mismo -- avisarle al usuario en vez de mezclar todo en un solo
//     listado como si fuera una sola entidad.
//
// Si la pregunta del usuario no deja claro que propiedades necesita, el
// subagente tiene que preguntarle antes de armar el pipeline final -- el
// usuario tambien puede consultarle al agente el diccionario/las relaciones
// directamente. Si un campo pedido no existe en "diccionario_de_campos", no
// se inventa: se informa el problema. Esta tool no puede validar esa
// disciplina por codigo (no sabe que pidio el usuario), pero su descripcion
// se lo recuerda al subagente en cada llamada.
//
// modo "conteo" (preguntas "cuantos"/"cantidad de"): el pipeline tiene que
// terminar en algo como $count o un $group equivalente. 0 documentos de
// vuelta es una respuesta VALIDA (cantidad 0) -- asi es como $count
// devuelve "no hubo matches" en Mongo, no significa que el pipeline este
// mal armado. Mas de un documento si es un error (el pipeline tiene que
// producir un unico valor). Nunca guarda archivo, formato siempre vacio.
//
// modo "listado": pantalla (recortada a limitePantalla filas, con aviso
// EXPLICITO si el total real es mayor -- nunca en silencio) y/o archivo
// JSON (SIEMPRE completo, sin importar el recorte de pantalla -- no hay
// opcion de CSV, ver el comentario de internal/reporting sobre por que). El
// nombre final del archivo (con sufijo _1/_2... si ya existia uno igual
// hoy) solo se sabe DESPUES de escribirlo -- no se le informa al usuario de
// antemano. La cantidad de filas que devolvio el pipeline SIEMPRE se
// informa (incluido 0) via totalFilas -- nunca se omite ese dato. Y si el
// pipeline no devuelve ningun documento, NO se crea archivo aunque se haya
// pedido guardarEnArchivo -- comunicarle al usuario que no hubo resultados
// en vez de dar por hecho que algo quedo guardado.
//
// Antes de ejecutar, el pipeline pasa por store.ValidatePipeline: solo
// etapas de agregacion de SOLO LECTURA, nada de $out/$merge/$where/
// $function, y ningun $lookup/$graphLookup fuera de las 6 colecciones del
// alcance -- la base de datos real que dio el jefe no se toca nunca, ni se
// lee nada fuera de lo acordado.
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
// Todos los caminos corren con un context.WithTimeout(pipelineTimeout) --
// un pipeline mal armado (ej. un $lookup sin indice contra una coleccion
// grande) no puede colgar el servidor indefinidamente.
type reportingArgs struct {
	ColeccionInicial  string                   `json:"coleccionInicial"`
	Pipeline          []map[string]interface{} `json:"pipeline"`
	Modo              string                   `json:"modo"`
	MostrarEnPantalla bool                     `json:"mostrarEnPantalla"`
	LimitePantalla    int                      `json:"limitePantalla"`
	GuardarEnArchivo  bool                     `json:"guardarEnArchivo"`
	NombreSugerido    string                   `json:"nombreSugerido"`
}

func registerReporting(server *mcp.Server, g *graph.Graph, salidasDir string, limitePantallaDefault int, pipelineTimeout time.Duration) {
	schema := `{
		"type": "object",
		"required": ["coleccionInicial", "pipeline", "modo"],
		"properties": {
			"coleccionInicial": {
				"type": "string",
				"enum": ["holding", "grupo", "cliente", "centro", "punto", "direccion"],
				"description": "Tipo de entidad cuya coleccion Mongo es el punto de partida del pipeline (ver 'coleccionMongo' en relacion_entre_entidades para el nombre real de coleccion, util para armar $lookup hacia las otras)."
			},
			"pipeline": {
				"type": "array",
				"items": {"type": "object"},
				"description": "Etapas de agregacion Mongo en orden, cada una un objeto de un solo operador (ej. {\"$match\": {...}}). Armado por el subagente con ayuda de diccionario_de_campos/relacion_entre_entidades -- NO inventar nombres de campo que no esten documentados ahi; si falta informacion, avisar en vez de adivinar. Solo se permiten etapas de LECTURA (sin $out/$merge/$where/$function, ver store.ValidatePipeline) -- la base de datos real nunca se modifica."
			},
			"modo": {
				"type": "string",
				"enum": ["listado", "conteo"],
				"description": "'conteo' para preguntas 'cuantos'/'cantidad de' -- el pipeline debe terminar en $count o un $group equivalente. 0 documentos de vuelta es una respuesta valida (cantidad 0, asi es como Mongo devuelve 'no hubo matches' en un $count) -- mas de un documento si es un error. Nunca guarda archivo, formato siempre vacio. 'listado' para cualquier otra cosa (nombrar, listar, exportar)."
			},
			"mostrarEnPantalla": {
				"type": "boolean",
				"description": "true si el usuario quiere ver el resultado en pantalla. Se recorta a 'limitePantalla' filas, avisando explicitamente si el total real es mayor -- nunca se muestra un recorte como si fuera todo el resultado."
			},
			"limitePantalla": {
				"type": "integer",
				"description": "Filas maximas a mostrar en pantalla si mostrarEnPantalla=true. <=0 usa el default configurado en el servidor (HERMES_MCP_LIMITE_PANTALLA_DEFAULT). Cuando no se guarda archivo, este limite se empuja directo a Mongo (via $limit) -- no se trae de mas para despues recortar."
			},
			"guardarEnArchivo": {
				"type": "boolean",
				"description": "true si el usuario quiere el resultado guardado en un archivo local (siempre JSON, no hay opcion de CSV -- un pipeline de agregacion puede proyectar formas anidadas que no son una tabla). El archivo SIEMPRE lleva el resultado completo, sin importar limitePantalla. Ignorado si modo=conteo. Si el pipeline no devuelve ningun documento, NO se crea archivo aunque esto sea true -- la respuesta trae totalFilas=0 y archivoEscrito vacio; comunicarle eso al usuario en vez de asumir que algo se guardo."
			},
			"nombreSugerido": {
				"type": "string",
				"description": "Nombre base para el archivo, decidido por el agente (no hace falta consultarlo con el usuario antes) -- si ya existe un archivo con ese nombre hoy, se le agrega _1, _2, etc. El nombre final se devuelve en la respuesta, recien ahi para comunicarselo al usuario."
			}
		}
	}`

	server.RegisterTool(
		"reporting",
		"Ejecuta un pipeline de agregacion Mongo YA ARMADO por el subagente (con diccionario_de_campos/relacion_entre_entidades) sobre las entidades tipo cuenta (holding/grupo/cliente/centro/punto/direccion). Camino por defecto: armar el pipeline con el nombre y los filtros ya acordados con el usuario (ej. un $match por nombre aproximado) y ejecutarlo directamente -- no hace falta desambiguar con 'seek' antes de cada llamada. Lo que importa es como se reacciona al resultado: si da 0 documentos, no informar un 'no se encontro nada' y cortar -- proponerle al usuario una lista breve de motivos posibles (la entidad no existe con ese nombre, existe pero no cumple el resto del filtro, el filtro es muy estricto, etc.) y dejar que elija por cual seguir (ahi si puede convenir 'seek', como diagnostico, no como paso previo). Si el resultado mezcla sin querer mas de una entidad de nombre similar (ej. dos clientes reales distintos que contienen 'Santander'), avisarlo en vez de presentarlo como un unico listado. No inventar campos que no esten en el diccionario: si falta informacion, decirlo en vez de adivinar. modo='conteo' devuelve un numero (nunca archivo); modo='listado' devuelve filas en pantalla (recortadas con aviso explicito) y/o un archivo JSON completo (no hay CSV). Toda etapa de escritura ($out, $merge) o de codigo arbitrario ($where, $function) es rechazada -- la base real nunca se modifica.",
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

			if err := store.ValidatePipeline(args.Pipeline); err != nil {
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
			var err error

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
				pipelineConLimite := make([]map[string]interface{}, len(args.Pipeline), len(args.Pipeline)+1)
				copy(pipelineConLimite, args.Pipeline)
				pipelineConLimite = append(pipelineConLimite, map[string]interface{}{"$limit": limitePantalla + 1})

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
