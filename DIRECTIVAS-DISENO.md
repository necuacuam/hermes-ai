# Directivas de diseño de Hermes AI

Base para construir Hermes AI y para contrastar cada cambio que se haga.

## 1. Componentes

Hermes AI consta de dos componentes: **Agentes** y **MCP Server**.

Los agentes conversan con el usuario. El MCP Server provee el conocimiento sobre Hermes y ejecuta las consultas. Los agentes accederán a los datos únicamente a través del MCP Server.

> **Principio:** el código es el motor, MongoDB los datos y el JSON son las reglas de negocio y las relaciones entre las entidades.

## 2. Agentes

Los agentes serán dos: uno de perfil de **negocio** y otro de perfil **negocio + técnico**.

|  | Negocio | Negocio + técnico |
|---|---|---|
| Lenguaje | Términos de negocio. No muestra propiedades, colecciones ni consultas. | Términos de negocio y, además, los nombres reales de propiedades y colecciones. |
| Consulta usada | No la muestra. | La muestra si se la piden. |
| Pregunta ambigua | Pregunta antes de consultar. | Trae todos los candidatos para que el usuario elija. |

Los dos agentes:

- Armarán y ejecutarán las consultas ellos mismos, sin subagentes.
- Usarán como única fuente lo que devuelva el MCP Server en la conversación en curso, sin memoria ni otras conversaciones. No inventarán propiedades.
- Todo dato que muestren (cifras, nombres, códigos, fechas) saldrá de un resultado del MCP Server en la conversación en curso. No estimarán, completarán ni deducirán datos que no estén en un resultado; si falta un dato, lo dirán.
- Toda tabla o listado que muestren al usuario saldrá de una consulta de `reporting`.
- Ante una pregunta que admita más de una lectura, no elegirán una por su cuenta.
- Ante un resultado vacío, expondrán las causas posibles y dejarán que el usuario elija cómo seguir.
- Mostrarán su razonamiento, para que el resultado se pueda verificar. Responderán de forma concisa salvo que se pida más detalle.
- Citarán el origen de los datos tal como lo declare el MCP Server, y no presentarán como regla de Hermes lo que es una observación sobre los datos.

Cada perfil se definirá en un system prompt. Cambiar el comportamiento de un agente será cambiar su prompt, no el MCP Server. Los prompts definirán solo comportamiento: no repetirán conocimiento del modelo (propiedades, colecciones, relaciones, catálogos), que el agente obtendrá siempre del MCP Server.

## 3. MCP Server

El MCP Server no contendrá reglas de negocio. Las mismas le serán provistas en un fichero JSON (ver sección 4).

### Herramientas

| Herramienta | Función |
|---|---|
| `diccionario_de_campos` | Propiedades de cada entidad y su significado. Responde desde el JSON. |
| `relacion_entre_entidades` | Relación entre entidades y origen de los datos. Responde desde el JSON. |
| `reporting` | Ejecución de la consulta armada por el agente, tal cual la recibe. |

### Consultas

El MCP Server validará cada consulta antes de ejecutarla: solo aceptará etapas de consulta de una lista cerrada, solo sobre las colecciones de las entidades del alcance y de las tablas auxiliares habilitadas, y con un tiempo máximo por consulta. Estas reglas vivirán en el MCP Server, no en los prompts, para que ningún agente pueda saltearlas.

### Resultados

- Conteo, o listado en pantalla limitado a un número de filas, con aviso cuando haya más. Ese número lo fija la configuración del MCP Server; el agente no lo puede cambiar ni juntar más filas partiendo la consulta.
- Listado completo en un archivo JSON cuando el usuario lo pida, guardado en un almacenamiento compartido con el usuario (por ejemplo, una carpeta de SharePoint) al que solo tendrán acceso el usuario y el MCP Server.
- Sin resultados no se generará archivo, y se informará explícitamente.

### Seguridad frente a la base

El MCP Server solo leerá de la base. Para eso:

- Aplicará la lista cerrada de etapas y el control de colecciones en todos los niveles de la consulta, incluidas las consultas anidadas dentro de otra etapa.
- Rechazará en cualquier posición los operadores que escriben en la base, ejecutan código en el servidor o exponen su estado interno.
- Rechazará las consultas que repitan una clave en un mismo objeto, para que lo que se valida sea exactamente lo que se ejecuta.
- Aplicará el tiempo máximo a toda consulta y se lo pasará a la base para que ella misma la corte.
- No mostrará las credenciales de la base en registros ni en mensajes de error.

> **Recomendación:** que el MCP Server se conecte con un usuario de base exclusivo, con permiso solo de lectura y solo sobre las colecciones del alcance y las tablas auxiliares habilitadas. Así, la protección no dependerá únicamente del código del MCP Server.

## 4. Fichero JSON

El fichero JSON tendrá:

- **Una descripción:** básicamente, el origen de los datos.
- **Conocimiento técnico:** propiedades, colecciones y relación entre las mismas.
- **Conocimiento de negocio:** significado de cada propiedad, etc.

Además podrá tener metadata de mantenimiento (de dónde sale cada dato: código, Excel, verificaciones). El MCP Server no la entregará a los agentes.

> El JSON nunca contendrá contenido de la base: ni conteos, ni valores observados, ni copias de catálogos o tablas auxiliares, ni ejemplos con datos reales (nombres, códigos, identificadores). Describirá la estructura, los mecanismos de relación y el significado de las propiedades; todo dato que esté en una colección se obtendrá consultándola con `reporting`.

La relación entre los nombres de las entidades y de las colecciones, así como las tablas auxiliares habilitadas, se hará hardcoded en el MCP Server, por un problema de seguridad: es lo que limita qué colecciones se pueden consultar.

Para generar o actualizar este JSON se deberá consultar:

- el código: el repositorio de Hermes y los adapters;
- el Excel que define los términos de negocio de las propiedades.

Podrá haber un JSON por entorno o por versión de Hermes. Cada instalación indicará cuál usa. Cambiar el JSON no requerirá recompilar el MCP Server.

## 5. Alta de una entidad y/o tablas auxiliares

Una tabla auxiliar es una colección que no es una entidad, por ejemplo un catálogo de códigos, y que se consulta solo para completar los datos de una entidad.

Sumar una entidad y/o una tabla auxiliar requerirá dos cambios:

- agregar su colección en el MCP Server (hardcoded), lo que exige recompilar. Una tabla auxiliar se habilitará solo para cruzarla desde una entidad, no como punto de partida de una consulta;
- generar o actualizar el JSON con sus propiedades, su significado y sus relaciones.

Las relaciones nuevas se confirmarán con el usuario contra datos reales antes de darlas por buenas. Los agentes no cambiarán: lo nuevo les llegará a través del MCP Server.

## 6. Configuración

- La conexión a la base y los límites de las consultas se configurarán junto al MCP Server.
- Qué JSON usa cada instalación se configurará en el cliente de esa instalación.
- Las credenciales y los JSON reales no se versionarán; sí sus plantillas de ejemplo.

## 7. Cambios

Este documento es la constitución de Hermes AI. No podrá ser cambiado a menos que se decida agregar o quitar funcionalidades.

Toda actualización de código deberá ser contrastada con este documento.

Todo cambio que contradiga este documento se notificará a quien solicitó el cambio, indicándole que debe modificar este documento.
