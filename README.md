# hermes-mcp

Servidor MCP (stdio, JSON-RPC) para consultar las entidades tipo cuenta de
Hermes (Prosegur): holding, grupo, cliente, centro, punto y dirección. A
esas 6 se suma `operativeLevel`, una colección auxiliar/catálogo (clase y
subclase de punto) -- NO es una entidad (no tiene `ucode`/`parentUCode`,
no puede ser punto de partida de un pipeline), solo está habilitada como
destino de `$lookup` de solo lectura desde las 6 entidades (ver sección
"Decisión de negocio" más abajo). Pensado para correr en local y
conectarse desde Claude Desktop. Contexto de producto en
`Hermes AI/propuesta-mcp-hermes-jefe.md` y
`Hermes AI/grafo-entidades-hermes.md` (fuera de este repo).

El diccionario de campos (`internal/dictionary`) y el grafo de relaciones
entre entidades se armaron estudiando dos fuentes en paralelo -- el Excel
de referencia de negocio `EntidadesOTC.xlsx` y el código fuente real de
Hermes (`support-otc-hermes-hms-master`) -- porque ninguna de las dos
alcanza sola: hay campos reales que el Excel no documenta (los de equipo
de `punto`, o `operativeLevel` entero) y discrepancias confirmadas entre
lo que dice el Excel y lo que hace el código (`longName`/`shortName`
declarados "aplica a todos" pero ausentes en `cliente`/`centro`). Cuando
las dos fuentes no alcanzaban, se confirmó contra una muestra real de
Mongo (UAT restaurada). Cada campo documentado en `dictionary.go` trae su
fuente exacta (Excel, código, o ambos) en el campo `fuente` -- metadata de
mantenimiento interno, nunca se le muestra al usuario final.

La base Mongo a la que apunta es de solo lectura: ningún pipeline puede
escribir (ver `internal/store/pipeline_guard.go`).

## Tools

- **`diccionario_de_campos`**: traduce nombre de negocio ↔ campo técnico
  Mongo, por tipo de entidad. Estática (no toca Mongo) -- el contenido
  completo (todos los campos que persisten de verdad en Mongo por tipo, no
  un subconjunto) se carga desde un fichero externo (`relaciones/`, ver más
  abajo, campo `campos`) en vez de vivir hardcodeado en `internal/dictionary`.
- **`relacion_entre_entidades`**: jerarquía de tipos, prefijos de ucode,
  nombre real de cada colección Mongo y el mecanismo de `parentUCode`.
  También estática (no toca Mongo) -- jerarquía/prefijos/colecciones siguen
  fijos en código (`internal/domain`), pero el mecanismo de relación, las
  notas de negocio y `origenDatos` se cargan desde el mismo fichero externo
  (`relaciones/`, ver más abajo) en vez de vivir hardcodeados en Go.
- **`seek`**: busca entidades reales por nombre aproximado sin asumir tipo.
  Diagnóstico posterior a un resultado inesperado, no un paso obligatorio.
- **`reporting`**: ejecuta un pipeline de agregación ya armado (modo
  `conteo` o `listado`, pantalla y/o archivo JSON local).

Todo campo de negocio vive anidado bajo `data` en el documento Mongo;
`ucode`/`parentUCode` son la excepción, a nivel raíz. `diccionario_de_campos`
ya devuelve cada nombre con el prefijo `data.` incluido.

## Prompts

Las 4 tools de arriba son la capa mecánica -- lo que convierte eso en un
asistente usable es el system prompt que se carga en Claude Desktop. Hay
dos, en `prompts/`, para dos audiencias distintas con objetivos distintos:

- **`agente-hermes-prompt.md`** (negocio): para gente que no conoce Mongo
  ni nombres técnicos de campo. Objetivo: traducir todo a lenguaje de
  negocio, ocultar el pipeline y los nombres de las tools, y pausar a
  preguntar ante cualquier ambigüedad (nombre de entidad repetido, alcance
  geográfico dudoso) antes de ejecutar nada -- prioriza no llevar al
  usuario por un camino equivocado por sobre resolver rápido.
- **`agente-hermes-tecnico-prompt.md`** (equipo de Hermes): para gente que
  sí conoce Mongo y aggregation pipelines. Objetivo: sin la capa de
  traducción, mostrando el pipeline armado y los nombres reales de
  campo/colección, y resolviendo la ambigüedad de nombre ampliando el
  filtro con `$regex` en vez de pausar a preguntar -- prioriza velocidad
  para alguien que ya sabe leer el resultado crudo.

Los dos comparten las mismas 4 tools y las mismas barreras de seguridad
(solo lectura, alcance de las 6 entidades + `operativeLevel`) -- esas
barreras están reforzadas en el servidor (`internal/store/pipeline_guard.go`),
no en el texto del prompt, así que ningún prompt puede aflojarlas aunque
lo pida el usuario. Lo que cambia entre los dos es exclusivamente el
estilo de interacción: qué se explica, qué se oculta, y cuándo se pausa a
preguntar en vez de decidir solo. Cómo cargar cada uno en Claude Desktop
está en el paso 6 de "Configurar Claude Desktop" más abajo.

## Variables de entorno

**Dos lugares, cada uno con un rol fijo -- no son intercambiables:**

- **`.env`**, al lado de `hermes-mcp.exe`: la config de conexión, compartida
  sea cual sea la forma de arrancar el servidor (Claude Desktop, `go run`,
  los scripts de `scripts\`) -- Mongo, carpeta de salidas, topes de
  recursos. Copiar `.env.example` a `.env` y completar ahí (lo lee el
  binario mismo, ver `loadDotEnv` en `main.go`). `HERMES_MCP_MONGO_URI` no
  tiene ningún default embebido en el código: si no está en `.env` ni en
  el entorno real, el servidor no arranca.
- **Bloque `env` de `claude_desktop_config.json`**: el único lugar de
  `HERMES_MCP_RELACIONES_PATH`. No es un override puntual de algo que
  también podría ir en `.env` -- es la variable que dice qué fichero de
  relaciones usa ESTA instalación de Claude Desktop, así que vive en la
  config de esa instalación, no en un `.env` que podría terminar
  compartido entre varias. Ver "Configurar Claude Desktop" más abajo.

| Variable | Para qué | Dónde se configura | Default |
|---|---|---|---|
| `HERMES_MCP_MONGO_URI` | Conexión a Mongo. Vacío explícito fuerza `mockstore` (sin pipelines). | `.env` | **ninguno -- obligatoria** (sin ella, o sin este archivo, el servidor no arranca) |
| `HERMES_MCP_MONGO_DB` | Base a usar. | `.env` | `hermes-local` |
| `HERMES_MCP_RELACIONES_PATH` | Ruta al fichero de relaciones a cargar (dentro de `relaciones/`) -- ahí vive tanto la estructura de `relacion_entre_entidades` como `origenDatos` (de qué base/entorno viene lo conectado ahora). Ver sección más abajo. | **bloque `env` de `claude_desktop_config.json`** (no `.env`) | **ninguno -- obligatoria** (sin ella, o sin el fichero, el servidor no arranca) |
| `HERMES_MCP_SALIDAS_DIR` | Carpeta de salida de `reporting` (`guardarEnArchivo=true`). | `.env` | `./salidas` |
| `HERMES_MCP_LIMITE_PANTALLA_DEFAULT` | Filas máximas en pantalla si no se especifica. | `.env` | `10` |
| `HERMES_MCP_MONGO_TIMEOUT` | Timeout por pipeline. | `.env` | `30s` |

## Cómo correrlo

```bash
go mod tidy
go build -o hermes-mcp ./cmd/hermes-mcp
./hermes-mcp
```

En Windows, `scripts\run-hermes-mcp.cmd` hace lo mismo cargando `.env`.

## Configurar Claude Desktop

Claude Desktop no corre `go run` -- necesita el **binario ya compilado**
registrado como servidor MCP local. Son dos archivos de configuración,
cada uno con un rol distinto y sin superposición entre sí:

- **`.env`** (al lado de `hermes-mcp.exe`, nunca se versiona): la config de
  conexión -- Mongo, carpeta de salidas, topes de recursos.
- **`relaciones/relaciones.json`** (carpeta al lado de `hermes-mcp.exe`;
  los `.json` reales nunca se versionan -- copiar desde
  `relaciones/relaciones.example.json` y completar `origenDatos`): la
  estructura de `relacion_entre_entidades` más de qué base/entorno viene
  lo conectado, Y (desde 2026-09-20) el contenido completo de
  `diccionario_de_campos` (clave `campos`, un array de `{campo,
  descripcion, fuente}` por tipo de entidad). Puede haber varios ficheros
  en esa carpeta (uno por entorno/release) -- `HERMES_MCP_RELACIONES_PATH`
  dice cuál usar.
- **`claude_desktop_config.json`**: le dice a Claude Desktop qué binario
  levantar, Y trae el bloque `env` con `HERMES_MCP_RELACIONES_PATH` --
  esta variable NO va en `.env`. Es la config de ESTA instalación de
  Claude Desktop, así que su lugar natural es la config de esa
  instalación, no un `.env` compartido. Ver paso 3 más abajo.

Pasos:

1. Compilar: `go build -o hermes-mcp.exe ./cmd/hermes-mcp` (Windows) o
   `go build -o hermes-mcp ./cmd/hermes-mcp` (Linux/Mac).
2. Copiar `.env.example` a `.env`, al lado del binario, y completar. Como
   mínimo hace falta `HERMES_MCP_MONGO_URI` (**obligatoria, sin default
   embebido en el código -- sin ella, o sin este archivo, el servidor no
   arranca**). Ejemplo real de este proyecto (Mongo local de
   `docker-compose.yml`):

   ```
   HERMES_MCP_MONGO_URI=mongodb://hermes:cambiame-antes-de-exponer@localhost:27017/?authSource=admin
   HERMES_MCP_MONGO_DB=hermes-local
   HERMES_MCP_SALIDAS_DIR=C:\ruta\completa\a\mcp-server\generatedfiles
   ```

   `HERMES_MCP_RELACIONES_PATH` **no va en `.env`** -- se configura en el
   paso 3, dentro de `claude_desktop_config.json`. `HERMES_MCP_SALIDAS_DIR`
   puede ser relativa (`./generatedfiles`, resuelta contra el directorio de
   trabajo con el que arranque el proceso) o absoluta -- absoluta es más
   segura si no se sabe con qué cwd va a lanzar Claude Desktop al binario.
   Además, copiar `relaciones/relaciones.example.json` a un nombre dentro
   de esa misma carpeta (ej. `relaciones/relaciones.json`) y completar
   `origenDatos` -- **actualizar ese campo junto con `HERMES_MCP_MONGO_URI`
   de arriba, en el mismo momento** (ver sección más abajo de por qué).
3. Editar `claude_desktop_config.json` (menú Claude Desktop → Settings →
   Developer → Edit Config) y agregar una entrada bajo `mcpServers` con
   `command` y el bloque `env` con `HERMES_MCP_RELACIONES_PATH`:

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

   Ruta de `command` absoluta, no relativa. El bloque `env` con
   `HERMES_MCP_RELACIONES_PATH` **es obligatorio acá, no un override
   puntual** -- es la variable que dice qué fichero de relaciones usa esta
   instalación de Claude Desktop en particular; sin ella el servidor no
   arranca, igual que sin `HERMES_MCP_MONGO_URI` en `.env`. Puede ser
   relativa (se busca primero contra el directorio de trabajo, después al
   lado del ejecutable) o absoluta.
4. Reiniciar Claude Desktop por completo (cerrar el proceso, no solo la
   ventana) para que levante el servidor. Cualquier recompilación
   posterior del binario, o edición de `.env` o de `claude_desktop_config.json`,
   también requiere este mismo reinicio completo -- Claude Desktop no
   re-lee ninguno de los tres en caliente.
5. Confirmar que conectó: en un chat nuevo con el MCP habilitado, pedir que
   liste los campos de una entidad (ej. "mostrame el diccionario de campos
   de cliente") y verificar que responde con datos, no con un error de
   conexión.
6. Cargar el prompt que corresponda (ver sección "Prompts" más arriba
   para cuál usar según la audiencia) como instrucciones personalizadas
   de un Project de Claude Desktop (Project settings → Custom
   instructions) y habilitar `hermes-mcp` en las conversaciones de ese
   Project -- no hace falta escribirlo de nuevo.

## Tests

```
go test ./...
```

## Estructura

```
cmd/hermes-mcp/            entrypoint
internal/mcp/               protocolo MCP sobre stdio
internal/domain/            tipos de entidad, mapeo a colección Mongo
internal/store/              interfaz Store + validación de pipelines (solo lectura)
internal/store/mongostore/  implementación real contra MongoDB
internal/store/mockstore/   fallback en memoria, solo para tests
internal/graph/              búsqueda y traversal de entidades vivas (usa "seek")
internal/dictionary/        diccionario de campos + jerarquía estática
internal/reporting/          regla de salida (pantalla/archivo, streaming)
internal/tools/               conecta las 4 tools MCP con la lógica de arriba
prompts/                     system prompts del Agente Hermes (negocio y técnico)
relaciones/                  ficheros de relaciones -- *.example.json versionados, el resto no
seed/seed.js                 datos de prueba para un sandbox desde cero -- nunca contra la base real
```

## Decisión de negocio: clase/subclase de punto (`operativeLevel`)

`data.additionalFields.itemClass`/`itemSubClass` de `punto` (nombre de
negocio: "clase"/"subclase" de punto -- distinto de `pointOfServiceType`,
"tipo de punto") se traducen a significado contra `operativeLevel`, una
colección auxiliar de Hermes (catálogo, no una entidad: no tiene
`ucode`/`parentUCode` y no participa del grafo de las 6 entidades del
alcance v1). Está fuera del alcance de `hermes-mcp` como entidad
consultable -- no puede ser `coleccionInicial` de un pipeline -- pero
**sí se puede consultar en vivo**: `operativeLevel` está habilitada como
destino de `$lookup`/`$graphLookup` de solo lectura (ver
`internal/store/pipeline_guard.go`, `coleccionesAuxiliaresPermitidas`),
típicamente con un `$lookup` sin correlación desde `servicePoint`. El
mecanismo y la tabla de códigos conocidos quedan además documentados en el
fichero de relaciones externo (`FieldInfo` de `punto`, ver `relaciones/`
más abajo) para cuando no haga falta ir a Mongo.

Decisión de negocio tomada en esta sesión: **siempre se usa el catálogo
de Chile (`country: "CL"`) para traducir estos códigos, sin importar el
país real del punto.** No es comportamiento del backend de Hermes (que no
tiene ningún fallback por país) -- es una simplificación propia. Razón
principal: el catálogo de Chile trae, para cada clase, una fila general
con subclase VACÍA además de las subclases específicas -- Brasil no tiene
esa fila general, solo subclases específicas -- y esa fila general es la
que permite resolver una búsqueda por clase sola (sin subclase) con una
descripción útil. Respaldo adicional: comparando el catálogo de Brasil y
Chile, el mismo código (`level1Code`+`level2Code`) trae la misma
clasificación en ambos países, solo cambia el idioma de la descripción.
Limitación conocida: el
catálogo de Chile es más chico en el nivel de subclase que lo que usan
otros países (ej. Portugal tiene 11 subclases reales para "Extintores",
Chile solo 1) -- una subclase real de un punto de otro país puede no
tener traducción en el catálogo de Chile aunque la clase (nivel 1) sí
resuelva. Detalle completo en `Hermes AI/grafo-entidades-hermes.md`.

## De qué base/entorno vienen los datos (`origenDatos`)

`relacion_entre_entidades` siempre incluye un campo `origenDatos` con lo que
traiga el fichero de relaciones cargado (`HERMES_MCP_RELACIONES_PATH`, ver
`relaciones/relaciones.example.json`) -- texto libre, pensado para decir de qué
base/entorno viene lo que está conectado *ahora mismo* (ej.
`"subconjunto restaurado de UAT, sept. 2026"`). Si ese campo viene vacío en
el archivo, `origenDatos` trae una instrucción explícita en vez de quedar
vacío: no asumir ni nombrar un entorno (UAT, dev, QA, producción) por
conocimiento previo ni por texto fijo de un prompt.

Esto existe porque un agente puede terminar calificando datos de prueba
obviamente falsos (`serialNumber` repetido, marcas tipo "QA-...") como "de
producción" o "de UAT" simplemente porque un prompt lo da por hecho fijo,
aunque en realidad sea una foto de un momento puntual y ni siquiera de la
totalidad del entorno. Mantener `origenDatos` en el fichero de relaciones
(no en el código ni en el prompt) permite actualizar un solo lugar cuando
cambia el Mongo conectado, sin tener que recompilar ni redistribuir el
binario -- útil sobre todo si hay más de una instancia corriendo (ej. una
para Claude Desktop, otra detrás de un proxy MCP-to-OpenAPI). Si el
servidor se apunta a otro Mongo (otro entorno, otro subconjunto), alcanza
con actualizar `origenDatos` en `relaciones/relaciones.json` -- no hace
falta tocar ningún prompt ni ningún `.go` para que el agente deje de
arrastrar una suposición vieja.

## Qué falta

- Tabla de códigos de `centerType`/`pointOfServiceType` (son códigos cortos
  sin significado documentado en el Excel ni en el código).
