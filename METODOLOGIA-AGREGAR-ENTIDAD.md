# Metodología para agregar una entidad nueva al mcp-server

Este documento existe porque el alcance va a crecer: hoy hay 7 entidades
(holding/grupo/cliente/centro/punto/dirección/salesPerson), pero el universo
real de Hermes tiene ~25. Cada alta nueva debe seguir estos mismos pasos,
en este orden, para no repetir la investigación a ciegas que costó resolver
`salesPerson` (ver `relaciones.json`, `otrasRelaciones`, para el caso real).

Principio rector (no negociable, ya establecido en `dictionary.go` y
reforzado el 2026-09-28): **lo técnico/estructural vive en Go y exige
recompilar; el contenido de negocio vive en `relaciones/*.json` y NO exige
recompilar.** La línea divisoria exacta está en el paso 2 vs. el paso 3 de
abajo. No mezclar las dos cosas — meter contenido de negocio en Go es
exactamente el error que este documento busca evitar para las próximas ~18
entidades.

## Paso 0 — Investigar la entidad antes de tocar nada

No inventar. Confirmar, en este orden de preferencia:

1. **Código fuente de Hermes** (`support-otc-hermes-hms-master`): buscar
   `entity.go` (tabla estática de prefijos — OJO, puede estar desactualizada,
   ver el caso de `salesPerson` donde la tabla decía `Prefix:"00"` pero el
   `Init()` real usaba `UCODE_FREE`) y `main.go`/`Init()` (la configuración
   REAL de runtime, la que manda). Confirmar: ¿tiene prefijo MDM de 2
   dígitos o es `UCODE_FREE`? ¿Tiene `ParentUCode` (cuelga de la jerarquía)
   o no (como `dirección`/`salesPerson`)?
2. **Excel `EntidadesOTC.xlsx`**: buscar una hoja `<Nombre>Data` con la
   misma estructura de columnas que las demás (S1..S10, JSON, Nombre/
   Descripción, Valores, alcance). Tratar como pista, no como verdad — puede
   tener campos "ADD X.Y" que no existen de verdad en el código, o marcar
   alcance "aplica a todos" incorrectamente (ver `longName`/`shortName` en
   cliente/centro).
3. **Adapters** (repos de integración tipo `adapter-meta4`, o cualquier
   otro que lea/escriba la entidad desde/hacia un sistema externo): sus
   structs de request/response, y sobre todo sus transforms (DataWeave o
   Go) que arman el mensaje de salida hacia Hermes o el mensaje de entrada
   que Hermes consume, muchas veces documentan la forma exacta de un campo
   mejor que el propio `model.go` de Hermes -- y son la fuente más confiable
   para entender de dónde sale un identificador externo (ej. un GUID de
   CRM) que Hermes solo guarda pero no genera ni interpreta. Mismo cuidado
   que con el código de Hermes: nunca editar el código de un adapter para
   esta investigación (ver la regla dura del proyecto de no tocar código de
   otros equipos), solo leerlo.
4. **Índices reales de la colección en UAT** (pedírselos a quien tenga
   acceso al Mongo real, como pasó con `salesPerson`): la lista de campos
   indexados es una pista fuerte de por cuáles campos se filtra/relaciona
   de verdad en producción, y puede revelar candidatos que ni el Excel ni
   el código de Hermes dejan ver a simple vista. No todo índice resulta
   útil (en el caso real de `salesPerson`, el índice `id` no llevó a nada:
   0 documentos de 1807 lo tenían poblado en esta base) -- tratarlos como
   candidatos a probar, no como confirmación por sí solos.
5. **Un documento real vía `reporting`** (una vez dada de alta en código,
   ver Paso 2): `{"coleccionInicial":"<entidad>","pipeline":[{"$limit":1}],
   "modo":"listado","mostrarEnPantalla":true}`. Este es el único paso que
   confirma de verdad si los campos anidan bajo `data.*` o van a nivel raíz
   — no asumir por analogía con las demás entidades. `$limit` como primera
   etapa no dispara la validación de prefijo `data.` del primer `$match`,
   así que es seguro usarlo para explorar a ciegas antes de saber la forma
   real del documento.

## Paso 1 — Decidir el alias de negocio

Elegir el nombre en español/alias de negocio que va a usar `coleccionInicial`
y los demás parámetros (ej. `salesPerson`, no `service...`). Si el nombre
técnico de colección Mongo coincide con el alias de negocio (como pasó con
`salesPerson`, a diferencia de las 6 originales que usan `serviceX`), decirlo
explícitamente en `mecanismoDeRelacion` para no repetir la confusión de
vocabulario ya documentada ahí.

## Paso 2 — Cambios de código (una vez por entidad, exige recompilar)

Estos 2-3 archivos son la ÚNICA parte que requiere tocar Go y recompilar
los dos binarios (Windows para Claude Desktop, Linux para `dist-linux`/Open
WebUI). Después de este paso, todo lo demás es edición de JSON.

1. **`internal/domain/entities.go`**:
   - Agregar la nueva constante a `EntityType`.
   - Agregarla a `AllTypes` (al final, si no participa de la jerarquía
     holding→grupo→cliente→centro→punto — como dirección y salesPerson; en
     el medio si sí participa).
   - Agregarla a `CollectionName` (nombre real de colección Mongo,
     confirmado en el Paso 0).
2. **`internal/tools/tools.go`**: agregar el alias de negocio a los 2
   `enum` de los schemas JSON (`dictionaryArgs.tipo`,
   `reportingArgs.coleccionInicial`). Son strings literales, no se leen de
   `relaciones.json` — si falta acá, el servidor rechaza la entidad aunque
   esté perfectamente documentada en el JSON. Ajustar las descripciones de
   las 3 tools si la entidad nueva tiene una particularidad relevante (ej.
   "no tiene padre/hijos", igual que se hizo para salesPerson).
3. **`internal/store/pipeline_guard.go`**: normalmente NO hace falta
   tocarlo — `coleccionesPermitidas` se arma automáticamente a partir de
   `domain.CollectionName`. Solo tocar si la entidad nueva tiene una regla
   de validación especial (ej. el caveat que se agregó sobre el prefijo
   `data.` para salesPerson, mientras su estructura no estaba confirmada).
4. **Compilar los dos binarios y reiniciar los dos servidores** (Claude
   Desktop + el que sirve a Open WebUI/mcpo vía `dist-linux`). Sin este
   paso la entidad no existe para ningún cliente MCP, sin importar cuánto
   se haya editado el JSON.

## Paso 3 — Contenido de negocio (solo JSON, repetible sin recompilar)

**Regla (directivas de diseño, §4): el JSON nunca lleva contenido de la
base.** Nada de conteos ("N documentos tienen X"), valores observados
("vistos en UAT: 'A', 'B'…"), copias de catálogos o tablas auxiliares
(códigos con su significado) ni ejemplos con datos reales (nombres,
ucodes, uuids). Se describe la estructura, el mecanismo de relación y el
significado de cada propiedad; si un dato está en una colección, la
descripción dice que se consulta con `reporting`. Consultar la base para
confirmar algo está bien; lo que no va en el JSON es el resultado de esa
consulta. Tampoco en `fuente`.

Editar los 5 ficheros de relaciones (ver lista al final) con las mismas
3 piezas, en este orden:

1. **`campos.<entidad>`**: un `FieldInfo` por campo real confirmado (no
   listar campos del Excel que no se confirmaron en el código o en un
   documento real — marcarlos como discrepancia si aparecen en una fuente y
   no en la otra, nunca como hecho). Cada entrada lleva `fuente` citando de
   dónde sale (código, Excel, o "confirmado contra un documento real de
   esta base" con fecha) — `fuente` es metadata interna: el MCP no se la
   entrega al agente.
2. **`prefijosUcode.<entidad>`**: el prefijo MDM de 2 dígitos del ucode,
   **SOLO SI** confirmaste que la entidad lo usa (no la tabla estática de
   `entity.go`, que puede mentir — ver Paso 0.1). Si usa `UCODE_FREE` o no
   tiene prefijo fijo, no se agrega (como salesPerson).
3. **`otrasRelaciones`**: agregar un ítem numerado nuevo describiendo cómo
   se vincula la entidad nueva con las demás — SOLO si se confirmó contra
   datos reales (ver Paso 4). Si la relación es una hipótesis sin confirmar,
   decirlo explícitamente ("NO CONFIRMADO", "hipótesis descartada: X, Y") en
   vez de presentarla como un hecho — el caso de `salesPerson.data.agentId`
   (se probó contra `ucode`, `salesOfficeId`, `id`, `parentUCode` antes de
   confirmar que era `uuid`) es el ejemplo de por qué importa dejar el
   rastro de lo descartado, no solo el resultado final.

## Paso 4 — Verificación antes de dar por cerrada el alta

1. Repetir el pipeline mínimo del Paso 0.5 si no se hizo antes de escribir
   el JSON — nunca escribir `campos.<entidad>` a partir de solo el Excel o
   solo el código sin cruzarlo contra un documento real, si hay acceso a
   Mongo para hacerlo.
2. Probar las 3 tools con la entidad nueva:
   - `diccionario_de_campos` con `tipo:"<entidad>"` — confirmar que
     devuelve los campos esperados.
   - `relacion_entre_entidades` — confirmar que aparece en `jerarquia`/
     `coleccionMongo` y que la nota nueva en `otrasRelaciones` se lee bien.
   - `reporting` con `coleccionInicial:"<entidad>"` y un `$match` real sobre
     un campo de `data.*` documentado — confirmar que la validación del
     primer `$match` (prefijo `data.`) no rechaza un campo real por mala
     suerte de nomenclatura (ver el caveat que se agregó en
     `pipeline_guard.go` para este caso).
3. Si se documentó una relación cross-entidad nueva en `otrasRelaciones`,
   confirmarla armando un pipeline real que la explote (ej. un `$lookup` o,
   si la conversión de tipos no lo permite como pasó con `salesPerson.uuid`,
   un cruce manual fuera de Mongo) — no alcanza con que "suene razonable".

## Paso 5 — Iterar con el usuario, no cerrar en soledad

Resolver una relación cross-entidad real casi nunca sale a la primera
(ver `salesPerson.data.agentId`: se probaron y descartaron `ucode`,
`data.salesOfficeId`, `id` y `parentUCode` antes de confirmar `uuid`, con
varias idas y vueltas). El flujo correcto NO es "investigar todo en
soledad y recién mostrar el resultado final" -- es ir y volver con el
usuario a medida que se prueba cada hipótesis:

1. Proponer la hipótesis más probable (ver Paso 0) y probarla con un
   pipeline real.
2. Mostrarle al usuario el resultado -- funcionó, no funcionó, o dio algo
   raro -- ANTES de escribir nada en `relaciones.json`. Si dio 0 o algo
   inesperado, decir explícitamente qué se descartó y por qué, y preguntar
   si seguir probando o si el usuario tiene una pista mejor (un índice de
   UAT, un adapter, alguien del equipo que sepa) -- exactamente como pasó
   acá.
3. Recién escribir/actualizar `relaciones.json` cuando haya algo confirmado
   (aunque sea parcial), y avisarle al usuario qué quedó documentado y qué
   sigue abierto. Si después aparece una corrección (como pasó dos veces
   con `salesPerson`: primero "no confirmado", después "confirmado pero
   con el campo equivocado", recién al final el mecanismo real), volver a
   editar el JSON las veces que haga falta -- no hay una única pasada
   definitiva, y dejar una entrada vieja sin corregir es peor que no
   haberla escrito.
4. No hace falta preguntarle al usuario antes de CADA prueba individual
   (probar un campo candidato es barato y reversible, no requiere permiso
   caso por caso) -- pero sí conviene levantar la cabeza y reportar cuando
   se agotan las hipótesis razonables antes de seguir tanteando a ciegas
   (ver el punto en el que se preguntó si continuar probando `salesPerson`
   o dejarlo documentado como no resuelto).

## Ficheros a editar en el Paso 3 (los 5, siempre)

- `mcp-server/relaciones/relaciones.json` (copia activa para Windows/Claude
  Desktop)
- `mcp-server/dist-linux/relaciones/relaciones.json` (copia activa para
  Linux/Open WebUI)
- `mcp-server/relaciones/relaciones.example.json` (plantilla versionada)
- `mcp-server/dist-linux/relaciones/relaciones.example.json` (plantilla
  versionada, copia Linux)
- `mcp-server/relaciones/relaciones.uat-mainaccountnumber.json` (snapshot
  con nombre propio — evaluar si sigue haciendo falta mantenerlo sincronizado
  a mano a medida que crece la lista de fixtures nombrados; si se agregan
  más snapshots así, considerar automatizar esta sincronización en vez de
  repetir el mismo bloque a mano 5+ veces)

`LoadRelations` (en `dictionary.go`) valida que `campos` no venga vacío
globalmente — pero no valida que TODAS las entidades de `domain.AllTypes`
tengan una entrada. Si se olvida un fichero de los 5, o una entidad queda
sin entrada, el servidor arranca igual pero `diccionario_de_campos` no
devuelve nada para esa entidad — no es un error fuerte, así que conviene
chequearlo a mano en el Paso 4, no asumir que el arranque exitoso
significa que todo quedó completo.

## Pendiente evaluado y descartado (por ahora) el 2026-09-28

Se evaluó mover también `AllTypes`/`CollectionName` y los
`enum` de `tools.go` a `relaciones.json`, para que una entidad nueva sea
100% JSON sin recompilar. Se descartó por ahora: es un refactor de
arquitectura real (esas variables hoy son globales de paquete fijadas antes
de que se lea `relaciones.json`; pasarlas a config exige inyectarlas como
parámetro en `mongostore.New`/`tools.RegisterAll`/etc., y perder la seguridad de
tipos en compilación de `domain.Cliente` como constante). Decisión de TAvo:
mantener el nombre de colección/prefijo MDM en el binario compilado
(seguridad), y el contenido de negocio en JSON — este documento es el
resultado de esa decisión. Si en algún momento el ritmo de altas de
entidades lo justifica, retomar esta evaluación citando este párrafo.
