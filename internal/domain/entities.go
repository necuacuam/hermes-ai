// Package domain es el registro: las entidades del alcance, su coleccion
// en Mongo y las tablas auxiliares habilitadas. Es lo que limita que se
// puede consultar, por eso vive en el codigo y no en el fichero de
// conocimiento. Como sumar una entidad: docs/05-alta-de-entidades.md.
package domain

// EntityType es el alias de negocio de una entidad (el que se usa en
// coleccionInicial).
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

// AllTypes lista las entidades del alcance. Como se relacionan entre si no
// esta aqui: lo dice la lista 'relaciones' del fichero de conocimiento.
var AllTypes = []EntityType{Holding, Grupo, Cliente, Centro, Punto, Direccion, SalesPerson}

// TablasAuxiliares son colecciones que no son entidades (no tienen
// ucode/parentUCode y no pueden ser coleccionInicial), pero que se pueden
// consultar como destino de un $lookup/$graphLookup: catalogos de
// referencia. La usan la validacion de pipelines y la del fichero de
// conocimiento (relaciones de tipo catalogo).
var TablasAuxiliares = []string{"operativeLevel"}

// CollectionName mapea cada entidad a su coleccion real en Mongo. Lo usan la
// ejecucion de pipelines, la validacion del "from" de un $lookup y
// relacion_entre_entidades, que se lo muestra al agente. salesPerson es la
// unica cuyo alias coincide con el nombre de la coleccion.
var CollectionName = map[EntityType]string{
	Holding:     "serviceHolding",
	Grupo:       "serviceGroup",
	Cliente:     "serviceEntity",
	Centro:      "servicePlace",
	Punto:       "servicePoint",
	Direccion:   "serviceAddress",
	SalesPerson: "salesPerson",
}
