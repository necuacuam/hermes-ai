// Package domain define las entidades del alcance acotado del proyecto:
// holding, grupo, cliente, centro, punto de servicio y direccion -- la
// cadena MDM de Hermes (prefijos 00-05), confirmada tanto por el codigo
// (core/hermes/entity/entity.go) como por la documentacion de negocio
// (Norma General 3P, "Entidades de la Cuenta de Cliente").
//
// serviceAddress ("direccion", prefijo 05) se agrego al alcance por pedido
// explicito de TAvo aunque conceptualmente sea la rara del grupo: en el
// esquema de ucode MDM "apunta a si misma" (no tiene padre real en la
// cadena), y por eso el grafo de dependencias de grafo-entidades-hermes.md
// la deja fuera del DAG holding->grupo->cliente->centro->punto. Aca se
// modela igual, con ParentUCode vacio salvo que en datos reales aparezca
// poblado -- el traversal (graph.go) no asume nada sobre esto, solo sigue
// el campo si esta presente.
package domain

// EntityType identifica el tipo de entidad dentro de la cadena MDM.
type EntityType string

const (
	Holding     EntityType = "holding"
	Grupo       EntityType = "grupo"
	Cliente     EntityType = "cliente"
	Centro      EntityType = "centro"
	Punto       EntityType = "punto"
	Direccion   EntityType = "direccion"
	SalesPerson EntityType = "salesPerson"
)

// AllTypes lista los 7 tipos -- usado por busquedas que no pueden asumir de
// antemano a que tipo pertenece una entidad (ver tool "seek": el usuario
// puede nombrar algo sin saber si es un cliente, un centro, etc.).
// SalesPerson se agrego 2026-09-28 (7ma entidad, pedido explicito de TAvo)
// -- va AL FINAL, fuera de la jerarquia holding->grupo->cliente->centro->
// punto (igual que direccion, que tampoco participa de esa cadena): no
// tiene parentUCode ni un prefijo MDM fijo (ver MDMPrefix mas abajo), asi
// que no cuelga de nadie ni nadie cuelga de el via ParentUCode -- se
// relaciona con cliente/grupo/holding por un campo distinto (data.agentId/
// data.agents, ver relaciones.json/otrasRelaciones), no por la cadena
// ucode/parentUCode que camina graph.go.
var AllTypes = []EntityType{Holding, Grupo, Cliente, Centro, Punto, Direccion, SalesPerson}

// MDMPrefix es el prefijo de 2 digitos que Hermes usa en el propio ucode
// para codificar el tipo de entidad (ver mdm-model.go / entity.go del repo
// de Hermes). Se guarda aca solo a fin informativo/documental -- el
// traversal de este proyecto NO depende de este prefijo para decidir a que
// tipo pertenece un documento (ver nota en graph.go sobre por que).
//
// SalesPerson A PROPOSITO NO esta aca: confirmado contra el codigo fuente
// real (main.go, Init()) que su UCodeType es UCODE_FREE con UCodePrefix
// vacio -- no usa el esquema de prefijo MDM de 2 digitos que si usan las
// otras 6 (la tabla estatica de entity.go dice Prefix:"00", pero esa tabla
// no refleja la configuracion real de runtime; ver investigacion
// 2026-09-2x). Agregarlo aca rompería mongostore.prefixToType (colisionaria
// con holding, que ya usa "00").
var MDMPrefix = map[EntityType]string{
	Holding:   "00",
	Grupo:     "01",
	Cliente:   "02",
	Centro:    "03",
	Punto:     "04",
	Direccion: "05",
}

// CollectionName mapea cada tipo a su coleccion real en Mongo -- fuente
// unica de verdad compartida entre mongostore (para saber donde leer/hacer
// $lookup) y dictionary (para que relacion_entre_entidades le pueda decir
// al subagente el nombre de coleccion exacto a usar como "from" en un
// $lookup, ya que el pipeline que arma el subagente habla en terminos de
// Mongo, no solo de tipos de negocio). Nombres CONFIRMADOS contra UAT
// (2026-09-08) para las primeras 5; "serviceAddress" es el nombre esperado
// segun el modulo Go (modules/entity/serviceAddress) pero no fue
// confirmado contra datos reales todavia -- si el pipeline contra esa
// coleccion no encuentra nada, revisar este nombre primero. "salesPerson"
// (agregado 2026-09-28): coleccion real, mismo nombre que el tipo de
// negocio (a diferencia de las otras 6, que usan un nombre tecnico
// "serviceX" distinto del alias de negocio) -- confirmado contra
// entity.go/main.go del repo de Hermes.
var CollectionName = map[EntityType]string{
	Holding:     "serviceHolding",
	Grupo:       "serviceGroup",
	Cliente:     "serviceEntity",
	Centro:      "servicePlace",
	Punto:       "servicePoint",
	Direccion:   "serviceAddress",
	SalesPerson: "salesPerson",
}

// Document representa un documento generico de cualquiera de las 6
// entidades. Se modela de forma unificada (en vez de un struct por tipo)
// porque el traversal tiene que poder recorrer la jerarquia sin asumir de
// antemano el tipo del padre o de los hijos -- ver la nota sobre
// anidamiento en graph.go.
type Document struct {
	UCode       string     `json:"ucode"`
	ParentUCode string     `json:"parentUCode,omitempty"` // vacio si no tiene padre (ej. un holding, o direccion -- ver nota del paquete)
	Type        EntityType `json:"tipo"`
	Name        string     `json:"nombre"` // nombre de negocio (longName/accountName/etc.) -- por lo que se busca en lenguaje natural
	// Attributes guarda campos propios del tipo (ej. centerType,
	// pointOfServiceType, taxpayerId) como pares clave/valor -- alcanza
	// para el alcance actual del proyecto sin tener que declarar un
	// struct Go distinto por cada _standard/model.go real de Hermes.
	//
	// map[string]interface{}, no map[string]string: CONFIRMADO CONTRA UAT
	// (2026-09-11, via reporting con un $limit:1 sin filtro) que el objeto
	// "data" real de Hermes mezcla tipos -- strings, booleanos
	// (billingCenter, financialEntity, merged) y arrays (agents, contacts)
	// -- map[string]string no puede decodificar eso. Ver
	// AttributeContainsFold en match.go para el ajuste correspondiente.
	Attributes map[string]interface{} `json:"atributos,omitempty"`
}

// ContainsFold y AttributeContainsFold (comparacion "contiene", sin
// distinguir mayusculas/acentos) viven en match.go.
