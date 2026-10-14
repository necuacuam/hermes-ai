// Package dictionary carga y valida el fichero de conocimiento
// (relaciones/*.json) y arma las respuestas de diccionario_de_campos y
// relacion_entre_entidades. Este paquete define el formato y la logica; el
// contenido (campos y relaciones) vive en el fichero, para que cambiarlo no
// exija recompilar. Formato y reglas: docs/04-fichero-de-conocimiento.md.
package dictionary

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"hermes-mcp/internal/domain"
)

// FieldInfo es un campo documentado. Descripcion es lo que el agente le
// puede decir al usuario: significado y limitaciones, nunca contenido de la
// base ni como se averiguo. Fuente es la procedencia (codigo, documentacion,
// consulta): metadata interna que no se entrega al agente.
type FieldInfo struct {
	Campo       string `json:"campo"`
	Descripcion string `json:"descripcion"`
	Fuente      string `json:"fuente"`
}

// Lookup devuelve los campos documentados de un tipo, o los de todos si t
// es "".
func Lookup(campos map[domain.EntityType][]FieldInfo, t domain.EntityType) map[domain.EntityType][]FieldInfo {
	if t == "" {
		return campos
	}
	if fields, ok := campos[t]; ok {
		return map[domain.EntityType][]FieldInfo{t: fields}
	}
	return map[domain.EntityType][]FieldInfo{}
}

// Relacion es una relacion entre entidades, o entre una entidad y una tabla
// auxiliar, tal como la declara el fichero de conocimiento.
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
	// que lo tengan. Opcional.
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
// agente en ese caso (no nombrar un entorno) lo dicen los prompts, porque
// el comportamiento de los agentes va en los prompts (constitucion, sec. 2).
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
