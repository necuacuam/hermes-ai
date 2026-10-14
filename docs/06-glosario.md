# Glosario

Términos tal como se usan en toda la documentación de Hermes AI.

| Término | Significado |
|---|---|
| **Hermes** | El sistema cuyos datos se consultan (base MongoDB). |
| **Hermes AI** | El asistente: agentes más MCP Server. |
| **Agente** | Claude con un system prompt. Conversa con el usuario y consulta los datos solo a través del MCP Server. Hay dos: negocio y negocio + técnico. |
| **System prompt** | Las instrucciones de comportamiento de un agente. No contiene conocimiento del modelo de datos. |
| **MCP Server** | El servidor (`hermes-mcp`) que expone las herramientas al agente y aplica todas las restricciones. |
| **Herramienta** | Cada función que el MCP Server ofrece al agente: `diccionario_de_campos`, `relacion_entre_entidades` y `reporting`. |
| **Entidad** | Un tipo de documento de negocio consultable (holding, grupo, cliente, centro, punto, dirección, salesPerson). Tiene una colección propia. |
| **Alias** | El nombre de negocio de una entidad (`cliente`). Se usa en `coleccionInicial`. |
| **Colección** | El nombre real en MongoDB (`serviceEntity`). Se usa en el `from` de un `$lookup`. |
| **Tabla auxiliar** | Una colección que no es una entidad (por ejemplo, un catálogo de códigos). Solo se puede usar como destino de un `$lookup`, nunca como punto de partida. Hoy: `operativeLevel`. |
| **Registro** | La lista de entidades, colecciones y tablas auxiliares fijada en el código (`internal/domain`). Es lo que limita qué se puede consultar; cambiarlo exige recompilar. |
| **Fichero de conocimiento** | El JSON (`relaciones/*.json`) con el significado de los campos, las relaciones y la estructura de los documentos. Cambiarlo no exige recompilar. |
| **Campo** | Una propiedad de los documentos de una entidad, documentada en `campos` del fichero de conocimiento. |
| **Relación** | Un vínculo entre entidades, o entre una entidad y una tabla auxiliar, documentado en `relaciones`. Tipos: `padre`, `anidamiento`, `referencia`, `catalogo`. |
| **Estado de una relación** | `confirmado`, `sin confirmar` o `informativo` (existe, pero no sirve para recorrer). |
| **Fuente** | Metadata interna de mantenimiento: de dónde salió un dato (código, documentación, consulta). El MCP Server no la entrega al agente. |
| **Origen de los datos** | `origenDatos`: de qué base o entorno vienen los datos conectados. Lo completa cada instalación. |
| **Pipeline** | La consulta de agregación de MongoDB que arma el agente y ejecuta `reporting`. |
| **Validación** | El control que hace el MCP Server sobre cada pipeline antes de ejecutarlo: etapas permitidas, colecciones del registro, operadores prohibidos, claves duplicadas. |
| **Límite de pantalla** | El máximo de filas que `reporting` devuelve al agente. Lo fija la configuración del servidor. |
| **Constitución** | `docs/01-constitucion.md`: las directivas de diseño contra las que se contrasta todo cambio. |
| **Educador** | (Línea futura) Perfil de usuario que puede proponer conocimiento de negocio. |
| **Curador** | (Línea futura) Persona que aprueba o rechaza lo que proponen los educadores. |
