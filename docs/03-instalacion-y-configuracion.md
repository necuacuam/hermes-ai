# Instalación y configuración

Cómo compilar, configurar y conectar el MCP Server a Claude Desktop. Los términos están definidos en el [glosario](06-glosario.md).

## Requisitos

- Go 1.25.5 o superior (lo exige la librería `mark3labs/mcp-go`; ver `go.mod`).
- Acceso a la base MongoDB de Hermes. Se recomienda un usuario de solo lectura, limitado a las colecciones del registro (ver la [constitución](01-constitucion.md), sección 3).
- Claude Desktop, para usar los agentes.

## Compilar

```bash
go build -o hermes-mcp.exe ./cmd/hermes-mcp
```

En Linux o Mac, `-o hermes-mcp`. Antes de compilar, cerrar Claude Desktop por completo: mientras está abierto, el ejecutable queda bloqueado.

Para correr los tests:

```bash
go test ./...
```

## Configuración

La configuración vive en dos lugares, cada uno con un rol fijo:

| Lugar | Qué contiene | Se versiona |
|---|---|---|
| `.env`, al lado del ejecutable | Conexión a la base, carpeta de salidas, límites. Lo lee el propio ejecutable, se arranque como se arranque. | No. Se copia de `.env.example`. |
| Bloque `env` de `claude_desktop_config.json` | `HERMES_MCP_RELACIONES_PATH`: qué fichero de conocimiento usa esa instalación. | No. |

Una variable definida en el entorno real tiene prioridad sobre el `.env`.

### Variables

| Variable | Para qué | Dónde | Por defecto |
|---|---|---|---|
| `HERMES_MCP_MONGO_URI` | Conexión a MongoDB. | `.env` | Ninguno: **obligatoria**. Sin ella el servidor no arranca. |
| `HERMES_MCP_MONGO_DB` | Base a usar. | `.env` | `hermes-local` |
| `HERMES_MCP_RELACIONES_PATH` | Ruta al fichero de conocimiento. Puede ser relativa (se busca primero en el directorio de trabajo y después al lado del ejecutable) o absoluta. | `claude_desktop_config.json` | Ninguno: **obligatoria**. |
| `HERMES_MCP_SALIDAS_DIR` | Carpeta donde `reporting` guarda los archivos. Conviene una ruta absoluta. | `.env` | `./salidas` |
| `HERMES_MCP_LIMITE_PANTALLA` | Máximo de filas que `reporting` devuelve al agente. El agente no lo puede cambiar. | `.env` | `10` |
| `HERMES_MCP_MONGO_TIMEOUT` | Tiempo máximo por consulta (formato `30s`, `2m`). MongoDB también lo recibe (`maxTimeMS`). | `.env` | `30s` |
| `HERMES_MCP_ZONA_HORARIA` | Zona horaria de la carpeta del día donde se guardan los archivos. | `.env` | `Europe/Madrid` |

La carpeta de salidas puede contener datos reales: no se versiona.

### Fichero de conocimiento

1. Copiar `relaciones/relaciones.example.json` a otro nombre dentro de `relaciones/` (por ejemplo `relaciones/relaciones.json`). Los ficheros que no terminan en `.example.json` no se versionan.
2. Completar `origenDatos` con la base o el entorno conectado. **Actualizarlo cada vez que cambie `HERMES_MCP_MONGO_URI`:** es lo que el agente cita como origen de los datos. Si queda vacío, el agente no nombra ningún entorno.

Puede haber varios ficheros (uno por entorno o versión); `HERMES_MCP_RELACIONES_PATH` decide cuál se carga. El formato está en [Fichero de conocimiento](04-fichero-de-conocimiento.md).

## Conectar Claude Desktop

1. Compilar el ejecutable y crear el `.env` (ver arriba).
2. En Claude Desktop, *Settings → Developer → Edit Config*, agregar:

   ```json
   {
     "mcpServers": {
       "hermes-mcp": {
         "command": "C:\\ruta\\completa\\a\\mcp-server\\hermes-mcp.exe",
         "env": {
           "HERMES_MCP_RELACIONES_PATH": "relaciones/relaciones.json"
         }
       }
     }
   }
   ```

   La ruta de `command` tiene que ser absoluta.
3. Reiniciar Claude Desktop por completo (cerrar el proceso, no solo la ventana). Lo mismo después de recompilar o de cambiar el `.env`, el fichero de conocimiento o esta configuración: nada se relee en caliente.
4. Comprobar la conexión: en un chat nuevo, pedir el diccionario de campos de una entidad.
5. Crear un proyecto por agente y cargar su system prompt (carpeta `prompts/`) como instrucciones del proyecto:
   - `agente-hermes-prompt.md`: perfil de **negocio**. No muestra campos, colecciones ni consultas, y pregunta ante cualquier ambigüedad.
   - `agente-hermes-tecnico-prompt.md`: perfil **negocio + técnico**. Muestra los nombres reales y la consulta si se la piden, y ante una ambigüedad trae todos los candidatos.

Los dos usan las mismas herramientas y las mismas restricciones: cambia solo el estilo de interacción.

## Linux y Open WebUI

`scripts/build-linux.ps1` genera la carpeta `dist-linux/` (no versionada) con el ejecutable de Linux, un `.env` y los ficheros de conocimiento, para servirlo a Open WebUI a través de `mcpo`. Si se usa, sus ficheros de conocimiento se mantienen junto con los de `relaciones/`.

## Base local para desarrollo

Para desarrollar sin acceso a la base real, se puede levantar un MongoDB vacío y cargarlo con `seed/seed.js` (datos de prueba). **El script borra las colecciones antes de cargarlas: nunca ejecutarlo contra una base real.**
