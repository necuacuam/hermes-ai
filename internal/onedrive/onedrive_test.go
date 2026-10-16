package onedrive_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hermes-mcp/internal/onedrive"
)

type oboFalso struct{ recibido string }

func (o *oboFalso) TokenGraph(_ context.Context, tokenUsuario string) (string, error) {
	o.recibido = tokenUsuario
	return "token-graph", nil
}

// graphFalso simula la sesion de carga de Graph y guarda lo que recibe.
type graphFalso struct {
	mu          sync.Mutex
	rutaSesion  string
	authSesion  string
	authPartes  []string
	recibido    bytes.Buffer
	falloSesion bool
}

func (g *graphFalso) handler(base *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":/createUploadSession"):
			g.rutaSesion = r.URL.EscapedPath()
			g.authSesion = r.Header.Get("Authorization")
			if g.falloSesion {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":{"code":"ResourceNotFound","message":"User's mysite not found."}}`)
				return
			}
			fmt.Fprintf(w, `{"uploadUrl":%q}`, *base+"/subida")
		case r.URL.Path == "/subida":
			g.authPartes = append(g.authPartes, r.Header.Get("Authorization"))
			var desde, hasta, total int64
			fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &desde, &hasta, &total)
			io.Copy(&g.recibido, r.Body)
			if hasta+1 < total {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"webUrl": "https://onedrive.example/archivo"})
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	})
}

func nuevoServidor(t *testing.T, g *graphFalso) *httptest.Server {
	t.Helper()
	var base string
	srv := httptest.NewServer(g.handler(&base))
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func TestSubir_VariasPartes(t *testing.T) {
	g := &graphFalso{}
	srv := nuevoServidor(t, g)
	obo := &oboFalso{}
	s := onedrive.NewConIntercambiador(obo, srv.Client(), srv.URL)

	// Mas de una parte (cada parte es de 3,125 MiB).
	contenido := bytes.Repeat([]byte("0123456789"), 400_000)
	ruta := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(ruta, contenido, 0o644); err != nil {
		t.Fatal(err)
	}

	enlace, err := s.Subir(context.Background(), "token-usuario", "2026-10-16", "clases pt.json", ruta)
	if err != nil {
		t.Fatalf("Subir: %v", err)
	}
	if enlace != "https://onedrive.example/archivo" {
		t.Errorf("enlace = %q", enlace)
	}
	if obo.recibido != "token-usuario" {
		t.Errorf("el intercambio no recibio el token del usuario: %q", obo.recibido)
	}
	if g.authSesion != "Bearer token-graph" {
		t.Errorf("la sesion de carga tiene que llevar el token de Graph: %q", g.authSesion)
	}
	if len(g.authPartes) < 2 {
		t.Errorf("se esperaban varias partes, hubo %d", len(g.authPartes))
	}
	for _, a := range g.authPartes {
		if a != "" {
			t.Errorf("las partes no deben llevar token: %q", a)
		}
	}
	if !bytes.Equal(g.recibido.Bytes(), contenido) {
		t.Errorf("el contenido recibido no coincide (%d vs %d bytes)", g.recibido.Len(), len(contenido))
	}
	if !strings.Contains(g.rutaSesion, "/me/drive/special/approot:/2026-10-16/clases%20pt.json:/createUploadSession") {
		t.Errorf("ruta de la sesion = %q", g.rutaSesion)
	}
}

func TestSubir_ErrorDeGraph(t *testing.T) {
	g := &graphFalso{falloSesion: true}
	srv := nuevoServidor(t, g)
	s := onedrive.NewConIntercambiador(&oboFalso{}, srv.Client(), srv.URL)
	ruta := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(ruta, []byte(`{}`), 0o644)

	_, err := s.Subir(context.Background(), "token-usuario", "d", "x.json", ruta)
	if err == nil || !strings.Contains(err.Error(), "ResourceNotFound") || !strings.Contains(err.Error(), "mysite") {
		t.Fatalf("se esperaba el error de Graph con su codigo y mensaje: %v", err)
	}
}

func TestSubir_SinToken(t *testing.T) {
	s := onedrive.NewConIntercambiador(&oboFalso{}, http.DefaultClient, "http://no-se-usa")
	if _, err := s.Subir(context.Background(), "", "d", "x.json", "no-existe"); err != onedrive.ErrSinToken {
		t.Fatalf("sin token tiene que devolver ErrSinToken: %v", err)
	}
}

func TestTokenDeCabecera(t *testing.T) {
	for cab, esperado := range map[string]string{
		"Bearer abc": "abc", "bearer abc ": "abc", "Basic abc": "", "": "", "Bearer": "",
	} {
		if got := onedrive.TokenDeCabecera(cab); got != esperado {
			t.Errorf("%q -> %q, esperado %q", cab, got, esperado)
		}
	}
}

func TestTenantDeToken(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	token := enc(`{"alg":"none"}`) + "." + enc(`{"tid":"9188040D-6C67-4C5B-B112-36A304B66DAD"}`) + ".firma"
	if got, err := onedrive.TenantDeToken(token); err != nil || got != "9188040d-6c67-4c5b-b112-36a304b66dad" {
		t.Errorf("tid = %q, %v", got, err)
	}
	for _, malo := range []string{"", "no-es-jwt", enc(`{}`) + "." + enc(`{"sub":"x"}`) + ".f"} {
		if _, err := onedrive.TenantDeToken(malo); err == nil {
			t.Errorf("%q: se esperaba error", malo)
		}
	}
}
