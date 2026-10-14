// Package dictionary implementa la tool "Diccionario de campos": traduce
// entre nombres tecnicos de campo y su significado de negocio.
//
// TODOS los campos listados abajo, salvo "ucode" y "parentUCode", viven
// ANIDADOS bajo "data" en el documento Mongo real -- el "campo" de cada
// FieldInfo ya incluye el prefijo "data." para que un pipeline armado con
// esta info funcione directo.
//
// Fuente principal: el Excel de referencia de negocio "EntidadesOTC.xlsx"
// (compartido por el equipo de Hermes/MDM), hojas "AccountData" (campos
// compartidos por cliente/centro/grupo/holding/punto -- el propio Excel
// las documenta juntas porque las 5 comparten un unico esquema base, con
// columnas de ejemplo separadas para "Example ServiceGroup"/"Example
// serviceEntity"/"Example servicePlace"), "AddressData" (campos de
// direccion) y "AccountLists" (tablas de codigo->significado, ej.
// recordTypeCode, customerClass, clientType). Esta es la fuente que hay
// que releer si aparece una entidad nueva o un campo nuevo: el agente que
// corre en produccion NO tiene acceso ni al Excel ni al repo de codigo de
// Hermes, asi que todo lo que hace falta para responder preguntas de
// negocio tiene que estar documentado aca -- no hay fallback de "ir a
// mirar el Excel" ni "ir a mirar el codigo" en tiempo de ejecucion. Desde
// 2026-09-20 el contenido en si (los FieldInfo por tipo) vive en el
// fichero de relaciones externo (RelationsData.Campos, ver mas abajo), no
// hardcodeado en este .go -- este archivo define el FORMATO (el struct
// FieldInfo) y la LOGICA (Lookup), no los datos.
//
// Un puñado de hechos ESTRUCTURALES (no de negocio) no estan en el Excel
// y solo se pudieron confirmar leyendo el codigo fuente real de Hermes
// durante una sesion de desarrollo puntual (con acceso al repo
// support-otc-hermes-hms-master, algo que el agente en produccion no
// tiene) -- estan marcados explicitamente como tales abajo, y quedan
// fijos aca porque no hay otra forma de que el agente los sepa:
//   - data.longName / data.shortName NO existen en los documentos reales
//     de cliente ni de centro (estan comentados/ausentes en el modelo Go
//     de esos dos tipos), aunque el Excel los marca como "Aplica a
//     todos" -- discrepancia real entre Excel y codigo, no error de
//     transcripcion.
//   - Los campos de equipo fisico de punto (itemClass, brand, model,
//     etc.) no aparecen documentados en ninguna hoja del Excel, y viven
//     anidados DOS niveles bajo data.additionalFields.*, no directo bajo
//     data.
//   - direccion no tiene ningun campo parentUCode en el documento real
//     (ni vacio ni poblado), a diferencia de las otras 5.
//
// Si en el futuro se agrega una entidad o un campo nuevo, el metodo es:
// (1) buscar en el Excel una hoja "<Nombre>Data" con la misma estructura
// de columnas (S1..S10 para el path anidado, "JSON" para el nombre real
// de campo, "Nombre"/"Description"/"Descripción" para el significado de
// negocio, "Valores" para tablas de codigo, "Tipo de cuenta a la que
// aplica" para el alcance); (2) si el campo no aparece en el Excel (como
// paso con additionalFields de punto), es necesario que alguien con
// acceso al repo de Hermes lo confirme contra el codigo fuente en una
// sesion de desarrollo, y lo agregue aca a mano -- no se puede resolver
// en tiempo de ejecucion.
package dictionary

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"hermes-mcp/internal/domain"
)

// BUG REAL CORREGIDO (2026-09-15, incidente en vivo con TAvo): la primera
// version de la entrada de itemClass/itemSubClass de punto metia TODA la
// narrativa de investigacion (que se encontro en un chat de Teams, la
// comparacion BR/CL con citas textuales, "confirmado por busqueda
// completa en el repo") directo en Descripcion -- y Descripcion es
// exactamente lo que el agente en produccion repite para explicarle el
// mecanismo al usuario. La regla dura del prompt ("el campo fuente es
// interno, nunca se menciona") no alcanzaba a proteger esto porque la
// fuga no estaba en Fuente, estaba en Descripcion. El usuario vio al
// agente citando "hallado en un chat de Teams" y con razon penso que
// habia usado una fuente externa no autorizada -- el agente solo repitio
// lo que esta tool le devolvio.
//
// Regla para toda entrada nueva de aca en mas: Descripcion es 100% lo
// que el agente puede decirle al usuario (mecanismo, limitaciones) --
// nunca contenido de la base (conteos, valores observados, copias de
// catalogos, ejemplos con datos reales: eso se consulta con reporting,
// ver directivas de diseño §4) -- y CERO narrativa de "como llegamos a esto"
// (Excel/codigo/chat/comparaciones hechas a mano, citas de investigacion).
// Toda esa procedencia va en Fuente, y solo en Fuente.
type FieldInfo struct {
	Campo       string `json:"campo"`
	Descripcion string `json:"descripcion"`
	Fuente      string `json:"fuente"`
}

// El diccionario de campos, desde 2026-09-20 (pedido por TAvo: "TENEMOS UN
// FICHERO EXTERNO CON EL CONTENIDO DE LAS ESTRUCTURAS, te has olvidado?" --
// corrigiendo que la ampliacion a "todos los campos" de ese mismo dia se
// habia hardcodeado aca en vez de sumarse al mecanismo externo que ya
// existia para 'relacion_entre_entidades'), YA NO vive en este archivo --
// vive en RelationsData.Campos (ver mas abajo), cargado desde el mismo
// fichero de relaciones externo que Hierarchy()/HERMES_MCP_RELACIONES_PATH.
// Mismo motivo que llevo a externalizar Hierarchy() el 2026-09-19: esto es
// contenido (que campos existen, que significan) curado por una persona
// leyendo Excel/codigo de Hermes, no logica -- no tiene sentido que
// cambiar una descripcion o agregar un campo exija recompilar y
// redistribuir dos binarios (Windows + Linux). Documentar TODOS los campos
// que persisten de verdad en Mongo (no bson:"-", no comentados/inactivos
// en el struct real) segun el codigo fuente de Hermes (los 6 model.go de
// support-otc-hermes-hms-master) sigue siendo la politica -- ver
// FieldInfo mas abajo para el formato de cada entrada, y
// relaciones/relaciones.example.json para el contenido completo.
//
// Enriquecimiento con el Excel EntidadesOTC.xlsx: los campos ya
// documentados antes de la ampliacion 2026-09-20 conservan su descripcion
// de negocio real (Fuente cita la hoja del Excel). Los agregados en esa
// ampliacion no se pudieron cruzar contra el Excel -- el bash de la sesion
// estaba roto (bug de un Windows Update, no afecta Claude Code) y no hubo
// otra forma de parsear un .xlsx en esa sesion -- quedan marcados en el
// JSON con una Fuente que dice explicitamente "confirmado que existe y
// persiste, pero sin cruzar contra el Excel ni UAT": no se invento
// significado de negocio mas alla del nombre del campo. Pendiente: repetir
// esa pasada cruzando contra el Excel (o contra quien conozca el dato)
// para subir esos campos de "confirmado" a "descripcion de negocio
// verificada" -- ahora que es un archivo JSON, ese trabajo se hace
// editando relaciones/*.json, sin tocar Go ni recompilar.

// Lookup devuelve los campos documentados para un tipo, leidos de "campos"
// (RelationsData.Campos, cargado desde el fichero de relaciones externo --
// ver comentario de arriba). Si t es "" (vacio), devuelve todos los tipos.
func Lookup(campos map[domain.EntityType][]FieldInfo, t domain.EntityType) map[domain.EntityType][]FieldInfo {
	if t == "" {
		return campos
	}
	if fields, ok := campos[t]; ok {
		return map[domain.EntityType][]FieldInfo{t: fields}
	}
	return map[domain.EntityType][]FieldInfo{}
}

// Relacion es una relacion entre entidades (o entre una entidad y una tabla
// auxiliar), tal como la declara el fichero de conocimiento (2026-10-14:
// antes las relaciones estaban repartidas en varios textos libres --
// mecanismoDeRelacion, direccion, otrasRelaciones, anidamiento -- y en las
// descripciones de los campos; ahora cada una es una entrada de esta lista).
//
// Desde es una lista para no repetir la misma relacion por cada entidad que
// la tiene (ej. data.addressCode en cliente, centro y punto). En tipo
// "anidamiento" Hacia va vacio: siempre es el mismo tipo que el origen.
// Fuente es metadata de mantenimiento, igual que en FieldInfo: no se le
// entrega al agente (ver relacionParaAgente).
type Relacion struct {
	Tipo         string              `json:"tipo"`
	Desde        []domain.EntityType `json:"desde"`
	Hacia        string              `json:"hacia,omitempty"`
	CampoOrigen  string              `json:"campoOrigen"`
	CampoDestino string              `json:"campoDestino"`
	Estado       string              `json:"estado"`
	Reglas       string              `json:"reglas"`
	Fuente       string              `json:"fuente,omitempty"`
}

// Tipos de Relacion.
const (
	RelacionPadre       = "padre"
	RelacionAnidamiento = "anidamiento"
	RelacionReferencia  = "referencia"
	RelacionCatalogo    = "catalogo"
)

var estadosDeRelacion = map[string]bool{"confirmado": true, "sin confirmar": true, "informativo": true}

// relacionParaAgente es lo que el agente recibe de cada relacion: todo
// menos Fuente.
type relacionParaAgente struct {
	Tipo         string              `json:"tipo"`
	Desde        []domain.EntityType `json:"desde"`
	Hacia        string              `json:"hacia,omitempty"`
	CampoOrigen  string              `json:"campoOrigen"`
	CampoDestino string              `json:"campoDestino"`
	Estado       string              `json:"estado"`
	Reglas       string              `json:"reglas"`
}

// HierarchyInfo es lo que devuelve relacion_entre_entidades. Tipos,
// ColeccionMongo y TablasAuxiliares vienen del registro (internal/domain,
// Go compilado): son lo que limita que se puede consultar (ver
// internal/store y las directivas de diseño, §4), asi que un archivo
// externo no puede cambiarlos. El resto viene del fichero de conocimiento.
//
// A proposito esta tool NO toca Mongo: la relacion entre TIPOS cambia solo
// cuando cambia el modelo de Hermes. Resolver entidades reales es trabajo
// de "reporting".
type HierarchyInfo struct {
	Tipos              []domain.EntityType          `json:"tipos"`
	ColeccionMongo     map[domain.EntityType]string `json:"coleccionMongo"` // nombre real de coleccion -- para el "from" de un $lookup
	TablasAuxiliares   []string                     `json:"tablasAuxiliares"`
	PrefijosUCode      map[domain.EntityType]string `json:"prefijosUcode,omitempty"`
	OrigenDatos        string                       `json:"origenDatos"`        // de que base/entorno viene lo conectado AHORA -- ver RelationsData
	EstructuraDeCampos string                       `json:"estructuraDeCampos"` // que campos estan en la raiz y cuales bajo 'data'
	General            string                       `json:"general,omitempty"`
	Relaciones         []relacionParaAgente         `json:"relaciones"`
}

// RelationsData es el contenido del fichero de conocimiento externo
// (relaciones/*.json, HERMES_MCP_RELACIONES_PATH en main.go). Se carga una
// sola vez al arrancar; cambiarlo no exige recompilar, solo reiniciar. Los
// .json reales no se versionan (mismo tratamiento que .env); la plantilla
// relaciones/relaciones.example.json si, con OrigenDatos en blanco.
type RelationsData struct {
	// OrigenDatos: de que base/entorno viene lo conectado ahora mismo. Puede
	// quedar vacio a proposito -- ver origenDatosNoConfigurado.
	OrigenDatos string `json:"origenDatos"`
	// EstructuraDeCampos: que campos estan en la raiz del documento y cuales
	// bajo 'data'. Obligatorio.
	EstructuraDeCampos string `json:"estructuraDeCampos"`
	// General: lo que no es una relacion concreta (ej. que vocabulario va en
	// coleccionInicial y cual en el "from" de un $lookup). Opcional.
	General string `json:"general,omitempty"`
	// PrefijosUCode: prefijo de 2 digitos del ucode por tipo, para los tipos
	// que lo tengan. Opcional (2026-10-14: una entidad sin ucode con prefijo
	// no lo necesita).
	PrefijosUCode map[domain.EntityType]string `json:"prefijosUcode,omitempty"`
	// Relaciones: todas las relaciones entre entidades y con tablas
	// auxiliares. Obligatorio, y cada entrada se valida contra el registro
	// (ver validarRelacion).
	Relaciones []Relacion `json:"relaciones"`
	// Campos: el diccionario de campos completo por tipo de entidad (la
	// tool "diccionario_de_campos", ver Lookup mas arriba). Obligatorio.
	Campos map[domain.EntityType][]FieldInfo `json:"campos"`
}

// LoadRelations lee y valida el fichero de relaciones -- falla fuerte
// (error, no un default silencioso) ante archivo ausente, JSON invalido,
// una clave desconocida (DisallowUnknownFields: un typo en el JSON no debe
// pasar inadvertido como "campo con zero value"), un campo obligatorio
// vacio, o una relacion que nombre un tipo, una tabla, un tipo de relacion
// o un estado que no existen. Mejor cortar al arrancar con un mensaje claro
// que arrancar con datos a medias.
func LoadRelations(path string) (RelationsData, error) {
	var rel RelationsData
	f, err := os.Open(path)
	if err != nil {
		return rel, fmt.Errorf("dictionary: abriendo fichero de relaciones %q: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rel); err != nil {
		return rel, fmt.Errorf("dictionary: parseando fichero de relaciones %q: %w", path, err)
	}

	var faltantes []string
	if rel.EstructuraDeCampos == "" {
		faltantes = append(faltantes, "estructuraDeCampos")
	}
	if len(rel.Relaciones) == 0 {
		faltantes = append(faltantes, "relaciones")
	}
	if len(rel.Campos) == 0 {
		faltantes = append(faltantes, "campos")
	}
	if len(faltantes) > 0 {
		return rel, fmt.Errorf("dictionary: fichero de relaciones %q incompleto -- faltan (o vienen vacios): %s", path, strings.Join(faltantes, ", "))
	}

	var errores []string
	for i, r := range rel.Relaciones {
		if err := validarRelacion(r); err != nil {
			errores = append(errores, fmt.Sprintf("relaciones[%d]: %v", i, err))
		}
	}
	for t := range rel.PrefijosUCode {
		if !esTipo(t) {
			errores = append(errores, fmt.Sprintf("prefijosUcode: %q no es un tipo del registro", t))
		}
	}
	for t := range rel.Campos {
		if !esTipo(t) {
			errores = append(errores, fmt.Sprintf("campos: %q no es un tipo del registro", t))
		}
	}
	if len(errores) > 0 {
		return rel, fmt.Errorf("dictionary: fichero de relaciones %q invalido -- %s", path, strings.Join(errores, "; "))
	}
	return rel, nil
}

func esTipo(t domain.EntityType) bool {
	for _, x := range domain.AllTypes {
		if x == t {
			return true
		}
	}
	return false
}

func esTablaAuxiliar(nombre string) bool {
	for _, x := range domain.TablasAuxiliares {
		if x == nombre {
			return true
		}
	}
	return false
}

// validarRelacion comprueba una entrada de 'relaciones' contra el registro:
// tipo y estado conocidos, origen entre los tipos del alcance, y destino
// segun el tipo de relacion (un tipo del alcance; una tabla auxiliar si es
// catalogo; vacio si es anidamiento, que siempre es al mismo tipo).
func validarRelacion(r Relacion) error {
	var problemas []string
	if len(r.Desde) == 0 {
		problemas = append(problemas, "falta desde")
	}
	for _, t := range r.Desde {
		if !esTipo(t) {
			problemas = append(problemas, fmt.Sprintf("desde: %q no es un tipo del registro", t))
		}
	}
	switch r.Tipo {
	case RelacionPadre, RelacionReferencia:
		if !esTipo(domain.EntityType(r.Hacia)) {
			problemas = append(problemas, fmt.Sprintf("hacia: %q no es un tipo del registro", r.Hacia))
		}
	case RelacionCatalogo:
		if !esTablaAuxiliar(r.Hacia) {
			problemas = append(problemas, fmt.Sprintf("hacia: %q no es una tabla auxiliar habilitada", r.Hacia))
		}
	case RelacionAnidamiento:
		if r.Hacia != "" {
			problemas = append(problemas, "hacia tiene que ir vacio en un anidamiento (siempre es al mismo tipo)")
		}
	default:
		problemas = append(problemas, fmt.Sprintf("tipo %q desconocido (padre, anidamiento, referencia o catalogo)", r.Tipo))
	}
	if !estadosDeRelacion[r.Estado] {
		problemas = append(problemas, fmt.Sprintf("estado %q desconocido (confirmado, sin confirmar o informativo)", r.Estado))
	}
	if r.CampoOrigen == "" {
		problemas = append(problemas, "falta campoOrigen")
	}
	if r.CampoDestino == "" {
		problemas = append(problemas, "falta campoDestino")
	}
	if r.Reglas == "" {
		problemas = append(problemas, "faltan reglas")
	}
	if len(problemas) > 0 {
		return fmt.Errorf("%s", strings.Join(problemas, ", "))
	}
	return nil
}

// origenDatosNoConfigurado es lo que se devuelve en OrigenDatos cuando
// RelationsData.OrigenDatos viene vacio. Es solo el dato: que hace el
// agente en ese caso (no nombrar un entorno) lo dicen los dos prompts
// (2026-10-07: antes la instruccion estaba escrita aca, y el comportamiento
// de los agentes va en sus prompts -- directivas de diseño, §2).
const origenDatosNoConfigurado = "No configurado."

// Hierarchy arma la respuesta de relacion_entre_entidades: el registro
// (internal/domain) mas el fichero de conocimiento cargado al arrancar (ver
// RelationsData, LoadRelations, HERMES_MCP_RELACIONES_PATH en main.go).
func Hierarchy(rel RelationsData) HierarchyInfo {
	origen := rel.OrigenDatos
	if origen == "" {
		origen = origenDatosNoConfigurado
	}
	relaciones := make([]relacionParaAgente, len(rel.Relaciones))
	for i, r := range rel.Relaciones {
		relaciones[i] = relacionParaAgente{
			Tipo:         r.Tipo,
			Desde:        r.Desde,
			Hacia:        r.Hacia,
			CampoOrigen:  r.CampoOrigen,
			CampoDestino: r.CampoDestino,
			Estado:       r.Estado,
			Reglas:       r.Reglas,
		}
	}
	return HierarchyInfo{
		Tipos:              domain.AllTypes,
		ColeccionMongo:     domain.CollectionName,
		TablasAuxiliares:   domain.TablasAuxiliares,
		PrefijosUCode:      rel.PrefijosUCode,
		OrigenDatos:        origen,
		EstructuraDeCampos: rel.EstructuraDeCampos,
		General:            rel.General,
		Relaciones:         relaciones,
	}
}
