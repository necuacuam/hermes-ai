package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func jwtDePrueba(carga string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(carga)) + ".firma"
}

func TestDescribirAutorizacion(t *testing.T) {
	token := jwtDePrueba(`{"iss":"https://login.microsoftonline.com/t/v2.0","aud":"api://hermes-mcp","scp":"access_as_user","tid":"t","email":"alguien@ejemplo.com","oid":"123","exp":1700000000}`)
	casos := []struct {
		nombre, cabecera string
		contiene         []string
		noContiene       []string
	}{
		{"sin cabecera", "", []string{"sin cabecera"}, nil},
		{"otro esquema", "Basic abc", []string{`"Basic"`}, nil},
		{"bearer opaco", "Bearer abc123", []string{"no es un JWT", "6 caracteres"}, []string{"abc123"}},
		{"jwt", "Bearer " + token,
			[]string{"aud=api://hermes-mcp", "scp=access_as_user", "tid=t", "exp=2023-11-14", "[oid email]"},
			[]string{"alguien@ejemplo.com", "123", token}},
	}
	for _, c := range casos {
		got := describirAutorizacion(c.cabecera)
		for _, s := range c.contiene {
			if !strings.Contains(got, s) {
				t.Errorf("%s: falta %q en %q", c.nombre, s, got)
			}
		}
		for _, s := range c.noContiene {
			if strings.Contains(got, s) {
				t.Errorf("%s: no deberia aparecer %q en %q", c.nombre, s, got)
			}
		}
	}
}
