# hermes-mcp

Servidor MCP (stdio, JSON-RPC) para consultar las entidades tipo cuenta de
Hermes (Prosegur): holding, grupo, cliente, centro, punto y dirección.
Pensado para correr en local y conectarse desde Claude Desktop. Contexto de
producto en `Hermes AI/propuesta-mcp-hermes-jefe.md` y
`Hermes AI/grafo-entidades-hermes.md` (fuera de este repo).

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

## Variables de entorno

Copiar `.env.example` a `.env` y completar según haga falta.

| Variable | Para qué | Default |
|---|---|---|
| `HERMES_MCP_MONGO_URI` | Conexión a Mongo. Vacío explícito fuerza `mockstore` (sin pipelines). | Mongo local (ver `main.go`) |
| `HERMES_MCP_MONGO_DB` | Base a usar. | `hermes-local` |
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
Para Claude Desktop, apuntar `claude_desktop_config.json` al binario
compilado (no `go run`).

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
prompts/                     system prompt del Agente Hermes
seed/seed.js                 datos de prueba para un sandbox desde cero -- nunca contra la base real
```

## Qué falta

- Tabla de códigos de `centerType`/`pointOfServiceType` (son códigos cortos
  sin significado documentado en el Excel ni en el código).
- Confirmar contra Mongo real los campos de `serviceAddress`.
- Correr `go build ./... && go test ./...` contra un entorno con acceso de
  red real (no verificado en la sesión donde se escribió este código).
