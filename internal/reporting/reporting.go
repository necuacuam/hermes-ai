// Package reporting implementa la regla de salida del resultado de un
// pipeline de agregacion Mongo:
//
//   - Si la pregunta es de tipo "cuantos"/"cantidad de" (modo "conteo"), el
//     pipeline tiene que terminar en $count o un $group equivalente. 0
//     documentos de vuelta es una respuesta valida (cantidad 0 -- asi es
//     como Mongo devuelve "no hubo matches" en un $count, no es un error);
//     mas de un documento si lo es. Eso es lo unico que se comunica,
//     siempre por pantalla, nunca a archivo.
//   - Si la pregunta pide un listado (modo "listado"), el resultado puede
//     mostrarse en pantalla (recortado a un maximo de filas, avisando
//     explicitamente si se recorto -- nunca en silencio) y/o guardarse en
//     un archivo JSON local, que siempre lleva el resultado completo sin
//     importar el recorte de pantalla. La cantidad de filas obtenidas del
//     pipeline SIEMPRE se informa (incluido el caso 0), y si el resultado
//     esta vacio no se crea ningun archivo aunque se haya pedido guardarlo
//     -- ver StreamWriter.
//
// El archivo siempre es JSON -- no hay opcion de CSV. Un pipeline de
// agregacion puede proyectar cualquier forma (documentos anidados, listas,
// resultados de $lookup) y CSV exige columnas fijas conocidas de antemano;
// forzar ese resultado a filas planas termina crudo (un sub-documento
// serializado como texto dentro de una celda) o, peor, pierde datos si
// aparece una columna nueva a mitad de archivo. JSON preserva la forma real
// del resultado sin inventar una tabla que no existe -- si el usuario
// necesita una hoja de calculo, la arma el mismo a partir del JSON.
//
// Este paquete no sabe nada de Mongo ni arma el pipeline -- eso es trabajo
// de internal/tools (arma y ejecuta) e internal/store (ejecuta y, si hace
// falta, pagina la lectura). Este paquete solo decide que hacer con el
// resultado: recortar para pantalla, o escribirlo a disco.
package reporting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	// Import en blanco: embebe la base de datos IANA de husos horarios en
	// el binario. time.LoadLocation("Europe/Madrid") depende de que el
	// sistema operativo tenga esa base instalada, y en un equipo Windows
	// corporativo eso no esta garantizado. Sin este import, un sistema sin esa base cae al fallback
	// UTC de carpetaDeHoy sin avisar, corriendo la fecha de la carpeta unas
	// horas cerca de medianoche.
	_ "time/tzdata"
)

// Modo distingue una pregunta de conteo ("cuantos hay") de una de listado
// (mostrar o exportar filas). Lo decide el agente al llamar a reporting.
type Modo string

const (
	ModoListado Modo = "listado"
	ModoConteo  Modo = "conteo"
)

// formatoArchivo es el unico formato de archivo soportado -- ver el
// comentario del paquete sobre por que no hay CSV.
const formatoArchivo = "json"

// LimitePantallaPorDefecto es el maximo de filas en pantalla si
// HERMES_MCP_LIMITE_PANTALLA no esta configurada (ver cmd/hermes-mcp). El
// agente no lo puede cambiar: la tool reporting no recibe un limite.
const LimitePantallaPorDefecto = 10

// Input son los parametros de una consulta de modo "listado" que solo pide
// pantalla. El guardado a archivo es responsabilidad exclusiva de
// StreamWriter (ver mas abajo) -- Run no escribe nada a disco.
type Input struct {
	Modo              Modo
	MostrarEnPantalla bool
	LimitePantalla    int // filas maximas en pantalla si MostrarEnPantalla=true; <=0 usa LimitePantallaPorDefecto
}

// Result es lo que el agente recibe de reporting: la cantidad (modo
// conteo), o las filas en pantalla y/o el archivo escrito (modo listado).
type Result struct {
	Modo     Modo `json:"modo"`
	Cantidad *int `json:"cantidad,omitempty"` // solo modo conteo

	Filas []map[string]interface{} `json:"filas,omitempty"` // solo si MostrarEnPantalla=true, modo listado

	// TotalFilas es un puntero a proposito: nil significa "no aplica"
	// (modo conteo, donde el numero ya esta en Cantidad) y se omite del
	// JSON; un valor apuntado en 0 significa "aplica y el pipeline no
	// devolvio ningun documento" y SE MUESTRA -- nunca hay que dejar que
	// el usuario se pregunte si la cantidad de resultados vino vacia o
	// simplemente no se informo.
	TotalFilas     *int   `json:"totalFilas,omitempty"`
	FilasTruncadas bool   `json:"filasTruncadas,omitempty"` // true si TotalFilas > len(Filas) -- avisar siempre, nunca en silencio
	ArchivoEscrito string `json:"archivoEscrito,omitempty"` // ruta final (con sufijo _1/_2... si hubo colision), vacio si no se guardo nada (incluido el caso de 0 resultados -- ver StreamWriter.Finish)
	Formato        string `json:"formato"`                  // "json" si se escribio archivo, string vacio si no se guardo nada
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "consulta"
	}
	return strings.ToLower(s)
}

// ZonaHorariaPorDefecto es la zona horaria de la carpeta del dia si
// HERMES_MCP_ZONA_HORARIA no esta configurada (ver cmd/hermes-mcp).
const ZonaHorariaPorDefecto = "Europe/Madrid"

var zonaHoraria = ZonaHorariaPorDefecto

// SetZonaHoraria fija la zona horaria con la que se arma la carpeta del dia.
// Devuelve error si el nombre no es una zona valida, y en ese caso no cambia
// nada.
func SetZonaHoraria(nombre string) error {
	if _, err := time.LoadLocation(nombre); err != nil {
		return err
	}
	zonaHoraria = nombre
	return nil
}

// carpetaDeHoy devuelve la subcarpeta del dia de hoy en la zona horaria
// configurada dentro de baseDir, para que la fecha no dependa del huso
// horario del proceso. Cae a UTC si la base de zonas horarias no esta
// disponible (ver el import en blanco de time/tzdata mas arriba, que hace
// que esto casi nunca pase).
func carpetaDeHoy(baseDir string) string {
	loc, err := time.LoadLocation(zonaHoraria)
	if err != nil {
		loc = time.UTC
	}
	fecha := time.Now().In(loc).Format("2006-01-02")
	return filepath.Join(baseDir, fecha)
}

// nombreSinColision arma el nombre final del archivo dentro de dir: si
// "<slug>.<ext>" no existe, lo usa tal cual; si existe, prueba
// "<slug>_1.<ext>", "<slug>_2.<ext>", etc. hasta encontrar uno libre --
// nunca sobrescribe un archivo del mismo dia con el mismo nombre sugerido.
func nombreSinColision(dir, slug, ext string) (string, error) {
	base := fmt.Sprintf("%s.%s", slug, ext)
	path := filepath.Join(dir, base)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	} else if err != nil {
		return "", fmt.Errorf("no se pudo comprobar si %s ya existe: %w", path, err)
	}
	for i := 1; ; i++ {
		candidato := fmt.Sprintf("%s_%d.%s", slug, i, ext)
		path = filepath.Join(dir, candidato)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("no se pudo comprobar si %s ya existe: %w", path, err)
		}
	}
}

// Run aplica la regla de salida de modo "listado" a pantalla, o de modo
// "conteo", sobre docs (el resultado ya ejecutado del pipeline). No toca
// disco -- para guardar a archivo usar StreamWriter.
func Run(docs []map[string]interface{}, in Input) (Result, error) {
	switch in.Modo {
	case ModoConteo:
		return runConteo(docs)
	case ModoListado:
		return runListado(docs, in)
	default:
		return Result{}, fmt.Errorf("modo %q invalido -- usar \"conteo\" o \"listado\"", in.Modo)
	}
}

// runConteo exige a lo sumo un documento con un unico valor numerico -- es
// responsabilidad del agente terminar el pipeline en algo como $count o
// un $group que produzca esa forma.
//
// Caso especial: 0 documentos no es un error, es la respuesta "0". Un
// pipeline que termina en $count (o en un $group tipo {_id: null, cantidad:
// {$sum: 1}}) no emite NINGUN documento cuando no hay ningun match -- eso
// es como funciona Mongo, no significa que el pipeline este mal armado. Si
// se tratara como error, cualquier pregunta "cuantos X hay" cuya respuesta
// real es cero terminaria devolviendo un mensaje de error en vez de la
// respuesta correcta.
func runConteo(docs []map[string]interface{}) (Result, error) {
	if len(docs) == 0 {
		cero := 0
		return Result{Modo: ModoConteo, Cantidad: &cero}, nil
	}
	if len(docs) > 1 {
		return Result{}, fmt.Errorf("modo conteo esperaba que el pipeline devolviera 0 o 1 documento con el valor numerico, devolvio %d -- revisar que el pipeline termine en un $count o un $group equivalente", len(docs))
	}
	valor, err := unicoValorNumerico(docs[0])
	if err != nil {
		return Result{}, fmt.Errorf("modo conteo: %w", err)
	}
	return Result{Modo: ModoConteo, Cantidad: &valor}, nil
}

// unicoValorNumerico exige que, descartando "_id", haya EXACTAMENTE un
// campo numerico en el documento -- no "el primero que aparezca" (iterar un
// map en Go tiene orden no determinista). Si hay ambiguedad, se rechaza
// explicitamente en vez de adivinar cual de los campos es el buscado.
func unicoValorNumerico(doc map[string]interface{}) (int, error) {
	type candidato struct {
		campo string
		valor int
	}
	var candidatos []candidato

	for k, v := range doc {
		if k == "_id" {
			continue // el _id de Mongo no es el valor buscado, aunque a veces sea numerico
		}
		switch n := v.(type) {
		case float64:
			candidatos = append(candidatos, candidato{k, int(n)})
		case int:
			candidatos = append(candidatos, candidato{k, n})
		case int32:
			candidatos = append(candidatos, candidato{k, int(n)})
		case int64:
			candidatos = append(candidatos, candidato{k, int(n)})
		case string:
			if iv, err := strconv.Atoi(n); err == nil {
				candidatos = append(candidatos, candidato{k, iv})
			}
		}
	}

	switch len(candidatos) {
	case 0:
		return 0, fmt.Errorf("el documento devuelto no tiene ningun campo numerico reconocible: %+v", doc)
	case 1:
		return candidatos[0].valor, nil
	default:
		campos := make([]string, len(candidatos))
		for i, c := range candidatos {
			campos[i] = c.campo
		}
		sort.Strings(campos)
		return 0, fmt.Errorf("el documento devuelto tiene mas de un campo numerico (%v) -- el pipeline de conteo tiene que proyectar un unico valor (ej. $count o un $project que deje solo el campo buscado)", campos)
	}
}

// runListado recorta docs para pantalla, avisando explicitamente si hubo
// recorte. No escribe nada a disco -- ver StreamWriter para eso.
func runListado(docs []map[string]interface{}, in Input) (Result, error) {
	if !in.MostrarEnPantalla {
		return Result{}, fmt.Errorf("modo listado sin MostrarEnPantalla no tiene ningun efecto en Run -- el guardado a archivo lo maneja StreamWriter directamente")
	}

	limite := in.LimitePantalla
	if limite <= 0 {
		limite = LimitePantallaPorDefecto
	}

	total := len(docs)
	result := Result{Modo: ModoListado, TotalFilas: &total}
	if len(docs) > limite {
		result.Filas = docs[:limite]
		result.FilasTruncadas = true
	} else {
		result.Filas = docs
		result.FilasTruncadas = false
	}
	return result, nil
}

// StreamWriter escribe el detalle de un listado a un archivo JSON a medida
// que llegan los documentos (via Add), en vez de esperar a tener el
// resultado completo en un slice -- el llamador (internal/tools) lo
// alimenta documento a documento desde store.PipelineRunner.
// RunPipelineStream, que es quien pagina la lectura contra Mongo
// (mongostore.pipelineBatchSize). Entre los dos, ni la lectura ni la
// escritura necesitan tener el resultado completo en memoria a la vez.
type StreamWriter struct {
	baseDir           string
	nombreSugerido    string
	mostrarEnPantalla bool
	limitePantalla    int
	pantalla          []map[string]interface{}
	totalVisto        int
	jw                *jsonStreamWriter // nil hasta el primer Add -- ver crearArchivo
	path              string            // vacio hasta que jw se crea
}

// NewStreamWriter prepara el streaming pero -- a proposito -- NO toca disco
// todavia: ni crea la carpeta del dia ni el archivo. Eso se posterga hasta
// el primer documento real (ver Add/crearArchivo), para que un pipeline que
// no devuelve nada nunca deje un archivo (vacio pero "valido") en el
// storage -- si no hay resultados, no hay archivo, y se informa al usuario
// en el Result (ArchivoEscrito/Formato vacios, TotalFilas apuntando a 0).
func NewStreamWriter(baseDir, nombreSugerido string, mostrarEnPantalla bool, limitePantalla int) (*StreamWriter, error) {
	if limitePantalla <= 0 {
		limitePantalla = LimitePantallaPorDefecto
	}
	return &StreamWriter{
		baseDir:           baseDir,
		nombreSugerido:    nombreSugerido,
		mostrarEnPantalla: mostrarEnPantalla,
		limitePantalla:    limitePantalla,
	}, nil
}

// crearArchivo crea la carpeta del dia (zona horaria configurada) y el archivo con
// nombre sin colision -- se llama una sola vez, recien cuando llega el
// primer documento (ver Add).
func (s *StreamWriter) crearArchivo() error {
	dir := carpetaDeHoy(s.baseDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("no se pudo crear la carpeta de salida %s: %w", dir, err)
	}
	path, err := nombreSinColision(dir, slugify(s.nombreSugerido), formatoArchivo)
	if err != nil {
		return err
	}
	jw, err := newJSONStreamWriter(path, s.nombreSugerido)
	if err != nil {
		return err
	}
	s.jw = jw
	s.path = path
	return nil
}

// Add procesa un documento: si es el primero que llega, recien ahi crea el
// archivo (ver crearArchivo); siempre lo escribe, y si mostrarEnPantalla=true
// ademas lo guarda en el buffer de vista previa mientras ese buffer no
// supere limitePantalla+1 (un elemento de mas para poder avisar "hay mas"
// sin necesitar un $count aparte).
func (s *StreamWriter) Add(doc map[string]interface{}) error {
	if s.jw == nil {
		if err := s.crearArchivo(); err != nil {
			return err
		}
	}
	s.totalVisto++
	if s.mostrarEnPantalla && len(s.pantalla) < s.limitePantalla+1 {
		s.pantalla = append(s.pantalla, doc)
	}
	return s.jw.Add(doc)
}

// Abort se llama cuando la ejecucion corta a mitad de camino (ej. por
// timeout) -- cierra el archivo sin escribir el cierre valido de JSON
// (quedaria truncado igual) y lo borra del disco, para no dejar un archivo
// con pinta de resultado valido pero con contenido invalido. Si todavia no
// se habia creado ningun archivo (cero documentos vistos antes de cortar),
// no hay nada que cerrar ni borrar. El borrado es best-effort: si falla, se
// ignora, ya se esta reportando el error original de todas formas.
func (s *StreamWriter) Abort() {
	if s.jw == nil {
		return
	}
	s.jw.f.Close()
	os.Remove(s.path)
}

// Finish cierra el archivo (si se llego a crear) y arma el Result final.
// TotalFilas es el total real (este camino siempre lee el pipeline
// completo, sin $limit) y SIEMPRE se informa, incluso en 0 -- ver el
// comentario del campo en Result. Si el pipeline no devolvio ningun
// documento, jamas se creo un archivo (ver Add/crearArchivo), asi que
// ArchivoEscrito y Formato quedan vacios, igual que en modo conteo cuando
// no se guarda nada.
func (s *StreamWriter) Finish() (Result, error) {
	total := s.totalVisto
	result := Result{Modo: ModoListado, TotalFilas: &total}

	if s.jw == nil {
		return result, nil
	}

	if err := s.jw.Close(); err != nil {
		return Result{}, err
	}
	result.ArchivoEscrito = s.path
	result.Formato = formatoArchivo

	if s.mostrarEnPantalla {
		if len(s.pantalla) > s.limitePantalla {
			result.Filas = s.pantalla[:s.limitePantalla]
			result.FilasTruncadas = true
		} else {
			result.Filas = s.pantalla
			result.FilasTruncadas = false
		}
	}
	return result, nil
}

// jsonStreamWriter escribe un objeto JSON de la forma
// {"consulta":...,"generadoEn":...,"detalle":[...],"cantidad":N} sin
// mantener "detalle" completo en memoria -- el orden de las claves en un
// objeto JSON no importa para ningun parser estandar, asi que "cantidad"
// (que solo se conoce al final) va al cierre sin problema.
type jsonStreamWriter struct {
	f     *os.File
	first bool
	count int
	err   error
}

func newJSONStreamWriter(path, etiquetaConsulta string) (*jsonStreamWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el archivo %s: %w", path, err)
	}
	consultaJSON, _ := json.Marshal(etiquetaConsulta)
	generadoEnJSON, _ := json.Marshal(time.Now())
	header := fmt.Sprintf(`{"consulta":%s,"generadoEn":%s,"detalle":[`, consultaJSON, generadoEnJSON)
	if _, err := f.WriteString(header); err != nil {
		f.Close()
		return nil, fmt.Errorf("no se pudo escribir el archivo %s: %w", path, err)
	}
	return &jsonStreamWriter{f: f, first: true}, nil
}

func (w *jsonStreamWriter) Add(doc map[string]interface{}) error {
	if w.err != nil {
		return w.err
	}
	if !w.first {
		if _, err := w.f.WriteString(","); err != nil {
			w.err = err
			return err
		}
	}
	w.first = false
	b, err := json.Marshal(doc)
	if err != nil {
		w.err = fmt.Errorf("no se pudo serializar un documento: %w", err)
		return w.err
	}
	if _, err := w.f.Write(b); err != nil {
		w.err = err
		return err
	}
	w.count++
	return nil
}

func (w *jsonStreamWriter) Close() error {
	defer w.f.Close()
	if w.err != nil {
		return w.err
	}
	footer := fmt.Sprintf(`],"cantidad":%d}`, w.count)
	if _, err := w.f.WriteString(footer); err != nil {
		return fmt.Errorf("no se pudo cerrar el archivo: %w", err)
	}
	return nil
}
