# Hermes AI: alcance y arquitectura

Así desarrollamos Hermes AI, un asistente que responde preguntas sobre los datos de Hermes en lenguaje natural, organizado según los patrones de diseño que implementa.

No cubre el despliegue (autenticación, hosting, almacenamiento compartido): esa parte todavía no está alineada con devops y hoy Hermes AI corre en local, desde Claude Desktop.

---

## 1. Alcance

- **Qué hace:** responde preguntas sobre las entidades de tipo cuenta de Hermes (holding, grupo, cliente, centro, punto, dirección) y los vendedores (`salesPerson`), más un catálogo auxiliar para traducir la clase de punto.
- **Qué no hace:** no escribe en la base, no consulta colecciones fuera de ese alcance y no responde con datos que no haya obtenido de una consulta en la misma conversación.

---

## 2. Patrones implementados

| Patrón | Qué resuelve | Dónde se aplica en Hermes AI |
|---|---|---|
| **NL-to-query** con *schema linking* | Traducir una pregunta en lenguaje natural a una consulta, dándole antes al modelo el esquema y su significado. | El agente consulta el diccionario y las relaciones, y después arma el pipeline (sección 4). |
| **Consulta libre validada** | Dar máxima flexibilidad al agente sin entregarle la base. | El agente escribe el pipeline; el servidor lo valida antes de ejecutarlo (sección 5). |
| **Capa semántica** | Separar el significado de los datos del motor que los consulta. | El JSON de conocimiento (sección 6). |
| **Aplicación de la política fuera del modelo** | Que ninguna instrucción al modelo pueda saltear una regla. | Todas las restricciones viven en el código del MCP Server, no en el prompt (sección 7). |
| **Mínimo privilegio y lista permitida** | Limitar qué se puede hacer a lo estrictamente necesario. | Etapas, colecciones y operadores permitidos; usuario de solo lectura (sección 7). |
| **Defensa en profundidad** | Que la seguridad no dependa de una sola barrera. | Validación en el servidor + tiempo máximo en la base + usuario de solo lectura (sección 7). |
| **Grounding** | Evitar que el modelo invente datos. | Toda respuesta sale de un resultado de una herramienta en la conversación en curso (sección 3). |
| **Control del tamaño de respuesta** | Que el resultado de una herramienta no desborde al agente ni se recorte en silencio. | Límite fijo en pantalla y archivo para el resultado completo (sección 8). |

El principio que une todo:

> El código es el motor, MongoDB los datos y el JSON son las reglas de negocio y las relaciones entre las entidades.

| Pieza | Contiene | Para cambiarla |
|---|---|---|
| Código (MCP Server) | Cómo se consulta, qué se permite, cómo se devuelve el resultado. | Recompilar. |
| MongoDB | Los datos. | Nada: se consultan siempre en vivo. |
| JSON de conocimiento | Significado de los campos, relaciones entre entidades, reglas de negocio. | Editar el JSON y reiniciar. Sin recompilar. |
| Prompts | Solo el comportamiento del agente. | Editar el prompt. |

### Arquitectura

```
 Usuario
   │  pregunta en lenguaje natural
   ▼
 Agente (Claude + system prompt)               ← grounding, comportamiento
   │  llama a herramientas MCP (stdio, JSON-RPC)
   ▼
 MCP Server (Go)                               ← política fuera del modelo
   ├─ diccionario_de_campos ───┐
   ├─ relacion_entre_entidades ┴─ capa semántica (JSON)   ← schema linking
   └─ reporting ──► validación ──► MongoDB (solo lectura) ← consulta libre validada
                                        │
                                        └─► pantalla (máx. N filas) y/o archivo JSON
                                                          ← control del tamaño
```

---

## 3. Grounding: los agentes y sus prompts

Hay dos agentes, cada uno definido por un system prompt. Usan las mismas herramientas y las mismas barreras; cambia solo el estilo de interacción.

|  | Negocio | Negocio + técnico |
|---|---|---|
| Lenguaje | Términos de negocio. No muestra campos, colecciones ni consultas. | Términos de negocio y los nombres reales de campos y colecciones. |
| Consulta usada | No la muestra. | La muestra si se la piden. |
| Pregunta ambigua | Pregunta antes de consultar. | Trae todos los candidatos para que el usuario elija. |

Reglas de grounding comunes a los dos:

- **No inventar datos.** Toda cifra, nombre, código o fecha sale de un resultado del MCP Server en la conversación en curso. Si falta un dato, lo dicen.
- **Toda tabla sale de `reporting`.** No arman tablas con conocimiento propio.
- **Citar el origen de los datos** tal como lo declara el MCP Server, y no presentar como regla de Hermes lo que es una observación sobre los datos.
- **Ante un resultado vacío,** explican las causas posibles y dejan elegir al usuario cómo seguir.
- **No elegir solos** entre dos lecturas posibles de una pregunta.
- **Mostrar el razonamiento** para que el resultado se pueda verificar.

Los prompts tienen **solo comportamiento**: no repiten conocimiento del modelo (campos, colecciones, relaciones, catálogos), que el agente obtiene siempre de la capa semántica. Así, un cambio en el modelo se hace en un solo lugar.

---

## 4. NL-to-query con schema linking: las herramientas

El MCP Server es un servidor en Go, por stdio (JSON-RPC), con tres herramientas:

| Herramienta | Qué hace | Toca la base |
|---|---|---|
| `diccionario_de_campos` | Devuelve, por entidad, cada campo técnico con su significado de negocio. | No. Responde desde el JSON. |
| `relacion_entre_entidades` | Devuelve las entidades y sus colecciones, las tablas auxiliares, la lista de relaciones (cada una con sus campos de cruce, su estado y sus reglas) y de qué base vienen los datos. | No. Responde desde el JSON y del registro de colecciones del código. |
| `reporting` | Ejecuta el pipeline de agregación que arma el agente, tal cual lo recibe, después de validarlo. | Sí, solo lectura. |

El flujo es el del schema linking: primero el agente consulta el diccionario y las relaciones para saber qué campos existen, qué significan y cómo se relacionan; después arma el pipeline y lo ejecuta con `reporting`. Nunca usa un campo que no figure en el diccionario.

---

## 5. Consulta libre validada

Hay tres formas habituales de dejar que un agente consulte datos:

| Enfoque | Cómo funciona | Flexibilidad | Riesgo |
|---|---|---|---|
| Herramientas predefinidas | Una herramienta por pregunta ("clientes por país"). | Baja | Bajo |
| API de capa semántica | El agente elige métricas y dimensiones ya definidas; la capa genera la consulta. | Media | Medio |
| **Consulta libre validada** | El agente escribe la consulta; el servidor la valida antes de ejecutarla. | **Alta** | Alto, controlado por la sección 7 |

Hermes AI usa la **consulta libre validada**: el agente puede responder preguntas que nadie anticipó (cruces entre entidades, agrupaciones nuevas, búsquedas sobre metadata de sincronización), a cambio de que toda la seguridad recaiga en la validación del servidor.

---

## 6. Capa semántica: el JSON de conocimiento

El JSON es lo que convierte un servidor genérico de consultas en un experto en Hermes. Tiene:

- **Origen de los datos:** de qué base o entorno viene lo conectado (por ejemplo, un subconjunto restaurado de UAT). El agente lo cita en vez de suponerlo.
- **Conocimiento técnico:** campos de cada entidad y una lista de relaciones, una entrada por relación:

  | Clave | Qué es |
  |---|---|
  | `tipo` | `padre` (cadena de dependencia), `anidamiento` (una entidad dentro de otra del mismo tipo), `referencia` (otra clave entre entidades) o `catalogo` (cruce con una tabla auxiliar). |
  | `desde` / `hacia` | Las entidades (o la tabla auxiliar) que une. |
  | `campoOrigen` / `campoDestino` | Los campos del cruce, listos para la consulta. |
  | `estado` | `confirmado`, `sin confirmar` o `informativo`. |
  | `reglas` | Los matices: cuándo viene vacío, qué rechaza el sistema, cómo resolverla. |

  Cada relación se describe una sola vez, y el servidor la valida al arrancar contra las entidades y tablas habilitadas en el código: un error de tipeo impide arrancar.
- **Conocimiento de negocio:** qué significa cada campo y cada código, y las reglas o decisiones del proyecto.

Reglas del JSON:

- **Nunca contiene contenido de la base:** ni conteos, ni valores observados, ni copias de catálogos, ni ejemplos con datos reales. Todo lo que está en una colección se consulta en vivo. Si el JSON copiara datos, quedaría desactualizado sin que nadie lo note.
- **Se valida al cargar:** una clave desconocida (un error de tipeo) impide arrancar, en vez de ignorarse en silencio.
- **Puede haber uno por entorno o versión,** y cada instalación indica cuál usa.
- **Puede llevar metadata de mantenimiento** (de dónde salió cada dato: código, Excel, verificación). El servidor no se la entrega al agente.

Para generarlo o actualizarlo se consulta el código de Hermes y el Excel con los términos de negocio, y las relaciones nuevas se confirman contra datos reales antes de darlas por buenas. Quienes mejor pueden mantenerlo son los equipos que desarrollan el sistema.

**Sumar una entidad o tabla auxiliar** requiere dos cambios: habilitar su colección en el código (recompilar) y documentarla en el JSON, con sus campos y sus relaciones. Los agentes no cambian: lo nuevo les llega por el MCP Server.

---

## 7. Política fuera del modelo, mínimo privilegio y defensa en profundidad

Todas las barreras están en el código del MCP Server. Un prompt puede pedir lo que quiera; el servidor decide qué se ejecuta.

**Lista permitida (mínimo privilegio):**

- **Etapas:** solo etapas de consulta (`$match`, `$project`, `$group`, `$lookup`, `$facet`, `$sort`, `$limit`, `$unwind`, etc.). Cualquier otra se rechaza.
- **Colecciones:** el punto de partida tiene que ser una entidad del alcance, y los cruces (`$lookup`, `$graphLookup`) solo pueden ir a colecciones del alcance o a tablas auxiliares habilitadas. La lista de colecciones está fija en el código, no en el JSON, justamente porque es lo que limita qué se puede consultar.

**Validación completa:**

- **Operadores prohibidos en cualquier posición,** aunque estén anidados dentro de una etapa permitida: los que escriben (`$out`, `$merge`), los que ejecutan código en el servidor (`$where`, `$function`, `$accumulator`, `$eval`) y los que exponen el estado interno de la base (`$currentOp`, `$collStats`, `$indexStats`, etc.).
- **Todos los niveles,** incluidos los sub-pipelines dentro de `$facet` y `$lookup`.
- **Claves duplicadas rechazadas,** para que lo que se valida sea exactamente lo que se ejecuta.

**Defensa en profundidad:**

| Capa | Barrera |
|---|---|
| MCP Server | Lista permitida y validación completa (arriba). |
| MCP Server + MongoDB | Tiempo máximo por consulta, aplicado en el servidor y pasado a MongoDB (`maxTimeMS`) para que la base misma corte. |
| MongoDB | **Recomendado:** usuario exclusivo, de solo lectura y solo sobre las colecciones del alcance, para que la protección no dependa únicamente del código. |
| Registros | La contraseña de conexión no aparece en registros ni en mensajes de error. |

---

## 8. Control del tamaño de respuesta: reporting y ficheros

`reporting` trabaja en dos modos:

| Modo | Qué devuelve | Restricciones |
|---|---|---|
| `conteo` | Un número. | El pipeline tiene que terminar en `$count` o un `$group` equivalente. Cero documentos es una respuesta válida (cantidad 0). Nunca genera archivo. |
| `listado` | Filas en pantalla y/o un archivo JSON. | En pantalla, como máximo N filas (configurable en el servidor, hoy 10), con aviso explícito si hay más. |

- **El límite lo pone el servidor,** no el prompt: el agente no recibe un parámetro para cambiarlo y tiene prohibido partir la consulta para juntar más filas.
- **El recorte nunca es silencioso,** y siempre se informa el total de filas, también cuando es 0.

**Ficheros,** cuando el usuario pide el resultado completo:

- **Formato JSON, siempre.** No hay CSV: un pipeline puede devolver documentos anidados y listas, y forzarlos a columnas fijas pierde datos o los deja ilegibles.
- **Resultado completo,** sin el límite de pantalla, escrito en streaming sin cargar todo en memoria.
- **Una carpeta por día** (con zona horaria configurable) dentro de la carpeta de salidas.
- **Nunca sobrescribe:** si el nombre ya existe ese día, agrega `_1`, `_2`, etc.
- **Sin resultados no hay archivo,** y la respuesta lo dice explícitamente.

> **Pendiente de despliegue:** la carpeta de salidas debería ser un almacenamiento compartido (por ejemplo, SharePoint) al que solo tengan acceso el usuario y el MCP Server.

---

## 9. Configuración

| Qué | Dónde |
|---|---|
| Conexión a la base, carpeta de salidas, límite de pantalla, tiempo máximo, zona horaria | `.env` al lado del binario (nunca se versiona). |
| Qué JSON de conocimiento usa la instalación | Configuración del cliente MCP (Claude Desktop). |
| Comportamiento de cada agente | System prompt del proyecto en Claude Desktop. |

Las credenciales y los JSON reales no se versionan; sí sus plantillas de ejemplo.

---

## 10. Lo que aprendimos

- **La fiabilidad sale del servidor, no del prompt.** Cada regla que dependía solo del texto del prompt (límite de filas, no mostrar metadata interna) terminó fallando en alguna prueba; se resolvió moviéndola al código.
- **El "no lo sé" del agente es útil.** Cuando la capa semántica no tiene una regla, el agente lo dice en vez de suponer, y eso muestra exactamente qué falta documentar.
- **Los datos en la capa semántica envejecen.** Copiar al JSON tablas o conteos de la base generó respuestas desactualizadas; ahora todo se consulta en vivo.
- **Endurecer sin recortar.** Al reforzar la seguridad conviene revisar si una capacidad ya está cubierta por otro control antes de quitarla.
