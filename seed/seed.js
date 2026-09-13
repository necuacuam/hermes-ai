// ATENCION -- LEER ANTES DE CORRER ESTO (2026-09-11):
//
// Este proyecto ya NO usa datos sinteticos para trabajar -- la base real es
// la que dio el jefe, restaurada aparte (ej. con mongorestore) en el Mongo
// local al que apunta HERMES_MCP_MONGO_URI. Esa base NO SE TOCA EN NINGUN
// MOMENTO, ni siquiera para "solo agregar" datos de prueba encima.
//
// Este script sigue existiendo UNICAMENTE para un sandbox de desarrollo
// desde cero (por ejemplo, para levantar un Mongo vacio y correr los tests
// de Go, o para probar un cambio de codigo sin tocar nada real). Las
// primeras lineas hacen deleteMany({}) en las 5 colecciones -- si esto se
// corre por error contra la base real, BORRA los datos del jefe sin poder
// deshacerlo.
//
// Regla simple para no confundirse: antes de correr
//   mongosh "<URI>" seed/seed.js
// confirmar que <URI>/<db> es un Mongo vacio de prueba, nunca la base que
// tiene los datos reales. En caso de duda, no correr este script.
//
// (Contenido identico a internal/store/mockstore/fixtures.go -- eso si
// sigue valiendo, es solo para los tests de Go y para este sandbox.)

const dbName = db.getName();

// Guarda de seguridad basica: si el nombre de la base no parece claramente
// una base de sandbox/test, no se corre nada. No es infalible (alguien
// podria nombrar la base real "hermes-local" por descuido), pero frena el
// caso mas comun de correr esto sin fijarse en que URI/db se esta parado.
if (!/test|local|sandbox|dev/i.test(dbName)) {
  throw new Error(
    `ABORTADO: la base "${dbName}" no tiene pinta de ser un sandbox de ` +
    `prueba (no matchea /test|local|sandbox|dev/i). Este script BORRA las ` +
    `5 colecciones antes de sembrar -- si esta apuntando a la base real ` +
    `del jefe, hay que cambiar de URI/db, NO forzar este script.`
  );
}

print(`Sembrando datos de prueba en la base "${dbName}"...`);

db.serviceHolding.deleteMany({});
db.serviceGroup.deleteMany({});
db.serviceEntity.deleteMany({});
db.servicePlace.deleteMany({});
db.servicePoint.deleteMany({});

// --- Cadena Macedonia (ejemplo de la reunion con el jefe) ---

db.serviceHolding.insertOne({
  ucode: "ES00000001",
  name: "Prosegur Global",
});

db.serviceGroup.insertOne({
  ucode: "ES01000001",
  parentUCode: "ES00000001",
  name: "Grupo Macedonia Iberia",
});

db.serviceEntity.insertOne({
  ucode: "ES02000001",
  parentUCode: "ES01000001",
  name: "Macedonia",
  data: { taxpayerId: "B00000001" },
});

db.servicePlace.insertOne({
  ucode: "ES03000001",
  parentUCode: "ES02000001",
  name: "Sucursal 23 (Madrid)",
  data: { centerType: "Sucursal", addressCity: "Madrid" },
});

// Centro anidado dentro de otro centro -- ver la nota sobre anidamiento
// centro-en-centro en el README / internal/graph/graph.go.
db.servicePlace.insertOne({
  ucode: "ES03000002",
  parentUCode: "ES03000001",
  name: "Sala 3",
  data: { centerType: "Zona interna" },
});

db.servicePoint.insertMany([
  {
    ucode: "ES04000001",
    parentUCode: "ES03000002",
    name: "Cámara 1",
    data: { pointOfServiceType: "Cámara" },
  },
  {
    ucode: "ES04000002",
    parentUCode: "ES03000002",
    name: "Cámara 2",
    data: { pointOfServiceType: "Cámara" },
  },
  {
    ucode: "ES04000003",
    parentUCode: "ES03000002",
    name: "Sensor de movimiento 1",
    data: { pointOfServiceType: "Sensor" },
  },
  {
    // Colgado directo de la sucursal, sin pasar por Sala 3 -- prueba que
    // el conteo recursivo no cuenta de mas ni de menos segun el nivel.
    ucode: "ES04000004",
    parentUCode: "ES03000001",
    name: "Puesto de vigilancia 1",
    data: { pointOfServiceType: "Puesto de vigilancia" },
  },
]);

// --- Cadena Banco Santander (ejemplo del PDF de la Norma 3P) ---

db.serviceEntity.insertOne({
  ucode: "ES02000002", // sin grupo -- tambien es un caso valido
  name: "Banco Santander",
  data: { taxpayerId: "A39000013" },
});

db.servicePlace.insertOne({
  ucode: "ES03000003",
  parentUCode: "ES02000002",
  name: "Agencia Plaza Conde de Casal 7",
  data: { centerType: "Agencia", addressCity: "Madrid" },
});

db.servicePoint.insertMany([
  {
    ucode: "ES04000005",
    parentUCode: "ES03000003",
    name: "Cámara entrada",
    data: { pointOfServiceType: "Cámara" },
  },
  {
    ucode: "ES04000006",
    parentUCode: "ES03000003",
    name: "Cámara caja",
    data: { pointOfServiceType: "Cámara" },
  },
]);

db.servicePlace.insertOne({
  ucode: "ES03000004",
  parentUCode: "ES03000003",
  name: "Caja fuerte",
  data: { centerType: "Zona interna" },
});

db.servicePoint.insertOne({
  ucode: "ES04000007",
  parentUCode: "ES03000004",
  name: "Cámara caja fuerte",
  data: { pointOfServiceType: "Cámara" },
});

// Indices basicos -- ucode unico por coleccion, y parentUCode para que
// FindChildren no haga collection scan a medida que crezcan los datos.
for (const coll of ["serviceHolding", "serviceGroup", "serviceEntity", "servicePlace", "servicePoint"]) {
  db[coll].createIndex({ ucode: 1 }, { unique: true });
  db[coll].createIndex({ parentUCode: 1 });
}

print("Listo. Resumen:");
for (const coll of ["serviceHolding", "serviceGroup", "serviceEntity", "servicePlace", "servicePoint"]) {
  print(`  ${coll}: ${db[coll].countDocuments({})} documentos`);
}
