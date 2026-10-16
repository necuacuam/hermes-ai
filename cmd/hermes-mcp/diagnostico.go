package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// describirAutorizacion resume la cabecera Authorization de una llamada
// HTTP para el diagnostico de autenticacion (HERMES_MCP_DIAGNOSTICO_AUTH):
// si llega, con que esquema y, si es un JWT, para quien fue emitido. Nunca
// incluye el token ni datos personales: de la identidad del usuario solo
// dice si los claims vienen, no su valor. No valida la firma: es un
// diagnostico, no una autenticacion.
func describirAutorizacion(cabecera string) string {
	if cabecera == "" {
		return "sin cabecera Authorization"
	}
	esquema, token, ok := strings.Cut(cabecera, " ")
	if !ok || !strings.EqualFold(esquema, "Bearer") {
		return fmt.Sprintf("esquema %q (no Bearer)", esquema)
	}
	partes := strings.Split(token, ".")
	if len(partes) != 3 {
		return fmt.Sprintf("Bearer que no es un JWT (%d caracteres)", len(token))
	}
	carga, err := base64.RawURLEncoding.DecodeString(partes[1])
	if err != nil {
		return "Bearer con forma de JWT, pero la carga no se puede leer"
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(carga, &claims); err != nil {
		return "Bearer con forma de JWT, pero la carga no es JSON"
	}

	var datos []string
	for _, c := range []string{"iss", "aud", "scp", "roles", "tid", "azp", "appid", "ver"} {
		if v, ok := claims[c]; ok {
			datos = append(datos, fmt.Sprintf("%s=%v", c, v))
		}
	}
	if exp, ok := claims["exp"].(float64); ok {
		datos = append(datos, "exp="+time.Unix(int64(exp), 0).UTC().Format(time.RFC3339))
	}
	var identidad []string
	for _, c := range []string{"oid", "upn", "email", "preferred_username"} {
		if _, ok := claims[c]; ok {
			identidad = append(identidad, c)
		}
	}
	return fmt.Sprintf("JWT: %s | claims de identidad presentes: %v", strings.Join(datos, " "), identidad)
}
