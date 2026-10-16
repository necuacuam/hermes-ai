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
| `HERMES_MCP_TRANSPORTE` | `stdio` (Claude Desktop) o `http` (clientes remotos, como Open WebUI). | `.env` o entorno | `stdio` |
| `HERMES_MCP_HTTP_DIRECCION` | Dirección donde escucha el transporte HTTP. El servidor atiende en la ruta `/mcp`. | `.env` o entorno | `:8080` |

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

## Lab con Open WebUI

Un entorno local con Docker para probar Hermes AI con Open WebUI: `hermes-mcp` por HTTP en un contenedor, Open WebUI en otro, y la misma base MongoDB de siempre (no se levanta otra). Los ficheros están en `lab/`.

> **El transporte HTTP todavía no tiene autenticación.** El lab publica los dos servicios solo en el propio equipo (`127.0.0.1`). Nunca publicarlos hacia la red.

1. Crear `lab/hermes-mcp.env` a partir de `lab/hermes-mcp.env.example`: la misma conexión del `.env`, pero con `host.docker.internal` en lugar de `localhost`, que es como se alcanza la base del equipo desde un contenedor. No se versiona.
2. Desde `lab/`, levantar todo (la primera vez descarga la imagen de Open WebUI, de varios GB):

   ```bash
   docker compose up -d --build
   ```

3. Abrir `http://localhost:3000` y crear el usuario administrador (el primero que se registra).
4. En la administración de Open WebUI:
   - **Conexión al modelo:** la API del modelo que se use, con su clave.
   - **Servidor de herramientas:** en el **panel de administración** (*Admin Panel → Settings → External Tools*), no en los ajustes del usuario ni en *Workspace → Tools*. Tipo **MCP (Streamable HTTP)**, no OpenAPI; URL `http://hermes-mcp:8080/mcp`. Como el servidor todavía no revisa la autenticación, elegir *None* o Bearer con **cualquier valor no vacío**: con la clave vacía, Open WebUI envía una cabecera `Authorization` inválida y la conexión falla.
   - **Modelo con el prompt:** en *Workspace → Models*, un modelo propio por perfil: modelo base (uno grande, de uso diario), system prompt `prompts/agente-hermes-tecnico-prompt.md` (o el de negocio) y el servidor de herramientas habilitado. Sin el prompt, las respuestas fallan: el prompt es parte del despliegue. En el chat se elige ese modelo propio, no el modelo base.
   - **Llamada a funciones nativa:** en los parámetros avanzados del modelo, *Function Calling* en **Native**. En el modo por defecto, Open WebUI llama a una sola herramienta y le pasa el resultado como contexto: el agente no puede encadenar el diccionario, las relaciones y `reporting`, y termina respondiendo sin datos.
   - **Perfiles por grupo:** cada modelo propio, privado y con acceso solo para el grupo de usuarios de su perfil.

   Los nombres exactos de los menús dependen de la versión de Open WebUI.

### Login con Microsoft Entra ID

1. En Azure, *Microsoft Entra ID → App registrations → New registration*:
   - Tipo de cuenta: solo cuentas de este directorio.
   - URI de redirección, plataforma **Web**: `http://localhost:3000/oauth/microsoft/callback`.
2. *Token configuration → Add optional claim → ID → `email`*, aceptando el permiso `email` de Microsoft Graph que propone. Open WebUI identifica a cada usuario por su email.
3. *Certificates & secrets → New client secret*: copiar el **valor** (se muestra una sola vez), no el ID del secreto.
4. Crear `lab/open-webui.env` a partir de `lab/open-webui.env.example` (no se versiona). Los identificadores salen del *Overview* del registro, y es fácil confundirlos:

   | Variable | Campo del *Overview* |
   |---|---|
   | `MICROSOFT_CLIENT_ID` | Application (client) ID |
   | `MICROSOFT_CLIENT_TENANT_ID` y el `<tenant>` de `OPENID_PROVIDER_URL` | **Directory (tenant) ID**, no el *Object ID* ni el ID de la suscripción |

   Si el tenant está mal, el botón de login da un error 500 y el registro de Open WebUI muestra que Microsoft respondió `AADSTS90002: Tenant not found`.
5. Recrear Open WebUI (`docker compose up -d open-webui`) y esperar a que arranque: mientras arranca puede responder un 500 pasajero.
6. Entrar con *Continue with Microsoft*. El primer ingreso de cada usuario queda pendiente: lo aprueba un administrador (el login local sigue activo para eso).

**Perfiles:** en Open WebUI los roles son fijos (administrador, usuario, pendiente); los perfiles se arman con **grupos** (*Admin Panel → Users → Groups*). Cada grupo necesita acceso en **dos lugares**: al modelo de su perfil (*Workspace → Models*) y a la conexión del servidor de herramientas (*Admin Panel → Settings → External Tools*). Con acceso al modelo pero no a la conexión, el agente ve las herramientas pero sus llamadas no devuelven nada.

El fichero de conocimiento se monta desde `relaciones/` (solo lectura): para cambiarlo, editar el archivo y reiniciar el contenedor (`docker compose restart hermes-mcp`). Los archivos de `reporting` quedan en `lab/salidas/`, que no se versiona.

Para probar el servidor sin Open WebUI, el contenedor también responde en `http://127.0.0.1:8081/mcp`.

## Linux y Open WebUI

`scripts/build-linux.ps1` genera la carpeta `dist-linux/` (no versionada) con el ejecutable de Linux, un `.env` y los ficheros de conocimiento, para servirlo a Open WebUI a través de `mcpo`. Si se usa, sus ficheros de conocimiento se mantienen junto con los de `relaciones/`.

## Base local para desarrollo

Para desarrollar sin acceso a la base real, se puede levantar un MongoDB vacío y cargarlo con `seed/seed.js` (datos de prueba). **El script borra las colecciones antes de cargarlas: nunca ejecutarlo contra una base real.**
