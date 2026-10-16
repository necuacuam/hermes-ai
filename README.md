# Hermes AI — MCP Server

Hermes AI es un asistente que responde en lenguaje natural preguntas sobre los datos de Hermes: holding, grupo, cliente, centro, punto, dirección y vendedores (`salesPerson`). Este repositorio contiene el **MCP Server** (`hermes-mcp`) y los **system prompts** de sus dos agentes.

> El código es el motor, MongoDB los datos y el JSON son las reglas de negocio y las relaciones entre las entidades.

- **Agentes:** Claude con un system prompt (negocio, o negocio + técnico). Consultan los datos solo a través del MCP Server.
- **MCP Server:** expone tres herramientas (`diccionario_de_campos`, `relacion_entre_entidades` y `reporting`) y aplica todas las restricciones. Solo lee de la base.
- **Fichero de conocimiento:** el JSON con el significado de los campos y las relaciones entre entidades. Se cambia sin recompilar.

## Documentación

| Documento | Para qué |
|---|---|
| [Constitución](docs/01-constitucion.md) | Las directivas de diseño. Todo cambio se contrasta con ellas. |
| [Arquitectura](docs/02-arquitectura.md) | Cómo está armado y qué patrones implementa. |
| [Instalación y configuración](docs/03-instalacion-y-configuracion.md) | Compilar, configurar y conectar Claude Desktop. |
| [Fichero de conocimiento](docs/04-fichero-de-conocimiento.md) | Formato y reglas del JSON. |
| [Alta de entidades](docs/05-alta-de-entidades.md) | Cómo sumar una entidad o una tabla auxiliar. |
| [Glosario](docs/06-glosario.md) | Los términos que usa toda la documentación. |
| [Historial](docs/historial.md) | Por qué es como es: decisiones con fecha. |

## Inicio rápido

```bash
go test ./...
go build -o hermes-mcp.exe ./cmd/hermes-mcp
```

Después, copiar `.env.example` a `.env`, crear el fichero de conocimiento y registrar el ejecutable en Claude Desktop, como explica [Instalación y configuración](docs/03-instalacion-y-configuracion.md).

## Estructura del repositorio

```
cmd/hermes-mcp/              punto de entrada, configuración y servidor MCP (librería mark3labs/mcp-go, stdio)
internal/domain/             registro: entidades, colecciones y tablas auxiliares
internal/dictionary/         carga y validación del fichero de conocimiento
internal/store/              validación de pipelines
internal/store/mongostore/   ejecución de pipelines contra MongoDB
internal/reporting/          salida de resultados (pantalla y archivo)
internal/tools/              las tres herramientas MCP
prompts/                     system prompts de los agentes
relaciones/                  ficheros de conocimiento (solo *.example.json se versiona)
scripts/                     arranque en Windows y compilación para Linux
seed/                        datos de prueba para una base de desarrollo vacía
lab/                         lab local con Docker: hermes-mcp por HTTP + Open WebUI
docs/                        documentación
Dockerfile                   imagen de hermes-mcp con transporte HTTP
```
