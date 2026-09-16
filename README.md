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
  Mongo, por tipo de entidad. Estática (hardcodeada en
  `internal/dictionary`), no toca Mongo.
- **`relacion_entre_entidades`**: jerarquía de tipos, prefijos de ucode,
  nombre real de cada colección Mongo y el mecanismo de `parentUCode`.
  También estática.
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

**Un solo lugar para todo: `.env`, al lado de `hermes-mcp.exe`.** Copiar
`.env.example` a `.env` y completar ahí -- lo lee el binario mismo (ver
`loadDotEnv` en `main.go`), sea cual sea la forma de arrancarlo (Claude
Desktop, `go run`, los scripts de `scripts\`). No hace falta nada en el
bloque `env` de `claude_desktop_config.json` salvo que se quiera pisar
puntualmente un valor desde ahí para una instalación en particular (una
variable ya seteada en el entorno real siempre gana sobre `.env`). Si
estás depurando qué está conectado de verdad, `.env` es la respuesta
completa -- no hace falta mirar `claude_desktop_config.json` para nada
salvo overrides puntuales. `HERMES_MCP_MONGO_URI` no tiene ningún default
embebido en el código: si no está en `.env` ni en el entorno real, el
servidor no arranca.

| Variable | Para qué | Default |
|---|---|---|
| `HERMES_MCP_MONGO_URI` | Conexión a Mongo. Vacío explícito fuerza `mockstore` (sin pipelines). | **ninguno -- obligatoria** (sin ella, o sin este archivo, el servidor no arranca) |
| `HERMES_MCP_MONGO_DB` | Base a usar. | `hermes-local` |
| `HERMES_MCP_ORIGEN_DATOS` | **Actualizar siempre junto con las dos de arriba.** Texto libre: de qué base/entorno viene lo conectado ahora (ej. `"subconjunto restaurado de UAT, sept. 2026"`, o `"producción"` si algún día se apunta ahí). Lo expone `relacion_entre_entidades` (`origenDatos`) para que el agente nunca tenga que asumir un entorno de memoria o de un prompt desactualizado -- ver sección más abajo. | vacío (el agente recibe una instrucción explícita de no adivinar el entorno) |
| `HERMES_MCP_SALIDAS_DIR` | Carpeta de salida de `reporting` (`guardarEnArchivo=true`). | `./salidas` |
| `HERMES_MCP_LIMITE_PANTALLA_DEFAULT` | Filas máximas en pantalla si no se especifica. | `10` |
| `HERMES_MCP_MONGO_TIMEOUT` | Timeout por pipeline. | `30s` |

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

- **`.env`** (al lado de `hermes-mcp.exe`, nunca se versiona): TODA la
  configuración real -- Mongo, origen de datos, carpeta de salidas,
  topes de recursos. Es el único lugar que hace falta mirar para saber
  qué está conectado.
- **`claude_desktop_config.json`**: solo le dice a Claude Desktop qué
  binario levantar. No necesita bloque `env` en el uso normal.

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
   HERMES_MCP_ORIGEN_DATOS=subconjunto restaurado de UAT, sept. 2026
   HERMES_MCP_SALIDAS_DIR=C:\ruta\completa\a\mcp-server\generatedfiles
   ```

   **Actualizar siempre `HERMES_MCP_ORIGEN_DATOS` junto con
   `HERMES_MCP_MONGO_URI`, en este mismo archivo, en el mismo momento**
   (ver sección más abajo de por qué). `HERMES_MCP_SALIDAS_DIR` puede ser
   relativa (`./generatedfiles`, resuelta contra el directorio de trabajo
   con el que arranque el proceso) o absoluta -- absoluta es más segura
   si no se sabe con qué cwd va a lanzar Claude Desktop al binario.
3. Editar `claude_desktop_config.json` (menú Claude Desktop → Settings →
   Developer → Edit Config) y agregar una entrada bajo `mcpServers` con
   **solo** `command`:

   ```json
   {
     "mcpServers": {
       "hermes-mcp": {
         "command": "C:\\ruta\\completa\\a\\mcp-server\\hermes-mcp.exe"
       }
     }
   }
   ```

   Ruta absoluta, no relativa. No hace falta ningún bloque `"env"` acá --
   el binario ya lee `.env` solo. La única razón válida para agregar
   `"env": {...}` en esta misma entrada es pisar puntualmente una
   variable para ESTA instalación de Claude Desktop en particular sin
   tocar el `.env` compartido (ej. dos instalaciones que comparten
   carpeta pero apuntan a Mongos distintos) -- una variable seteada ahí
   siempre gana sobre `.env`.
4. Reiniciar Claude Desktop por completo (cerrar el proceso, no solo la
   ventana) para que levante el servidor. Cualquier recompilación
   posterior del binario, o edición de `.env`, también requiere este
   mismo reinicio completo -- Claude Desktop no re-lee ninguno de los dos
   en caliente.
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
mecanismo y la tabla de códigos conocidos quedan además hardcodeados en
`internal/dictionary` (ver el `FieldInfo` de `punto`) para cuando no haga
falta ir a Mongo.

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
se haya configurado en `HERMES_MCP_ORIGEN_DATOS` -- texto libre, pensado
para decir de qué base/entorno viene lo que está conectado *ahora mismo*
(ej. `"subconjunto restaurado de UAT, sept. 2026"`). Si esa variable no
está seteada, el campo trae una instrucción explícita en vez de quedar
vacío: no asumir ni nombrar un entorno (UAT, dev, QA, producción) por
conocimiento previo ni por texto fijo de un prompt.

Esto se agregó (2026-09-16) después de un caso real: un agente en producción
calificó datos de prueba obviamente falsos (`serialNumber` repetido, marcas
tipo "QA-...") como "de esta restauración UAT", solo porque el prompt
técnico lo daba por hecho fijo -- cuando en realidad era una foto de un
momento (y ni siquiera del UAT completo, sino de un subconjunto). Si el
servidor se apunta a otro Mongo (otro entorno, otro subconjunto), alcanza
con cambiar esta variable -- no hace falta tocar ningún prompt ni ningún
`.go` para que el agente deje de arrastrar una suposición vieja.

## Qué falta

- Tabla de códigos de `centerType`/`pointOfServiceType` (son códigos cortos
  sin significado documentado en el Excel ni en el código).
