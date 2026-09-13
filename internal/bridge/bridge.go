// Package bridge: DESACTIVADO el 2026-09-11 (conversacion con TAvo,
// "olvida lo del ssh porfa").
//
// Este paquete implementaba un puente ciego stdio<->TCP para el escenario
// de tunel SSH inverso (Claude Desktop en una laptop sin acceso a Hermes,
// el binario real corriendo en la laptop de la empresa) -- ver el README
// del mcp-server, seccion "Conectar desde otra maquina" (ya eliminada).
// Ese escenario dejo de aplicar: para esta v1 el Mongo local YA tiene los
// datos necesarios (los que dio el jefe), asi que todo corre en una sola
// maquina y no hace falta ningun tunel.
//
// El codigo funcional (func Dial) se saco a proposito para que quede
// inequivocamente claro que no esta en uso -- no hay ninguna llamada a
// este paquete desde cmd/hermes-mcp ni desde internal/mcp. No se pudo
// borrar el archivo directamente en esta sesion (sin acceso a un shell
// funcional contra este repo); si nadie lo necesita, borrar el directorio
// internal/bridge/ entero (bridge.go + bridge_test.go) a mano.
package bridge
