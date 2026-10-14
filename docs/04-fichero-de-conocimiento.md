# Fichero de conocimiento

El fichero de conocimiento es el JSON que convierte el MCP Server en un experto en Hermes: dice qué significa cada campo, cómo se relacionan las entidades y cómo están armados los documentos. Lo leen las herramientas `diccionario_de_campos` y `relacion_entre_entidades`. Los términos están definidos en el [glosario](06-glosario.md).

Se carga una sola vez al arrancar, desde la ruta de `HERMES_MCP_RELACIONES_PATH`. Cambiarlo no exige recompilar: basta con reiniciar el servidor.

## Reglas de contenido

Vienen de la [constitución](01-constitucion.md), sección 4:

- **Nunca contiene contenido de la base:** ni conteos, ni valores observados, ni copias de catálogos o tablas auxiliares, ni ejemplos con datos reales (nombres, códigos, identificadores). Describe la estructura, las relaciones y el significado. Lo que está en una colección se consulta con `reporting`. Esto vale también para `fuente`.
- **`descripcion` es lo que el agente le dirá al usuario:** solo el significado del campo y sus limitaciones, sin contar cómo se averiguó.
- **`fuente` es la procedencia** (código, documentación, persona, consulta con fecha). El servidor no se la entrega al agente.
- **Cada relación se describe una sola vez,** en `relaciones`. La descripción del campo dice qué significa y remite a `relaciones`.
- **No se inventa significado a partir del nombre de un campo.** Si no se conoce, la descripción lo dice.
- **Una decisión del proyecto no es una regla de Hermes:** si se decide algo propio, el texto lo dice ("por decisión de este proyecto").
- **Si las fuentes se contradicen, manda el código de Hermes.** Si el código cambia, el fichero se adapta.

## Estructura

```jsonc
{
  // OPCIONALES
  "origenDatos": "de qué base/entorno vienen los datos (lo completa cada instalación)",
  "general": "lo que no es una relación concreta",
  "prefijosUcode": { "holding": "00", "grupo": "01", "...": "..." },

  // OBLIGATORIOS: si falta alguno o viene vacío, el servidor no arranca
  "estructuraDeCampos": "qué campos están en la raíz del documento y cuáles bajo 'data'",
  "relaciones": [
    {
      "tipo": "padre | anidamiento | referencia | catalogo",
      "desde": ["centro"],
      "hacia": "cliente",
      "campoOrigen": "parentUCode",
      "campoDestino": "ucode",
      "estado": "confirmado | sin confirmar | informativo",
      "reglas": "los matices de la relación",
      "fuente": "procedencia (interna, opcional)"
    }
  ],
  "campos": {
    "cliente": [ { "campo": "data.customerName", "descripcion": "...", "fuente": "..." } ],
    "...": [ ]
  }
}
```

### `relaciones`

| Clave | Qué es |
|---|---|
| `tipo` | `padre` (la cadena por `parentUCode`), `anidamiento` (una entidad dentro de otra del mismo tipo), `referencia` (cualquier otra clave entre entidades) o `catalogo` (cruce con una tabla auxiliar). |
| `desde` | Lista de entidades que tienen la relación. Una relación igual en varias entidades se escribe una sola vez. |
| `hacia` | Una entidad; en `catalogo`, una tabla auxiliar; en `anidamiento` va vacío, porque siempre es al mismo tipo. |
| `campoOrigen` / `campoDestino` | Los campos del cruce, listos para un `$lookup`. |
| `estado` | `confirmado` (lo prueban el código o los datos), `sin confirmar` (hipótesis o fuente no verificada), `informativo` (existe, pero no sirve para recorrer). |
| `reglas` | Todo matiz: cuándo viene vacío, qué rechaza Hermes, cómo se resuelve si no se puede cruzar en un solo pipeline, qué está pendiente de confirmar. |
| `fuente` | Procedencia. Las hipótesis descartadas van acá. |

### `campos`

- Las claves son los alias del registro.
- `campo` es el nombre técnico tal como está en MongoDB, con la ruta si está anidado (`data.customerName`). Varios campos que van juntos se pueden documentar en una entrada, separados por " / ".

## Validación al arrancar

El servidor rechaza el fichero, y no arranca, si:

- el JSON no es válido o tiene una clave desconocida (por ejemplo, un error de tipeo);
- falta `estructuraDeCampos`, `relaciones` o `campos`, o vienen vacíos;
- una relación nombra una entidad o una tabla auxiliar que no está en el registro, o un tipo o un estado que no existen;
- un `catalogo` no apunta a una tabla auxiliar, o un `anidamiento` trae `hacia`;
- `campos` o `prefijosUcode` usan una entidad que no está en el registro.

No valida que todas las entidades del registro tengan campos documentados: eso se revisa a mano (ver [Alta de entidades](05-alta-de-entidades.md)).

## Ficheros

| Fichero | Uso | Se versiona |
|---|---|---|
| `relaciones/relaciones.example.json` | Plantilla, con `origenDatos` vacío. | Sí |
| `relaciones/relaciones.json` | El que usa la instalación de Windows. | No |
| `relaciones/relaciones.uat-mainaccountnumber.json` | Copia con otro nombre; se mantiene sincronizada con `relaciones.json`. | No |
| `dist-linux/relaciones/relaciones.json` y `relaciones.example.json` | Los de la instalación de Linux. | No |

Un cambio que aplica a todos los entornos se hace en todos los ficheros, incluida la plantilla. Después, reiniciar el servidor.

## Pendientes conocidos

- `centerType` y `pointOfServiceType` son códigos cortos sin significado documentado ni en el Excel de Hermes ni en el código.
- "Vacío = cabecera" en el anidamiento sale de leer el código; falta confirmarlo contra las cabeceras reales del negocio.
- Muchos campos están confirmados en el código pero sin cruzar contra el Excel de términos de negocio (lo dice su `fuente`).
