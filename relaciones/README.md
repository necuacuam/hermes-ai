# Fichero de relaciones

## Qué es

Es la configuración de negocio de `hermes-mcp`, externa al código Go. El
servidor la carga una única vez al arrancar (`LoadRelations`, en
`internal/dictionary/dictionary.go`), desde la ruta que indique la
variable de entorno `HERMES_MCP_RELACIONES_PATH` (se configura en el
bloque `env` de `claude_desktop_config.json`, no en `.env` — ver el
`README.md` de la raíz del proyecto).

Contiene todo lo que responden las tools `diccionario_de_campos` y
`relacion_entre_entidades`: qué campos existen en cada tipo de entidad y
qué significan en términos de negocio, cómo se relacionan los tipos entre
sí, y de qué base/entorno vienen los datos conectados ahora mismo. Es
contenido curado por una persona (leyendo el Excel de referencia y/o el
código fuente de Hermes), no lógica — por eso vive en un JSON aparte y no
hardcodeado en Go: cambiar una descripción o agregar un campo no debería
exigir recompilar y redistribuir el binario.

Hay dos archivos en esta carpeta:

- **`relaciones.json`**: el fichero real, con datos concretos del entorno
  conectado. No se versiona (está en `.gitignore`).
- **`relaciones.example.json`**: plantilla versionada, con `origenDatos`
  en blanco. Sirve de punto de partida para crear `relaciones.json` y de
  referencia de la estructura esperada.

Puede haber más de un fichero de relaciones en esta carpeta (por ejemplo,
uno por entorno) — `HERMES_MCP_RELACIONES_PATH` decide cuál se carga en
cada arranque.

## Estructura

```jsonc
{
  "origenDatos": "texto libre. Único campo OPCIONAL — puede ir vacío.",

  // Los siguientes son OBLIGATORIOS: si falta alguno o viene vacío,
  // el servidor no arranca.
  "mecanismoDeRelacion": "texto libre",
  "estructuraDeCampos": "texto libre",
  "direccion": "texto libre",
  "otrasRelaciones": "texto libre",
  "anidamiento": {
    "segunCodigoHermes": "texto libre",
    "segunDocumentacionDeNegocio": "texto libre",
    "estadoActual": "texto libre"
  },

  // También obligatorio (no puede faltar ni venir vacío).
  "campos": {
    "holding": [ { "campo": "...", "descripcion": "...", "fuente": "..." } ],
    "grupo":    [ ... ],
    "cliente":  [ ... ],
    "centro":   [ ... ],
    "punto":    [ ... ],
    "direccion": [ ... ]
  }
}
```

Notas sobre `campos`:

- Las 6 claves (`holding`, `grupo`, `cliente`, `centro`, `punto`,
  `direccion`) son los tipos de entidad de Hermes.
- Cada elemento de la lista es un campo documentado: `campo` es el nombre
  técnico tal como vive en Mongo (con el prefijo `data.` cuando
  corresponde — todo salvo `ucode`/`parentUCode` vive anidado ahí),
  `descripcion` es el significado de negocio en términos que se le pueden
  repetir al usuario tal cual, y `fuente` es metadata interna de
  mantenimiento (de dónde salió el dato: Excel, código fuente, UAT) que
  **nunca se le muestra al usuario** — el agente la usa solo para
  priorizar de qué campos hablar con confianza.

## Cómo actualizarlo

1. Editar `relaciones.json` (el real, no la plantilla) directamente con
   cualquier editor de texto/JSON.
2. Reiniciar el servidor MCP (en Claude Desktop: reiniciar la app, o
   recargar la conexión MCP) para que recargue el archivo — se lee solo
   al arrancar, un cambio en caliente no tiene efecto hasta reiniciar.
3. El parseo es estricto: una clave que no exista en la estructura de
   arriba (por ejemplo, un typo) hace que el servidor falle al arrancar
   en vez de ignorarla en silencio. Igual con cualquier campo obligatorio
   vacío. El mensaje de error indica qué falta o qué sobra.
4. Si se agrega una entidad o un campo nuevo a documentar en `campos`,
   confirmar primero el nombre real y el significado contra el Excel de
   referencia y/o el código fuente de Hermes antes de escribirlo — no
   inventar significado de negocio a partir del nombre del campo.
5. Si el cambio aplica a todos los entornos (no es un dato puntual de
   este `origenDatos`), replicarlo también en `relaciones.example.json`
   para que la plantilla se mantenga al día.

No hace falta tocar ningún archivo `.go` ni recompilar el binario para
ninguno de estos cambios.
