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
- Listado completo en un archivo JSON cuando el usuario lo pida y la instalación lo habilite, guardado en un almacenamiento compartido con el usuario (por ejemplo, una carpeta de SharePoint) al que solo tendrán acceso el usuario y el MCP Server.
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

## 7. Líneas futuras

Decididas, pero no implementadas. Mientras no se implementen, no rigen.

### Enseñar al agente

Ciertos usuarios, con perfil **educador**, podrán enseñarle al agente conocimiento de negocio que no está en el JSON (por ejemplo, qué significa un código).

- **Alcance:** solo información de negocio de propiedades de documentos de colecciones **existentes**: el significado de una propiedad, o una propiedad que existe en los documentos y no está documentada. No se podrán agregar colecciones, entidades, tablas auxiliares ni relaciones, ni cambiar la estructura. Eso sigue el camino de la sección 5.
- **Proponer no es publicar:** lo enseñado quedará como una propuesta pendiente. Un curador la revisará y la aprobará o rechazará; solo lo aprobado pasará al JSON.
- Las propuestas cumplirán las mismas reglas del JSON (sección 4): nunca contenido de la base ni instrucciones para el agente.
- El perfil educador lo controlará el MCP Server según la identidad del usuario, no el prompt. Requiere autenticación de usuarios.
- **Almacenamiento:** las propuestas y las versiones del JSON se guardarán en Mongo, en una base propia de Hermes AI, nunca en la base consultada. Esa base tendrá un usuario de escritura propio, y la conexión a la base consultada seguirá siendo de solo lectura.
- **Versiones:** cada aprobación generará una versión nueva del JSON, con su autor, su fecha y las propuestas que incluye. El MCP Server cargará la versión activa, y volver atrás será activar una versión anterior.

### Consultas mixtas y área de trabajo

Hermes AI podrá responder preguntas que combinen datos de bases de datos y de APIs, y cruzarlos entre sí.

**Conector de API**
- Además de consultar bases de datos, el MCP Server podrá llamar a APIs, creadas para Hermes AI, que escriben sus resultados en una colección del área de trabajo.
- Las APIs que el agente puede llamar se declararán en el registro del MCP Server (código o configuración), igual que las colecciones. El agente podrá hacer todas las llamadas que necesite, y el MCP Server validará los parámetros de cada una antes de hacerla.
- El fichero JSON describirá el significado de cada API y de las columnas de la colección que crea, y con qué propiedades de las entidades se puede cruzar.

**Área de trabajo**
- El área de trabajo vivirá en un servidor de Mongo propio, nunca en las bases consultadas, que seguirán siendo de solo lectura.
- El agente pedirá a cada API los datos que necesita y le indicará el nombre de la colección del área de trabajo donde dejarlos: un UUID, que no se repite. La API creará esa colección con el resultado.
- Si necesita datos de la base de un departamento, una función ejecutará el pipeline que haga falta y volcará el resultado en una colección del área de trabajo, también con un UUID.
- Cuando todas hayan respondido, el agente ejecutará el pipeline final que necesite sobre esas colecciones, con las mismas reglas de validación que las consultas a las bases, y su resultado tendrá el mismo límite de pantalla y el mismo archivo.
- Las colecciones del área de trabajo se borrarán solas pasado un tiempo.

**Prompts:** indicarán solo el comportamiento, por ejemplo cuándo usar el área de trabajo en lugar de mostrar un resultado directamente. Qué APIs existen y qué significan saldrá siempre del MCP Server.

## 8. Cambios

Este documento es la constitución de Hermes AI. No podrá ser cambiado a menos que se decida agregar o quitar funcionalidades.

Toda actualización de código deberá ser contrastada con este documento.

Todo cambio que contradiga este documento se notificará a quien solicitó el cambio, indicándole que debe modificar este documento.
