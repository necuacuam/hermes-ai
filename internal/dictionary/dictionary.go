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
// negocio tiene que vivir hardcodeado aca -- no hay fallback de "ir a
// mirar el Excel" ni "ir a mirar el codigo" en tiempo de ejecucion.
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

import "hermes-mcp/internal/domain"

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

// fieldsByType es deliberadamente un subconjunto -- los campos mas
// relevantes para responder preguntas de negocio sobre esta cadena de
// entidades, no una copia completa del modelo (que tiene mas de 60 campos
// por documento en cliente/centro/punto/grupo/holding, la mayoria de
// facturacion/CRM/sincronizacion sin relevancia para este alcance).
var fieldsByType = map[domain.EntityType][]FieldInfo{
	domain.Cliente: {
		{"ucode", "Identificador unico del cliente en Hermes (formato: pais + '02' + 8 digitos). Nivel raiz, no bajo 'data'.", "código (entity.go) + confirmado contra UAT"},
		{"parentUCode", "Ucode del grupo o holding del que depende, vacio si no tiene. Nivel raiz, no bajo 'data'.", "confirmado contra UAT"},
		{"data.customerName", "Razon social de la cuenta.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.legalName", "Razon Social (alcance Excel: Cliente).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.accountDescription", "Descripcion de la cuenta.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.longName / data.shortName", "El Excel los documenta como 'Razon social de la cuenta'/'Nombre corto de la cuenta', alcance 'Aplica a todos' -- pero NO EXISTEN en los documentos reales de cliente (comentados/ausentes en el modelo Go de serviceEntity). Discrepancia confirmada Excel-vs-codigo: no usar estos dos campos para cliente aunque el Excel diga que aplican a todos los tipos.", "Excel EntidadesOTC.xlsx dice 'aplica a todos'; codigo fuente Hermes confirma que NO existen para este tipo"},
		{"data.accountName", "Nombre de la cuenta -- pero en los documentos reales puede venir TRUNCADO a ~20 caracteres (ej. 'CAIXA CENTRAL DE CRE' en vez del nombre completo, un problema del sistema de origen, no del modelo). No usar como unica fuente de busqueda, preferir customerName/legalName/accountDescription.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT (truncamiento)"},
		{"data.taxpayerId / data.taxpayerRegistrationNumber", "Numero de documento del cliente (ERP) / concatenacion de pais + numero de documento. Es el identificador fiscal real: distinto NIF = cliente distinto.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.documentTypeCode / data.documentNumber / data.registrationNumber", "Codigo de tipo de documento / numero de documento del cliente / codigo pais + numero documento (ERP).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.dunsNumber / data.duns", "Codigo DUNS (Data Universal Numbering System, identificador global de empresa a 9 digitos).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.customerGroupCode / data.customerGroupName", "customerGroupCode SI es una FK utilizable: el Excel lo documenta como 'Lookup a account por parentaccount, si tipo cuenta=cliente tipo del parent=grupo' -- apunta al ucode de un grupo real (serviceGroup), no es solo un dato de copia. customerGroupName es el nombre ya resuelto (denormalizado), no sirve para JOIN.", "Excel EntidadesOTC.xlsx, hoja AccountData (texto 'Lookup...') + codigo fuente Hermes (mdm.go mapea Data.CustomerGroupCode al campo externo MDM 'Grupo')"},
		{"data.customerHoldingCode / data.customerHoldingName / data.customerHoldingCountryCode", "customerHoldingCode SI es una FK utilizable: el Excel lo documenta como 'Lookup a account por parentaccount del parentaccount, si tipo cuenta=cliente ... y tipo del parent del parent=holding' (sube dos niveles) -- apunta al ucode de un holding real (serviceHolding). customerHoldingName/CountryCode son datos ya resueltos (denormalizados), no sirven para JOIN.", "Excel EntidadesOTC.xlsx, hoja AccountData (texto 'Lookup...') + codigo fuente Hermes (mdm.go mapea Data.CustomerHoldingCode al campo externo MDM 'Holding')"},
		{"data.addressCode", "FK real hacia el ucode de un documento de 'direccion' (serviceAddress) -- NO es un dato de ubicacion en si mismo, es el enlace hacia la entidad direccion completa (calle/piso/coordenadas/etc). Cobertura confirmada contra UAT: 20088 de 20120 centros (99.8%) lo tienen poblado a nivel cliente/centro/punto -- similar deberia esperarse en cliente. Puede venir vacio o apuntar a un ucode que hoy no existe en serviceAddress (relacion opcional, no garantizada).", "Excel EntidadesOTC.xlsx, hoja AccountData (dice explicitamente 'Lookup a traves de account.pro_direccioncentro') + codigo fuente Hermes (on-merge-step-2-generate.go, getAddressFromDB hace ReadEntity contra serviceAddress usando este valor) + confirmado contra UAT"},
		{"data.recordType / data.recordTypeCode", "Tipo de registro que indica el tipo de cuenta. Valores: Holding=283900000, Grupo=283900001, Cliente=283900002, Centro=283900003, PuntoDeServicio=283900004.", "Excel EntidadesOTC.xlsx, hojas AccountData + AccountLists"},
		{"data.customerClass", "Indica si la cuenta es publica. Valores: administracion publica=283900000, no publica=283900001.", "Excel EntidadesOTC.xlsx, hojas AccountData + AccountLists"},
		{"data.clientType", "Tipologia del cliente. Valores: D=definitivo, I=cliente del grupo (intercia), P=potencial.", "Excel EntidadesOTC.xlsx, hojas AccountData + AccountLists"},
		{"data.strategicAccount", "Indica si se trata de una cuenta estrategica (solo aplica a cliente). Codificado como string '0'/'1'.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.financialEntity", "Booleano -- indica si el cliente es entidad financiera.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT (tipo bool en Mongo)"},
		{"data.segmentCode / data.segmentName / data.subsegmentCode / data.subsegmentName / data.segmentCode3 / data.segmentName3", "Rubro/sector de negocio del cliente (segmento, subsegmento, y un tercer nivel mas granular). El Excel marca el alcance como Cliente/Centro, pero el mismo campo existe tambien en grupo/holding/punto segun el codigo fuente -- ver nota en esos tipos.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.addressCity / data.addressCityName / data.addressProvince / data.addressProvinceName / data.addressCountry", "Ubicacion del cliente -- los que terminan en 'Name' son el texto legible, los otros son codigos internos (ID MDM de poblacion/provincia, codigo ISO de pais).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.addressName / data.addressNumber / data.addressFloor / data.addressStairs / data.addressDoor / data.addressPostalCode", "Componentes de la direccion del cliente: nombre de via, numero, piso, escalera, puerta, codigo postal.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.addressComplType / data.addressComplName / data.addressRegion / data.addressLatitude / data.addressLongitude / data.addressReferAreaNeighbourhood / data.addressInternational", "Complemento de direccion, region, coordenadas, colonia/barrio, e indicador de si la direccion es internacional.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.agentId / data.telephoneNumber / data.emailAddress / data.primaryContact", "Comercial principal, telefono, email y contacto principal de la cuenta.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.agents", "Array de todos los comerciales principales del cliente (cada elemento con agentId + business, ej. 'integra'/'vigilancia'/'tecnología'). Un cliente puede tener varios.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.contacts / data.documents", "Arrays de contactos y de documentos fiscales del cliente (documents: pais emisor + tipo + numero).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.asnef / data.lastValidationDate / data.suggestedPaymentMode / data.suggestedPaymentPeriod / data.limit / data.recommendation / data.scoring / data.creditClassification / data.grade / data.coverageStartingDate / data.coverageEndingDate", "Bloque de credito comercial (solo aplica a clientes): si esta en asnef, ultima validacion, forma/plazo de pago sugerido, limite, recomendacion, scoring, clasificacion crediticia, grade, y fechas de cobertura de seguro.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.validityStartingDate / data.validityEndingDate", "Fecha de inicio/fin de vigencia de la cuenta.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
	},
	domain.Centro: {
		{"ucode", "Identificador unico del centro (formato: pais + '03' + 8 digitos). Nivel raiz, no bajo 'data'.", "código + confirmado contra UAT"},
		{"parentUCode", "Ucode del cliente (o, si el anidamiento esta habilitado, de otro centro) del que depende. Nivel raiz, no bajo 'data'.", "confirmado contra UAT"},
		{"data.customerName / data.accountDescription", "Nombre de negocio del centro (razon social de la cuenta / descripcion de la cuenta).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.longName / data.shortName", "El Excel los documenta con alcance 'Aplica a todos' -- pero NO EXISTEN en los documentos reales de centro (comentados/ausentes en el modelo Go de servicePlace), igual que en cliente. No usarlos para este tipo.", "Excel EntidadesOTC.xlsx dice 'aplica a todos'; codigo fuente Hermes confirma que NO existen para este tipo"},
		{"data.accountName", "Tambien el nombre del centro, pero puede venir TRUNCADO a ~20 caracteres -- no usar como unica fuente de busqueda.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT (truncamiento)"},
		{"data.centerType / data.centerTypeCode", "Tipo de centro. Los valores reales son CODIGOS CORTOS -- vistos contra UAT: '', '1', 'AE', 'AG', 'BE', 'BP', 'CD', 'CL', 'GN', 'MA', 'NO', 'OP', 'ZS'. El Excel documenta que el campo existe ('Codigo tipo centro') pero NO trae la tabla de significado de cada codigo -- tampoco esta en AccountLists ni en el codigo fuente. No adivinar cual es 'Cámara' o similar sin esa tabla; si hace falta, avisar el hueco y preguntar a Hermes/MDM.", "Excel EntidadesOTC.xlsx, hoja AccountData (sin tabla de valores) + confirmado contra UAT"},
		{"data.addressCity / data.addressCityName / data.addressProvince / data.addressProvinceName / data.addressCountry", "Ubicacion del centro.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.addressName / data.addressNumber / data.addressFloor / data.addressStairs / data.addressDoor / data.addressPostalCode / data.addressComplType / data.addressComplName / data.addressRegion / data.addressLatitude / data.addressLongitude", "Componentes de la direccion del centro (calle, numero, piso, escalera, puerta, codigo postal, complemento, region, coordenadas). 'addressName' es el nombre de la via -- confirmado contra UAT que es el campo real usado como 'calle'.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.taxpayerId / data.taxpayerRegistrationNumber / data.documentTypeCode / data.documentNumber", "Un centro puede tener su propio NIF cuando representa una sucursal con entidad fiscal propia, distinta a la del cliente.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.billingCenter / data.billingName", "Indica si el centro es un centro de facturacion, y su nombre de facturacion (campos añadidos para la integracion con Marte).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.financialEntity", "Booleano -- indica si el centro (heredado del cliente) es entidad financiera.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.shopCode / data.subthirdCode", "Codigo de tienda a nivel de cuenta / codigo de subtercero del ERP (solo aplica en Mexico).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.municipalRegistration / data.stateRegistration", "Inscripcion municipal / estadual del centro (campos añadidos para la integracion con Marte).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.parentName", "Nombre del cliente padre, denormalizado y legible (ademas de poder subir por parentUCode).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.segmentCode / data.segmentName / data.subsegmentCode / data.subsegmentName / data.segmentCode3 / data.segmentName3", "Rubro/sector de negocio del centro. Mismo campo, presente en las 5 entidades de la cadena (ver nota en cliente).", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.addressCode", "FK real hacia el ucode de un documento de 'direccion' (serviceAddress) -- ver nota completa en cliente. Confirmado contra UAT: 20088 de 20120 centros (99.8%) lo tienen poblado.", "Excel EntidadesOTC.xlsx, hoja AccountData ('Lookup a traves de account.pro_direccioncentro') + codigo fuente Hermes (on-merge-step-2-generate.go) + confirmado contra UAT"},
		{"data.mergedMainCenter", "Autoreferencia centro->centro (no cliente->centro): tras una fusion de centros, apunta al ucode del centro 'principal' resultante. Formato de ucode valido confirmado en datos de prueba, pero no se encontro codigo en el repo que efectivamente lo use para hacer un lookup en tiempo de ejecucion -- tratarlo como dato informativo, no como mecanismo de traversal probado.", "código fuente Hermes (servicePlace_standard/model.go + datos de seed/test) -- NO esta documentado en el Excel"},
	},
	domain.Punto: {
		{"ucode", "Identificador unico del punto de servicio (formato: pais + '04' + 8 digitos). Nivel raiz, no bajo 'data'.", "código + confirmado contra UAT"},
		{"parentUCode", "Ucode del centro (o, si el anidamiento esta habilitado, de otro punto) del que depende. Nivel raiz, no bajo 'data'.", "confirmado contra UAT"},
		{"data.longName / data.customerName / data.accountName", "Nombre de negocio del punto (ej. 'Cámara 1', 'Rack 14'). A diferencia de cliente/centro, en punto SI existe data.longName segun el modelo -- pero en los documentos muestreados los tres campos vinieron con el MISMO valor, posiblemente truncado a ~20 caracteres. No se encontro un campo garantizado sin truncar para este tipo.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT (truncamiento sin resolver)"},
		{"data.pointOfServiceType / data.pointOfServiceTypeCode", "Tipo de punto de servicio. Los valores reales son CODIGOS CORTOS, no texto descriptivo -- vistos contra UAT: '', '1', 'CL', 'NO', 'GN'. El Excel documenta que el campo existe ('Codigo tipo punto de servicio') pero NO trae la tabla de significado -- tampoco esta en AccountLists ni en el codigo fuente. NO ADIVINAR el codigo (ej. cual corresponde a 'Cámara'); si hace falta, avisar el hueco o filtrar por nombre en vez de por tipo.", "Excel EntidadesOTC.xlsx, hoja AccountData (sin tabla de valores) + confirmado contra UAT"},
		{"data.additionalFields.itemClass / data.additionalFields.itemSubClass / data.additionalFields.serialNumber / data.additionalFields.brand / data.additionalFields.model", "Datos del equipo fisico instalado en el punto, si aplica -- anidados DOS niveles (bajo 'additionalFields', no directo bajo 'data'). Puede no estar poblado si el punto no tiene equipo asociado. IMPORTANTE: este grupo de campos NO aparece documentado en ninguna hoja del Excel EntidadesOTC -- se confirmo unicamente leyendo el codigo fuente real de Hermes (struct TAdditionalFields) durante una sesion de desarrollo puntual. Si se necesita ampliar o confirmar esto a futuro y no hay acceso al repo, coordinarlo con alguien que si lo tenga -- no adivinar la estructura.", "código fuente Hermes (servicePoint_standard/model.go, struct TAdditionalFields) -- NO esta en el Excel"},
		{"data.parentName", "Nombre del centro padre, denormalizado y legible.", "Excel EntidadesOTC.xlsx, hoja AccountData"},
		{"data.segmentCode / data.segmentName / data.subsegmentCode / data.subsegmentName", "Rubro/sector de negocio del punto. Mismo campo, presente en las 5 entidades de la cadena (ver nota en cliente).", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.addressCode", "FK real hacia el ucode de un documento de 'direccion' (serviceAddress) -- ver nota completa en cliente.", "Excel EntidadesOTC.xlsx, hoja AccountData ('Lookup a traves de account.pro_direccioncentro') + codigo fuente Hermes (on-merge-step-2-generate.go)"},
		{"data.customerId", "CUIDADO, comportamiento distinto al resto de la cadena: en punto este campo es una FK real que apunta directo al ucode del CLIENTE (dos niveles arriba, saltandose el centro) -- no al centro padre inmediato. Confirmado en codigo (on-merge-step-1-plan.go recalcula este campo como el ucode del abuelo) y en datos de seed. Util para llegar al cliente sin pasar por parentUCode+parentUCode.", "código fuente Hermes (modules/entity/merge/on-merge-step-1-plan.go) + datos de seed -- NO esta documentado como FK en el Excel"},
		{"data.additionalFields.itemClass (nombre de negocio: 'clase de punto') / data.additionalFields.itemSubClass (nombre de negocio: 'subclase')", "NO confundir con data.pointOfServiceType ('tipo de punto', otro campo distinto, sin tabla de significado) -- itemClass/itemSubClass son dos NIVELES de una clasificacion jerarquica del EQUIPO instalado en el punto (ej. clase='Extintores', subclase='PÓ QUÍMICO ABC'), consultable con solo la clase o con clase+subclase. NO son una FK hacia otra entidad -- son codigos de una TABLA AUXILIAR/CATALOGO externa a las 6 entidades de este servidor (coleccion Mongo 'operativeLevel', que NO tiene ucode/parentUCode ni participa del grafo de entidades: es una tabla de referencia, no una entidad). Esta relacion es EXCLUSIVA de punto -- ninguna otra entidad la usa. Estructura de 'operativeLevel': un documento por pais, con un array 'services' de elementos {productId, level1Code, level2Code, description} -- CADA ELEMENTO TIENE UN UNICO CAMPO 'description' de texto libre que ya trae el camino jerarquico concatenado (ej. 'Extintores → PÓ QUÍMICO ABC'), NO hay un campo de descripcion separado por nivel. El cruce se arma con un $lookup de solo lectura desde 'punto' hacia 'operativeLevel'. SIEMPRE se consulta el documento {country: 'CL'} de 'operativeLevel', sin importar el pais real del punto -- no hay logica de 'usar el pais del punto y caer a CL si falta', es CL siempre y en todos los casos; por eso la descripcion puede aparecer en un idioma distinto al del punto. El mismo par clase+subclase se repite en el catalogo bajo distintos 'productId' (mantenimiento correctivo, preventivo, instalacion, gestion de obras) -- agrupar por clase+subclase antes de mostrar resultados, no presentar esos duplicados como filas distintas. El catalogo de CL no tiene todas las clases/subclases que aparecen pobladas en puntos reales de otros paises -- puede faltar la traduccion de una CLASE ENTERA, no solo de una subclase puntual; no inventar el significado en ese caso, avisar que no hay traduccion disponible. Tabla de referencia (level1Code -> clase), confirmada contra el catalogo real: PO00001=Deteccion automatica de incendios, PO00003=Deteccion de gases, PO00007=Extintores, PO00008=Sistemas automaticos de extincion por agua (sprinklers y diluvio), PO00009=Sistemas de extincion por gases, PO00015=Carretes/mangueras de incendio, PO00025=Control de accesos, PO00089=Sistemas automaticos de extincion por espuma, PO00090=Hidrantes y bocas de incendio, PO00093=Sistemas automaticos de extincion para campanas de cocina, PO00094=Centrales de bombeo / abastecimiento de agua contraincendios, PO00620=Equipos de respiracion autonoma (ARICA), PO00627=Duchas lavaojos, PO00629=General/sin categoria especifica. NO resueltos contra el catalogo de CL (no inventar su significado): itemClass='PI00001', itemClass='PR00006', itemSubClass='POO0170' (bajo itemClass=PO00007), y en general cualquier subclase de otros paises que el catalogo de CL no tenga cargada.", "código fuente Hermes (support-otc-hermes-hms-master/modules/reference/handler.go, GetOperativeLevelList) + colección Mongo operativeLevel (bulk BR/CL cargado a mano en hermes-local) + confirmado por búsqueda completa en el repo de Hermes que ningún otro módulo de entidad usa itemClass/additionalFields, y que operativeLevel/OperativeLevelCollection no se referencia fuera de modules/reference y su test e2e -- NO está documentado en el Excel EntidadesOTC. Comparación BR vs CL (hecha a mano sobre el bulk) confirma que el mismo level1Code+level2Code trae una traducción literal entre los dos idiomas (ej. POO0176/177/178 bajo PO00001: 'Sistema de Detecção de Incêndio/Sistema Aspirador/Sirenes' en BR vs 'Sistema Detección/Sistema Aspiración/Sirenas' en CL) -- de ahí la confianza en usar un solo país como referencia. El uso fijo de CL (sin importar el país del punto) es decisión propia, no está en el código de Hermes -- se sostiene porque CL trae una fila general con level2Code vacío por cada clase que Brasil no tiene, lo que permite resolver una búsqueda por clase sola. itemclass.txt (volcado 2023, Portugal, hallado en un chat de Teams) se usó solo como evidencia de investigación para armar la tabla de arriba -- NO se usa como fuente de traducción en tiempo de ejecución porque no siempre coincide con la numeración viva del bulk BR/CL (ej. PO00025/POO0125 difiere entre ambas fuentes). ESTE CAMPO 'fuente' ES METADATA INTERNA DE MANTENIMIENTO -- nunca repetirlo ni parafrasearlo al usuario (ver regla dura del prompt)."},
	},
	domain.Grupo: {
		{"ucode", "Identificador unico del grupo (formato: pais + '01' + 8 digitos). Nivel raiz, no bajo 'data'.", "código + confirmado contra UAT"},
		{"parentUCode", "Ucode del holding, vacio si no tiene. Nivel raiz, no bajo 'data'.", "confirmado contra UAT"},
		{"data.customerName / data.longName / data.shortName / data.accountName / data.accountDescription", "Nombre de negocio del grupo (razon social / nombre corto / nombre de cuenta / descripcion). El Excel documenta estos 5 campos con alcance 'Aplica a todos', y a diferencia de cliente/centro, en grupo SI existen de verdad longName y shortName segun el codigo fuente. accountName puede aparecer vacio/ausente en algunos documentos -- eso no significa que el campo no exista, solo que no esta poblado para ese grupo puntual.", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.recordType / data.recordTypeCode", "Tipo de registro -- para grupo, valor fijo Grupo=283900001.", "Excel EntidadesOTC.xlsx, hojas AccountData + AccountLists"},
		{"data.segmentCode / data.segmentName / data.subsegmentCode / data.subsegmentName", "Rubro/sector de negocio del grupo. El Excel solo marca este campo con alcance Cliente/Centro, pero el codigo fuente confirma que tambien existe (y se puebla) en grupo -- no asumir que es exclusivo de cliente/centro pese a lo que diga el alcance del Excel.", "código fuente Hermes (serviceGroup_standard/model.go) corrige el alcance declarado en el Excel + confirmado contra UAT"},
	},
	domain.Holding: {
		{"ucode", "Identificador unico del holding (formato: pais + '00' + 8 digitos). No tiene padre. Nivel raiz, no bajo 'data'.", "código + confirmado contra UAT"},
		{"data.customerName / data.longName / data.shortName / data.accountName / data.accountDescription", "Nombre de negocio del holding -- mismo patron que grupo (ver esa nota).", "Excel EntidadesOTC.xlsx, hoja AccountData + confirmado contra UAT"},
		{"data.recordType / data.recordTypeCode", "Tipo de registro -- para holding, valor fijo Holding=283900000.", "Excel EntidadesOTC.xlsx, hojas AccountData + AccountLists"},
		{"data.segmentCode / data.segmentName / data.subsegmentCode / data.subsegmentName", "Rubro/sector de negocio. El Excel solo marca este campo con alcance Cliente/Centro, pero el codigo fuente confirma que tambien existe en holding -- no asumir que es exclusivo de cliente/centro. Si se necesita el segmento de un cliente puntual, leerlo directo de data.segmentName en el propio documento del cliente -- no hace falta subir a holding.", "código fuente Hermes (serviceHolding_standard/model.go) corrige el alcance declarado en el Excel + confirmado contra UAT"},
	},
	domain.Direccion: {
		{"ucode", "Identificador unico de la direccion (formato: pais + '05' + 8 digitos). En el esquema de ucode MDM esta entidad 'apunta a si misma' -- CONFIRMADO CONTRA UAT que el documento no tiene NINGUN campo parentUCode (ni vacio ni poblado), a diferencia de las otras 5 (ver grafo-entidades-hermes.md). Este hecho no esta en el Excel -- se confirmo contra datos reales.", "código + confirmado contra UAT"},
		{"data.streetName / data.number / data.streetType / data.streetComplement", "Nombre de la via, numero, tipo de via, y complemento de via.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.floor / data.door / data.staircase / data.complement", "Piso, puerta, escalera, y complemento de direccion.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.postalCode", "Codigo postal.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.descriptionLevel1 / data.descriptionLevel2 / data.descriptionLevel3 / data.descriptionLevel4", "Jerarquia geografica en texto legible (ej. descriptionLevel1='MADRID') -- cuantos niveles vienen poblados varia segun el pais.", "Excel EntidadesOTC.xlsx, hoja AddressData + confirmado contra UAT"},
		{"data.level1 / data.level2 / data.level3 / data.level4", "Los mismos niveles geograficos pero como codigo interno en vez de texto (ej. level1='ES', level2='ES028').", "Excel EntidadesOTC.xlsx, hoja AddressData + confirmado contra UAT"},
		{"data.latitude / data.longitude", "Coordenadas.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.countryCode", "Codigo de pais (pais propietario de la direccion).", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.timeZone", "Huso horario de la direccion.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"data.standardized", "Indica si la direccion fue normalizada.", "Excel EntidadesOTC.xlsx, hoja AddressData"},
		{"NO tiene nombre de negocio", "A diferencia de las otras 5 entidades, 'direccion' no tiene ningun campo tipo accountName/customerName/longName -- se identifica por sus componentes de domicilio de arriba, no por un nombre. No inventar un campo de nombre para esta entidad.", "Excel EntidadesOTC.xlsx, hoja AddressData (ninguna fila documenta un campo de nombre)"},
	},
}

// Lookup devuelve los campos documentados para un tipo. Si t es "" (vacio),
// devuelve todos los tipos.
func Lookup(t domain.EntityType) map[domain.EntityType][]FieldInfo {
	if t == "" {
		return fieldsByType
	}
	if fields, ok := fieldsByType[t]; ok {
		return map[domain.EntityType][]FieldInfo{t: fields}
	}
	return map[domain.EntityType][]FieldInfo{}
}

// HierarchyInfo es la data ESTATICA de como se relacionan estructuralmente
// los tipos de entidad -- el equivalente, para este proyecto, del "Grafo de
// entidades Hermes" que en la arquitectura completa genera el Agente
// Generador (parseando codigo + Excel en cada deploy a PRO) y que se
// consulta desde SharePoint. Aca esta hardcodeada porque no existe (todavia)
// un Agente Generador real -- es la misma idea, en chico.
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
	OrigenDatos         string                        `json:"origenDatos"`        // de que base/entorno viene lo conectado AHORA -- ver Hierarchy() y HERMES_MCP_ORIGEN_DATOS
	Anidamiento         struct {
		SegunCodigoHermes           string `json:"segunCodigoHermes"`
		SegunDocumentacionDeNegocio string `json:"segunDocumentacionDeNegocio"`
		EstadoActual                string `json:"estadoActual"`
	} `json:"anidamiento"`
}

// origenDatosNoConfigurado es lo que se devuelve en OrigenDatos cuando
// HERMES_MCP_ORIGEN_DATOS no esta seteada. No es un texto neutro cualquiera
// -- lleva la instruccion adentro a proposito (ver nota de TAvo 2026-09-16:
// un agente en vivo califico datos de prueba como "de esta restauracion
// UAT" solo porque el prompt tecnico lo decia como hecho fijo, cuando en
// realidad era una foto de un momento y ni siquiera del UAT completo, sino
// de un subconjunto). Poniendo la regla ACA, sobrevive aunque el prompt se
// desactualice o cambie: el agente la ve fresca en cada llamada a
// relacion_entre_entidades, no depende de que la recuerde de la
// conversacion o de texto estatico en su system prompt.
const origenDatosNoConfigurado = "No configurado en el servidor (HERMES_MCP_ORIGEN_DATOS vacia). NO asumas ni nombres un entorno (UAT, dev, QA, produccion, etc.) por conocimiento previo, por lo que diga cualquier prompt, o por lo que se haya dicho en otra conversacion -- ese dato no esta confirmado. Si hace falta comentar que unos valores parecen de prueba (placeholders, seriales repetidos, etc.), decilo en esos terminos genericos, sin nombrar un origen especifico."

// Hierarchy devuelve la relacion estructural entre los tipos, mas de que
// base/entorno viene lo que esta conectado ahora mismo (origenDatos, tal
// cual lo configuro quien corre el servidor -- ver HERMES_MCP_ORIGEN_DATOS
// en main.go). Ese dato SI depende de la config de cada corrida, a
// diferencia de todo lo demas en este struct -- por eso es el unico
// parametro que recibe.
func Hierarchy(origenDatos string) HierarchyInfo {
	origen := origenDatos
	if origen == "" {
		origen = origenDatosNoConfigurado
	}
	info := HierarchyInfo{
		Jerarquia:           domain.AllTypes,
		PrefijosUCode:       domain.MDMPrefix,
		ColeccionMongo:      domain.CollectionName,
		OrigenDatos:         origen,
		MecanismoDeRelacion: "Cada documento tiene un unico campo generico ParentUCode que apunta al ucode de su padre -- no hay una tabla de relaciones separada ni un campo distinto por tipo de padre. Para armar un $lookup en un pipeline, usar los nombres de 'coleccionMongo' como 'from' y 'parentUCode'/'ucode' como campos de join.",
		EstructuraDeCampos:  "ucode y parentUCode viven a NIVEL RAIZ del documento. Todo lo demas (nombres de negocio, NIF, tipo, ubicacion, etc.) vive ANIDADO bajo un campo 'data' -- un $match tiene que usar 'data.accountName', no 'accountName' a secas, o no va a matchear nada (asi es como esta documentado cada campo en diccionario_de_campos, con el prefijo 'data.' ya incluido en el nombre). Unica excepcion: 'direccion' no tiene ningun campo de nombre de negocio bajo 'data', solo componentes de domicilio.",
		Direccion:           "'direccion' (serviceAddress) esta incluida en el alcance por pedido explicito, pero no participa de la cadena holding->grupo->cliente->centro->punto: en el esquema de ucode MDM 'apunta a si misma', sin padre real. CONFIRMADO CONTRA UAT: el documento de 'direccion' ni siquiera tiene un campo 'parentUCode' (ni vacio ni poblado) -- a diferencia de las otras 5, que si lo tienen. No asumir un ParentUCode util para traversal -- si un pipeline necesita relacionar una direccion con otra entidad, buscar el campo de vinculo real en 'diccionario_de_campos' o preguntar, no asumir ParentUCode por analogia con las otras 5.",
		OtrasRelaciones:     "Ademas de ucode/parentUCode existen otros campos que funcionan como FK real hacia otra coleccion (o hacia la misma), confirmados contra codigo fuente y/o Excel: (1) data.addressCode (en cliente/centro/punto) apunta al ucode de un documento de 'direccion' -- es como se llega de cliente/centro/punto a serviceAddress, ya que direccion no tiene parentUCode; relacion opcional, puede estar vacio o no resolver. (2) data.customerGroupCode apunta al ucode de un 'grupo' real (serviceGroup), y data.customerHoldingCode al ucode de un 'holding' real (serviceHolding) -- son atajos denormalizados equivalentes a subir por parentUCode dos o tres niveles, no un camino distinto. (3) data.customerId en 'punto' es un caso especial: apunta directo al ucode del CLIENTE (dos niveles arriba, saltando el centro) en vez de al padre inmediato -- ver diccionario_de_campos de punto para el detalle. (4) data.mergedMainCenter en 'centro' es una autoreferencia centro->centro tras fusiones, con formato de ucode valido pero sin uso de lookup confirmado en el codigo -- tratarlo como informativo. Ver diccionario_de_campos de cada tipo para el detalle y la fuente de cada uno. APARTE de estas 4 (todas FK entre las 6 entidades), data.additionalFields.itemClass/itemSubClass ('clase'/'subclase' de punto, solo en 'punto', confirmado que ninguna otra entidad lo usa) se resuelve contra 'operativeLevel', que NO es una entidad sino una TABLA AUXILIAR/CATALOGO externa (sin ucode/parentUCode, fuera del grafo). El cruce es por valor (SIEMPRE contra el documento {country:'CL'} de 'operativeLevel', sin mirar el pais real del punto -- decision propia), muchos-a-uno, sin lookup real en el backend de Hermes. Ver diccionario_de_campos de punto para el mecanismo completo, la tabla de codigos, y las limitaciones de cobertura de subclase.",
	}
	info.Anidamiento.SegunCodigoHermes = "handle-create-code.go valida y rechaza que un centro dependa de otro centro, o que un punto dependa de otro punto -- un centro siempre cuelga de un cliente, un punto siempre cuelga de un centro."
	info.Anidamiento.SegunDocumentacionDeNegocio = "La Norma General 3P (Entidades de la Cuenta de Cliente) describe el anidamiento centro-en-centro y punto-en-punto como funcionalidad prevista, con ejemplos concretos (ej. el Bernabeu subdividido en zonas-centro, un rack con paneles como puntos hijos)."
	info.Anidamiento.EstadoActual = "CONFIRMADO CONTRA UAT (2026-09-08): 0 centros con padre-centro sobre 20072 servicePlace, y 0 puntos con padre-punto sobre 13976 servicePoint -- el anidamiento descrito en la Norma 3P no esta en uso hoy en datos reales, el codigo (que lo rechaza) refleja la realidad actual. La tool 'seek' sigue caminando ParentUCode sin mirar tipos de todas formas -- si el dia de mañana se habilita, sigue funcionando sin cambios."
	return info
}
