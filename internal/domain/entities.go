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
// poblado.
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

// AllTypes lista los 7 tipos del alcance (los 'tipos' que devuelve
// relacion_entre_entidades). Como se relacionan entre si no esta aca: lo
// dice la lista 'relaciones' del fichero de conocimiento (2026-10-14).
// SalesPerson se agrego 2026-09-28 (7ma entidad).
var AllTypes = []EntityType{Holding, Grupo, Cliente, Centro, Punto, Direccion, SalesPerson}

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
// TablasAuxiliares son colecciones que NO son entidades (no tienen
// ucode/parentUCode, no pueden ser coleccionInicial) pero que se permite
// consultar como destino de un $lookup/$graphLookup de solo lectura --
// catalogos de referencia. Vive en el registro (2026-10-14, antes en
// store/pipeline_guard.go) porque la usan la validacion de pipelines y la
// del fichero de conocimiento (relaciones de tipo catalogo).
var TablasAuxiliares = []string{"operativeLevel"}

var CollectionName = map[EntityType]string{
	Holding:     "serviceHolding",
	Grupo:       "serviceGroup",
	Cliente:     "serviceEntity",
	Centro:      "servicePlace",
	Punto:       "servicePoint",
	Direccion:   "serviceAddress",
	SalesPerson: "salesPerson",
}
