package domain

import "strings"

// stripAccents normaliza los acentos/dieresis mas comunes del espanol.
// Deliberadamente simple (mapa fijo, no unicode/norm) para no sumar una
// dependencia externa por esto -- alcanza para nombres de negocio en
// espanol como "Cámara"/"camara" o "Dirección"/"direccion". Si en datos
// reales de UAT aparecen otros idiomas con mas diacriticos, ampliar este
// mapa o pasar a golang.org/x/text/unicode/norm.
var accentReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
)

func normalize(s string) string {
	return accentReplacer.Replace(strings.ToLower(s))
}

// ContainsFold compara dos strings ignorando mayusculas/minusculas y los
// acentos comunes del espanol (ver normalize) -- para que "Cámara" matchee
// con la busqueda "camara" o "amara" tal como escribiria un usuario o un
// LLM sin acentuar.
func ContainsFold(haystack, needle string) bool {
	return strings.Contains(normalize(haystack), normalize(needle))
}

// AttributeContainsFold busca needle (sin distinguir mayusculas) en el
// valor de un atributo del documento. Devuelve false si el atributo no
// existe, o si no es un string (ej. un booleano o un array -- "contains"
// no tiene sentido sobre esos tipos, y los atributos reales de Hermes
// mezclan strings/booleanos/arrays en el mismo objeto "data").
func (d Document) AttributeContainsFold(key, needle string) bool {
	v, ok := d.Attributes[key]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return ContainsFold(s, needle)
}
