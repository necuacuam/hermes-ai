# Alta de entidades y tablas auxiliares

Pasos para sumar una entidad o una tabla auxiliar al alcance de Hermes AI. Los términos están definidos en el [glosario](06-glosario.md).

**Principio:** lo que limita qué se puede consultar vive en el código y exige recompilar; el significado y las relaciones viven en el fichero de conocimiento y no exigen recompilar. No mezclar las dos cosas.

## Paso 0 — Investigar antes de tocar nada

No inventar. Confirmar en este orden:

1. **Código de Hermes** (`support-otc-hermes-hms-master`). La configuración real de una entidad está en su `main.go` (`Init()`): si su ucode lleva prefijo de tipo o es libre (`UCODE_FREE`), si tiene padre (`ParentUCode`). La tabla de prefijos de `entity.go` puede estar desactualizada: manda `Init()`. Los campos que persisten están en el `model.go` estándar de la entidad (los marcados `bson:"-"` no se guardan).
2. **Excel de términos de negocio** (`EntidadesOTC.xlsx`): la hoja de la entidad. Es una pista, no la verdad: puede listar campos que el código no tiene o marcar mal su alcance.
3. **Adapters** (integraciones que leen o escriben la entidad): sus transformaciones muestran la forma exacta de un campo y de dónde salen los identificadores externos. Solo se leen; nunca se modifican.
4. **Índices de la colección** (pedirlos a quien tenga acceso a la base): señalan por qué campos se filtra o relaciona de verdad. Son candidatos, no confirmaciones.
5. **Un documento real vía `reporting`,** una vez que la colección está en el registro (Paso 2): `{"coleccionInicial": "<alias>", "pipeline": [{"$limit": 1}], "modo": "listado", "mostrarEnPantalla": true}`. Es lo único que confirma qué campos están en la raíz y cuáles bajo `data`.

## Paso 1 — Elegir el alias

El nombre de negocio que usará `coleccionInicial` (por ejemplo `salesPerson`). Si coincide con el nombre técnico de la colección, a diferencia de las entidades `serviceX`, decirlo en `general` del fichero de conocimiento.

## Paso 2 — Código (exige recompilar)

1. **`internal/domain/entities.go`:**
   - Agregar la constante a `EntityType`.
   - Agregarla a `AllTypes`.
   - Agregarla a `CollectionName` con el nombre real de la colección.
   - Si es una **tabla auxiliar,** solo agregarla a `TablasAuxiliares`.
2. **Nada más,** salvo que la entidad tenga una regla de validación especial (`internal/store/pipeline_guard.go`). Los `enum` de las herramientas y la lista de colecciones permitidas se arman solos desde el registro.
3. **Compilar y reiniciar** cada instalación (Windows y, si se usa, Linux). Sin esto la entidad no existe para ningún agente, aunque esté documentada.

## Paso 3 — Fichero de conocimiento (sin recompilar)

Siguiendo las [reglas de contenido](04-fichero-de-conocimiento.md#reglas-de-contenido):

1. **`campos.<alias>`:** una entrada por campo confirmado en el código o en un documento real. Si un campo aparece en una fuente y no en otra, se dice así en `fuente`; nunca se presenta como hecho.
2. **`prefijosUcode.<alias>`:** solo si la entidad usa prefijo de tipo según `Init()`. Si es `UCODE_FREE`, no se agrega.
3. **`relaciones`:** una entrada por cada relación con las demás entidades. Lo no confirmado va con `estado: "sin confirmar"`; las hipótesis descartadas, en `fuente`.

Aplicar el cambio en todos los ficheros de conocimiento (ver la tabla de [ficheros](04-fichero-de-conocimiento.md#ficheros)).

## Paso 4 — Verificar

1. Si no se hizo antes, consultar un documento real (Paso 0.5). No documentar campos solo desde el Excel o solo desde el código si hay acceso a la base.
2. Probar las tres herramientas:
   - `diccionario_de_campos` con el alias: devuelve los campos esperados.
   - `relacion_entre_entidades`: el alias aparece en `tipos` y `coleccionMongo`, y sus relaciones se leen bien.
   - `reporting` con el alias y un `$match` sobre un campo documentado.
3. Cada relación nueva se prueba con un pipeline real que la use. Si no se puede cruzar en un solo pipeline (por ejemplo, por tipos incompatibles), se documenta cómo resolverla en dos consultas.
4. Comprobar que la entidad tiene campos en todos los ficheros: el servidor no lo valida.

## Paso 5 — Iterar con quien conoce el negocio

Una relación real rara vez se resuelve a la primera. El flujo:

1. Proponer la hipótesis más probable y probarla con un pipeline.
2. Mostrar el resultado antes de escribir nada en el fichero. Si no funcionó, decir qué se descartó y preguntar si hay una pista mejor (un índice, un adapter, alguien del equipo).
3. Escribir en el fichero cuando haya algo confirmado, aunque sea parcial, y decir qué sigue abierto. Si aparece una corrección, editar el fichero las veces que haga falta: una entrada vieja sin corregir es peor que no tenerla.
4. Probar un candidato no necesita permiso; agotar las hipótesis razonables sí amerita parar y consultar.
