# Historial de decisiones

Por qué Hermes AI es como es. Los demás documentos describen el estado actual; este registra cómo se llegó, en orden cronológico. Cada entrada: qué se decidió y por qué.

## 2026-09-13 — Versión inicial

Servidor MCP con cuatro herramientas: `diccionario_de_campos`, `relacion_entre_entidades`, `seek` (búsqueda de entidades por nombre) y `reporting`. Seis entidades: holding, grupo, cliente, centro, punto y dirección.

El diccionario se armó estudiando a la vez el Excel de términos de negocio (`EntidadesOTC.xlsx`) y el código de Hermes, porque ninguno alcanza solo: hay campos reales que el Excel no documenta (los del equipo instalado en un punto, o el catálogo `operativeLevel` completo) y discrepancias entre los dos (por ejemplo, `longName`/`shortName` declarados para todas las entidades pero ausentes en cliente y centro). Lo que no se pudo resolver con esas fuentes se confirmó contra una copia de UAT.

## 2026-09-10 — Solo stdio

El servidor podía servir MCP por TCP para un túnel SSH inverso entre dos equipos. Se quitó: todo corre en una sola máquina y Claude Desktop solo necesita stdio. (El código del túnel se borró del todo el 2026-10-07.)

## 2026-09-11 — `reporting` con pipeline libre y su validación

`reporting` dejó de aceptar solo parámetros fijos y pasó a ejecutar un pipeline armado por el agente. Eso agranda lo que se puede ejecutar contra la base, así que se agregó la validación: lista blanca de etapas de lectura, operadores prohibidos en cualquier posición y control del `from` de los `$lookup`, que hasta entonces podía apuntar a cualquier colección de la base. Se agregaron también los primeros tests de la validación, la pieza más crítica del servidor. El mismo día se definieron la lectura por lotes de los resultados que van a archivo y el tiempo máximo por consulta.

## 2026-09-15 — Clase y subclase de punto contra `operativeLevel`

`itemClass`/`itemSubClass` se traducen contra el catálogo `operativeLevel`, habilitado como tabla auxiliar (solo destino de `$lookup`).

**Decisión del proyecto:** siempre se usa el catálogo de Chile (`country: "CL"`), sea cual sea el país del punto. No es una regla de Hermes, que no tiene ninguna lógica por país. Motivos:
- El catálogo de Chile trae, por cada clase, una fila general con la subclase vacía. Brasil no la tiene, y es la que permite traducir una clase sola.
- Comparando Brasil y Chile, el mismo código trae la misma clasificación; solo cambia el idioma.

Limitación conocida: el catálogo de Chile tiene menos subclases que las que usan otros países, así que una subclase puede no tener traducción aunque su clase sí la tenga.

**Incidente:** la primera versión de la descripción de `itemClass` incluía toda la investigación (dónde se encontró cada dato, comparaciones, citas). El agente la repitió al usuario y pareció haber usado fuentes no autorizadas. De ahí la regla: la descripción es solo lo que se le puede decir al usuario; la procedencia va en `fuente`.

## 2026-09-16 — Origen de los datos y configuración en un solo lugar

- `origenDatos` pasó al fichero de conocimiento. Un agente había calificado datos de prueba como "de producción" porque el prompt daba el entorno por hecho. Ahora el entorno lo declara cada instalación, y si no lo declara, el agente no nombra ninguno.
- `HERMES_MCP_MONGO_URI` dejó de tener un valor por defecto en el código: había dos lugares con el mismo dato. Ahora es obligatoria en el `.env`.
- El ejecutable empezó a leer el `.env` por sí mismo. Antes solo lo leían los scripts de arranque, y cuando Claude Desktop lanzaba el ejecutable directamente, el `.env` se ignoraba.

## 2026-09-19 y 2026-09-20 — Conocimiento fuera del código

Las notas de relaciones (2026-09-19) y después el diccionario de campos completo (2026-09-20) pasaron del código Go al fichero de conocimiento. Cambiar una descripción no debe exigir recompilar ni redistribuir dos ejecutables (Windows para Claude Desktop y Linux para Open WebUI). El registro de entidades y colecciones se quedó en el código, por seguridad: es lo que limita qué se puede consultar.

## 2026-09-21 — Anidamiento por `mainAccountNumber`

Leyendo el código de Hermes (ramas master y develop, idénticas en esto) se confirmó que el anidamiento de una entidad dentro de otra del mismo tipo se modela con `data.mainAccountNumber`, no con `parentUCode`: Hermes rechaza que un centro dependa de otro centro o un punto de otro punto.

## 2026-09-28 — salesPerson, séptima entidad

- Es la primera entidad cuyo alias coincide con el nombre de la colección, y su ucode es libre (sin prefijo de tipo): la tabla de prefijos de `entity.go` decía otra cosa, pero manda la configuración de `Init()`.
- La relación con cliente costó varias hipótesis: se probaron y descartaron `ucode`, `data.salesOfficeId`, `id` y `parentUCode` antes de confirmar que `data.agentId` apunta al `uuid` binario. Esta versión de MongoDB no convierte binario a texto dentro de un pipeline, así que el cruce se resuelve en dos consultas.
- De esta experiencia salió la metodología de alta de entidades (hoy [05-alta-de-entidades.md](05-alta-de-entidades.md)).
- **Evaluado y descartado:** mover también el registro (`AllTypes`, `CollectionName`) al fichero de conocimiento para que una entidad nueva fuera solo JSON. Se descartó para mantener en el código, por seguridad, lo que limita qué se puede consultar. Retomar si el ritmo de altas lo justifica.

## 2026-09-25 y 2026-10-03 — Controles para un modelo más débil, y su retiro

El 2026-09-25 se agregaron dos controles por errores de un modelo más débil usado desde Open WebUI: exigir el prefijo `data.` en los campos del primer `$match` y rechazar un espacio antes del `$` de un operador. El 2026-10-03 se quitaron: ese modelo ya no se usaba, y el primero bloqueaba campos reales de la raíz (`uuid`, `countryCode`, `revision`). Las reglas de seguridad no dependen del modelo.

## 2026-10-03 — Extended JSON en `reporting`

El pipeline se lee como Extended JSON (`$uuid`, `$date`, `$oid`, `$binary`). Hizo posible el cruce de clientes con vendedores por `uuid`. Además, cada etapa se ejecuta desde su texto original: antes pasaba por un mapa y un `$sort` por varios campos llegaba a MongoDB en orden alfabético, sin error.

## 2026-10-07 — Seguridad frente a la base y constitución

- **Validación recursiva:** la lista cerrada de etapas y el control de colecciones se aplican también dentro de `$facet` y de `$lookup.pipeline`.
- **Claves duplicadas rechazadas,** para que lo que se valida sea exactamente lo que se ejecuta.
- **Sin `$skip`:** solo servía para paginar, y el agente no debe juntar más filas que el límite.
- **Tiempo máximo** también en MongoDB (`maxTimeMS`), y la URI sin contraseña en los registros.
- **Regla "no inventar datos"** en los prompts: toda cifra, nombre o código sale de un resultado de la conversación.
- **La `fuente` deja de entregarse al agente.** Antes la protegía solo una instrucción del prompt.
- **Sin modo de datos de prueba en memoria:** el servidor exige una base real.
- La zona horaria de la carpeta de archivos pasó a ser configurable.
- Las directivas de diseño pasan a ser la **constitución** oficial del proyecto.

## 2026-10-14 — Sin `seek`, límite fijo y JSON sin contenido de la base

- **Se quitó `seek`:** toda tabla que ve el usuario sale de `reporting`, que respeta el límite de pantalla.
- **El límite de pantalla lo fija el servidor.** Desde la versión inicial el agente podía pedir el límite que quisiera, y lo hacía; además partía consultas para juntar más filas. Ahora el límite es configuración del servidor y los prompts prohíben partir consultas.
- **El fichero de conocimiento se limpió de contenido de la base:** conteos, listas de valores observados, una tabla de clases copiada del catálogo y datos personales de ejemplo. Regla incorporada a la constitución: el JSON describe estructura, relaciones y significado; los datos se consultan.
- **Prompts solo de comportamiento:** se quitaron las tablas de colecciones, clases y otros conocimientos duplicados del JSON.
- Se corrigió una generalización errónea: el `countryCode` de la raíz es el país de la entidad (no un dato de sincronización) y normalmente coincide con el prefijo del ucode, aunque no siempre.
- Se corrigió el formato descrito de la descripción de clases: el primer tramo es el servicio, no la clase.

## 2026-10-14 — Relaciones unificadas

Las relaciones estaban repartidas en cuatro textos libres y en las descripciones de los campos, con duplicaciones y una contradicción (`parentUCode` de centro y punto decía que podía anidar; el código de Hermes lo rechaza). Pasaron a una lista `relaciones`, una entrada por relación, validada al arrancar contra el registro. Decisiones:
- Ante una contradicción entre fuentes, manda el código de Hermes; si cambia, el fichero se adapta.
- `parentUUID` se documenta como relación informativa: Hermes lo guarda al crear la entidad y no lo actualiza, así que puede faltar o estar desactualizado.
- `prefijosUcode` pasa a ser opcional.
- Los `enum` de las herramientas y la lista de tablas auxiliares salen del registro, para que sumar una entidad no exija tocar el código en varios lugares.

## 2026-10-14 — Documentación normalizada

La documentación se reorganizó en `docs/` (constitución, arquitectura, instalación, fichero de conocimiento, alta de entidades, glosario e historial), en español neutro. Los comentarios del código dejaron de contar la historia: explican el porqué, y la historia quedó en este documento.

## 2026-10-14 — Línea futura: enseñar al agente

Decidido, sin implementar (ver la constitución, sección 7): un perfil educador podrá proponer conocimiento de negocio de propiedades de colecciones existentes; un curador lo aprueba, y las versiones del JSON se guardan en una base propia de Hermes AI.

## 2026-10-15 — Protocolo MCP con `mark3labs/mcp-go`

El protocolo MCP estaba implementado a mano, para no depender de librerías externas en una red corporativa restringida. Al pasar el código al repositorio corporativo ese motivo dejó de aplicar, y se reemplazó por la librería `mark3labs/mcp-go`, a pedido de quienes van a desplegarlo. Por ahora solo con stdio; el transporte HTTP queda para el despliegue. Para minimizar y estandarizar el código, las herramientas se registran directamente en la librería y usan sus tipos; el paquete propio `internal/mcp` desapareció. Los argumentos de las herramientas se siguen recibiendo como texto original, para conservar el orden de las claves del pipeline. La librería exige Go 1.25.5 o superior.

## 2026-10-16 — Transporte HTTP y lab con Open WebUI

El servidor puede atender también por HTTP (`HERMES_MCP_TRANSPORTE=http`), con el transporte "Streamable HTTP" de `mcp-go`; stdio sigue siendo el modo por defecto, así que Claude Desktop no cambia. Se agregó una imagen de Docker y un lab local (`lab/`) con Open WebUI, contra la misma base aprobada para las pruebas. La configuración y el fichero de conocimiento no van dentro de la imagen: se montan al arrancar, para que cambiarlos no exija reconstruirla. El HTTP todavía no tiene autenticación: es el primer paso hacia el despliegue con Entra ID y la escritura en OneDrive del usuario.

## 2026-10-16 — Lab: login con Entra ID y perfiles por grupo

Open WebUI inicia sesión con Microsoft Entra ID (registro de aplicación en un directorio propio, solo para el lab). Los perfiles se arman con grupos de Open WebUI, y cada grupo necesita acceso tanto al modelo de su perfil como a la conexión con `hermes-mcp`. Probado con un usuario del directorio: ve solo su modelo y obtiene los mismos resultados que el administrador. Cuando a ese usuario le faltó el acceso a la conexión, el agente no inventó datos: avisó que las herramientas no devolvían nada.
