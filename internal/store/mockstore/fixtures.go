package mockstore

import "hermes-mcp/internal/domain"

// Fixtures devuelve datos inventados para poder probar el servidor de
// punta a punta sin Mongo real. Cubren dos casos:
//
//  1. El ejemplo de la reunion con el jefe: cliente Macedonia -> sucursal 23
//     (Madrid) -> sala 3 -> camaras -- con la sala como un CENTRO anidado
//     dentro de otro centro, tal como describe la Norma General 3P (aunque
//     el codigo actual de Hermes todavia no lo permite -- ver README).
//  2. El ejemplo del propio documento de la Norma 3P: Banco Santander ->
//     Agencia Plaza Conde de Casal 7 -> caja fuerte -> camaras -- mismo
//     patron, para probar que no es un caso especial de los datos.
//
// Los ucodes siguen el formato real de Hermes: pais + prefijo MDM de 2
// digitos + 8 digitos secuenciales.
func Fixtures() []domain.Document {
	return []domain.Document{
		// --- Cadena Macedonia ---
		{
			UCode: "ES00000001", Type: domain.Holding, Name: "Prosegur Global",
		},
		{
			UCode: "ES01000001", ParentUCode: "ES00000001", Type: domain.Grupo,
			Name: "Grupo Macedonia Iberia",
		},
		{
			UCode: "ES02000001", ParentUCode: "ES01000001", Type: domain.Cliente,
			Name: "Macedonia", Attributes: map[string]interface{}{"taxpayerId": "B00000001"},
		},
		{
			UCode: "ES03000001", ParentUCode: "ES02000001", Type: domain.Centro,
			Name: "Sucursal 23 (Madrid)",
			Attributes: map[string]interface{}{
				"centerType":  "Sucursal",
				"addressCity": "Madrid",
			},
		},
		{
			// Centro anidado dentro de otro centro -- ver nota en el
			// comentario del paquete.
			UCode: "ES03000002", ParentUCode: "ES03000001", Type: domain.Centro,
			Name:       "Sala 3",
			Attributes: map[string]interface{}{"centerType": "Zona interna"},
		},
		{
			UCode: "ES04000001", ParentUCode: "ES03000002", Type: domain.Punto,
			Name: "Cámara 1", Attributes: map[string]interface{}{"pointOfServiceType": "Cámara"},
		},
		{
			UCode: "ES04000002", ParentUCode: "ES03000002", Type: domain.Punto,
			Name: "Cámara 2", Attributes: map[string]interface{}{"pointOfServiceType": "Cámara"},
		},
		{
			UCode: "ES04000003", ParentUCode: "ES03000002", Type: domain.Punto,
			Name: "Sensor de movimiento 1", Attributes: map[string]interface{}{"pointOfServiceType": "Sensor"},
		},
		{
			// Punto colgado directo de la sucursal, sin pasar por Sala 3 --
			// para probar que el conteo recursivo no cuenta de mas ni de
			// menos segun el nivel donde este el punto.
			UCode: "ES04000004", ParentUCode: "ES03000001", Type: domain.Punto,
			Name: "Puesto de vigilancia 1", Attributes: map[string]interface{}{"pointOfServiceType": "Puesto de vigilancia"},
		},

		// --- Cadena Banco Santander (mismo patron que el ejemplo del PDF de la Norma 3P) ---
		{
			UCode: "ES02000002", Type: domain.Cliente, // sin grupo -- tambien es un caso valido
			Name: "Banco Santander", Attributes: map[string]interface{}{"taxpayerId": "A39000013"},
		},
		{
			UCode: "ES03000003", ParentUCode: "ES02000002", Type: domain.Centro,
			Name:       "Agencia Plaza Conde de Casal 7",
			Attributes: map[string]interface{}{"centerType": "Agencia", "addressCity": "Madrid"},
		},
		{
			UCode: "ES04000005", ParentUCode: "ES03000003", Type: domain.Punto,
			Name: "Cámara entrada", Attributes: map[string]interface{}{"pointOfServiceType": "Cámara"},
		},
		{
			UCode: "ES04000006", ParentUCode: "ES03000003", Type: domain.Punto,
			Name: "Cámara caja", Attributes: map[string]interface{}{"pointOfServiceType": "Cámara"},
		},
		{
			UCode: "ES03000004", ParentUCode: "ES03000003", Type: domain.Centro,
			Name: "Caja fuerte", Attributes: map[string]interface{}{"centerType": "Zona interna"},
		},
		{
			UCode: "ES04000007", ParentUCode: "ES03000004", Type: domain.Punto,
			Name: "Cámara caja fuerte", Attributes: map[string]interface{}{"pointOfServiceType": "Cámara"},
		},
	}
}
