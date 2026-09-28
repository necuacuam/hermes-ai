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
// que el agente puede decirle al usuario (mecanismo, limitaciones,
// tabla de codigos) -- CERO narrativa de "como llegamos a esto"
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

// HierarchyInfo es la data de como se relacionan estructuralmente los tipos
// de entidad -- el equivalente, para este proyecto, del "Grafo de entidades
// Hermes" que en la arquitectura completa genera el Agente Generador
// (parseando codigo + Excel en cada deploy a PRO) y que se consulta desde
// SharePoint.
//
// Jerarquia/PrefijosUCode/ColeccionMongo siguen viniendo de internal/domain
// (Go compilado): son hechos atados al codigo real que arma pipelines de
// verdad en otro lado del servidor (ver internal/store) -- no tiene sentido
// que un archivo externo pueda decir un nombre de coleccion distinto al que
// el propio codigo usa para consultar. El resto (mecanismo de relacion,
// notas de negocio, origenDatos) es texto libre que SI puede cambiar sin
// tocar codigo -- ver RelationsData/LoadRelations mas abajo (2026-09-19,
// evaluado y pedido por TAvo: mantener sincronizados dos binarios,
// Windows para Claude Desktop y Linux para el spin-off de Open WebUI/Qwen
// via mcpo, cada vez que cambia una nota de negocio o el origen de datos,
// era mas trabajo del necesario).
//
// A proposito esta tool NO toca Mongo ni datos vivos: la relacion
// estructural entre TIPOS (que un centro cuelga de un cliente, que existe
// un campo generico ParentUCode) cambia solo cuando cambia el modelo de
// Hermes, no con cada alta de un cliente real. Resolver una entidad REAL
// por nombre (¿existe un cliente llamado 'Macedonia'? ¿cual es su
// sucursal?) es un trabajo distinto, contra datos vivos -- eso lo hace la
// tool "seek".
type HierarchyInfo struct {
	Jerarquia           []domain.EntityType          `json:"jerarquia"`
	PrefijosUCode       map[domain.EntityType]string `json:"prefijosUcode"`
	ColeccionMongo      map[domain.EntityType]string `json:"coleccionMongo"` // nombre real de coleccion -- para armar $lookup en un pipeline
	MecanismoDeRelacion string                        `json:"mecanismoDeRelacion"`
	EstructuraDeCampos  string                        `json:"estructuraDeCampos"` // donde viven los campos de negocio -- ver comentario del campo mas abajo
	Direccion           string                        `json:"direccion"`          // nota aparte sobre por que "direccion" no encaja en la cadena de ParentUCode
	OtrasRelaciones     string                        `json:"otrasRelaciones"`    // FKs cross-entidad que NO son ucode/parentUCode -- ver Hierarchy()
	OrigenDatos         string                        `json:"origenDatos"`        // de que base/entorno viene lo conectado AHORA -- ver RelationsData
	Anidamiento         struct {
		SegunCodigoHermes           string `json:"segunCodigoHermes"`
		SegunDocumentacionDeNegocio string `json:"segunDocumentacionDeNegocio"`
		EstadoActual                string `json:"estadoActual"`
	} `json:"anidamiento"`
}

// RelationsData es el contenido del "fichero de relaciones" externo -- lo
// que antes vivia hardcodeado en Hierarchy() como texto libre, mas
// OrigenDatos (que antes era la variable de entorno HERMES_MCP_ORIGEN_DATOS,
// sacada 2026-09-19 a favor de este mecanismo -- ver HERMES_MCP_RELACIONES_PATH
// en main.go). Se carga UNA sola vez al arrancar el servidor, desde un
// archivo dentro de la carpeta relaciones/ que NO se versiona (mismo
// tratamiento que .env -- ver relaciones/relaciones.example.json, la
// plantilla que SI se versiona, con OrigenDatos en blanco). Puede haber
// varios (uno por entorno/release) -- HERMES_MCP_RELACIONES_PATH dice cual
// cargar.
//
// Por que esto y no seguir todo hardcodeado: si cambia una nota de negocio
// (o el entorno/Mongo conectado), alcanza con reemplazar este archivo y
// reiniciar el proceso -- no hace falta recompilar ni redistribuir el
// binario. Eso empieza a importar de verdad ahora que va a haber mas de un
// binario corriendo (Windows + Linux). La curacion del CONTENIDO (leer el
// Excel/codigo de Hermes e interpretar que significa cada cosa) sigue
// siendo 100% trabajo humano -- este mecanismo solo cambia COMO se
// distribuye el resultado, no quien ni como se genera. El diccionario de
// campos (antes fieldsByType, hardcodeado en este archivo) TAMBIEN vive
// aca desde 2026-09-20 (campo Campos, mas abajo) -- mismo mecanismo, misma
// razon, para las dos tools ("relacion_entre_entidades" y
// "diccionario_de_campos").
type RelationsData struct {
	// OrigenDatos: de que base/entorno viene lo conectado ahora mismo (ej.
	// "subconjunto restaurado de UAT, sept. 2026"). Puede quedar vacio a
	// proposito -- ver origenDatosNoConfigurado, la regla de "nunca
	// adivinar" sigue aplicando igual que antes.
	OrigenDatos string `json:"origenDatos"`
	// El resto son obligatorios (LoadRelations falla si vienen vacios) --
	// a diferencia de OrigenDatos, no tienen un fallback razonable: sin
	// esto la tool no tiene nada util que decir.
	MecanismoDeRelacion string `json:"mecanismoDeRelacion"`
	EstructuraDeCampos  string `json:"estructuraDeCampos"`
	Direccion           string `json:"direccion"`
	OtrasRelaciones     string `json:"otrasRelaciones"`
	Anidamiento         struct {
		SegunCodigoHermes           string `json:"segunCodigoHermes"`
		SegunDocumentacionDeNegocio string `json:"segunDocumentacionDeNegocio"`
		EstadoActual                string `json:"estadoActual"`
	} `json:"anidamiento"`
	// Campos: el diccionario de campos completo por tipo de entidad (la
	// tool "diccionario_de_campos", ver Lookup mas arriba). Obligatorio
	// igual que el resto (salvo OrigenDatos) -- sin esto esa tool no tiene
	// nada que devolver.
	Campos map[domain.EntityType][]FieldInfo `json:"campos"`
}

// LoadRelations lee y valida el fichero de relaciones -- falla fuerte
// (error, no un default silencioso) ante archivo ausente, JSON invalido,
// una clave desconocida (DisallowUnknownFields: un typo en el JSON no debe
// pasar inadvertido como "campo con zero value"), o cualquier campo
// obligatorio vacio salvo OrigenDatos (ver comentario en RelationsData).
// Mismo espiritu que HERMES_MCP_MONGO_URI en main.go: mejor cortar al
// arrancar con un mensaje claro que arrancar con datos a medias.
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
	if rel.MecanismoDeRelacion == "" {
		faltantes = append(faltantes, "mecanismoDeRelacion")
	}
	if rel.EstructuraDeCampos == "" {
		faltantes = append(faltantes, "estructuraDeCampos")
	}
	if rel.Direccion == "" {
		faltantes = append(faltantes, "direccion")
	}
	if rel.OtrasRelaciones == "" {
		faltantes = append(faltantes, "otrasRelaciones")
	}
	if rel.Anidamiento.SegunCodigoHermes == "" || rel.Anidamiento.SegunDocumentacionDeNegocio == "" || rel.Anidamiento.EstadoActual == "" {
		faltantes = append(faltantes, "anidamiento.*")
	}
	if len(rel.Campos) == 0 {
		faltantes = append(faltantes, "campos")
	}
	if len(faltantes) > 0 {
		return rel, fmt.Errorf("dictionary: fichero de relaciones %q incompleto -- faltan (o vienen vacios): %s", path, strings.Join(faltantes, ", "))
	}
	return rel, nil
}

// origenDatosNoConfigurado es lo que se devuelve en OrigenDatos cuando
// RelationsData.OrigenDatos viene vacio. No es un texto neutro cualquiera
// -- lleva la instruccion adentro a proposito (ver nota de TAvo 2026-09-16:
// un agente en vivo califico datos de prueba como "de esta restauracion
// UAT" solo porque el prompt tecnico lo decia como hecho fijo, cuando en
// realidad era una foto de un momento y ni siquiera del UAT completo, sino
// de un subconjunto). Poniendo la regla ACA, sobrevive aunque el prompt se
// desactualice o cambie: el agente la ve fresca en cada llamada a
// relacion_entre_entidades, no depende de que la recuerde de la
// conversacion o de texto estatico en su system prompt.
const origenDatosNoConfigurado = "No configurado en el fichero de relaciones (origenDatos vacio). NO asumas ni nombres un entorno (UAT, dev, QA, produccion, etc.) por conocimiento previo, por lo que diga cualquier prompt, o por lo que se haya dicho en otra conversacion -- ese dato no esta confirmado. Si hace falta comentar que unos valores parecen de prueba (placeholders, seriales repetidos, etc.), decilo en esos terminos genericos, sin nombrar un origen especifico."

// Hierarchy devuelve la relacion estructural entre los tipos, combinando lo
// fijo (Jerarquia/PrefijosUCode/ColeccionMongo, de internal/domain) con lo
// que viene del fichero de relaciones cargado al arrancar (ver
// RelationsData, LoadRelations, HERMES_MCP_RELACIONES_PATH en main.go).
func Hierarchy(rel RelationsData) HierarchyInfo {
	origen := rel.OrigenDatos
	if origen == "" {
		origen = origenDatosNoConfigurado
	}
	info := HierarchyInfo{
		Jerarquia:           domain.AllTypes,
		PrefijosUCode:       domain.MDMPrefix,
		ColeccionMongo:      domain.CollectionName,
		OrigenDatos:         origen,
		MecanismoDeRelacion: rel.MecanismoDeRelacion,
		EstructuraDeCampos:  rel.EstructuraDeCampos,
		Direccion:           rel.Direccion,
		OtrasRelaciones:     rel.OtrasRelaciones,
	}
	info.Anidamiento = rel.Anidamiento
	return info
}
