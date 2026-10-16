// Package onedrive guarda los archivos de reporting en el OneDrive del
// usuario que pregunta, en la carpeta de la aplicacion
// (Aplicaciones/<nombre de la app>), en su nombre:
//
//  1. Cambia el token del usuario (emitido para la API de hermes-mcp) por un
//     token de Microsoft Graph con el flujo On-Behalf-Of (MSAL).
//  2. Sube el archivo con una sesion de carga de Graph, por partes, para no
//     tenerlo entero en memoria.
//
// El permiso que se pide es Files.ReadWrite.AppFolder: solo la carpeta de la
// aplicacion, no el resto de los archivos del usuario.
package onedrive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
)

// scopeGraph es el unico permiso que se pide a Graph en nombre del usuario.
const scopeGraph = "https://graph.microsoft.com/Files.ReadWrite.AppFolder"

// tamanoParte es el tamaño de cada parte de la subida: tiene que ser
// multiplo de 320 KiB (exigencia de Graph). 10 x 320 KiB = 3,125 MiB.
const tamanoParte = 10 * 320 * 1024

// ErrSinToken indica que la llamada no trae el token del usuario: sin el,
// no se puede escribir en su OneDrive.
var ErrSinToken = errors.New("la llamada no trae la identidad del usuario: no se puede guardar en su OneDrive")

// Intercambiador cambia el token del usuario por uno de Graph (OBO).
type Intercambiador interface {
	TokenGraph(ctx context.Context, tokenUsuario string) (string, error)
}

// Subidor guarda archivos en el OneDrive del usuario.
type Subidor struct {
	obo       Intercambiador
	cliente   *http.Client
	graphBase string // https://graph.microsoft.com/v1.0 (otro valor solo en tests)
}

// Config son los datos del registro de aplicacion de hermes-mcp en Entra ID.
type Config struct {
	ClientID string
	Secreto  string
	// TenantsPermitidos son los tenants (claim tid) cuyos usuarios pueden
	// guardar archivos. Vacio: cualquiera. Las cuentas personales de
	// Microsoft tienen el tenant 9188040d-6c67-4c5b-b112-36a304b66dad.
	TenantsPermitidos []string
}

// New arma el Subidor con el intercambio OBO de MSAL.
func New(cfg Config) (*Subidor, error) {
	cred, err := confidential.NewCredFromSecret(cfg.Secreto)
	if err != nil {
		return nil, fmt.Errorf("onedrive: credencial invalida: %w", err)
	}
	permitidos := map[string]bool{}
	for _, t := range cfg.TenantsPermitidos {
		if t = strings.TrimSpace(t); t != "" {
			permitidos[strings.ToLower(t)] = true
		}
	}
	obo := &oboMSAL{clientID: cfg.ClientID, cred: cred, permitidos: permitidos, clientes: map[string]confidential.Client{}}
	return NewConIntercambiador(obo, http.DefaultClient, "https://graph.microsoft.com/v1.0"), nil
}

// NewConIntercambiador permite inyectar el intercambio y el destino de Graph
// (para tests).
func NewConIntercambiador(obo Intercambiador, cliente *http.Client, graphBase string) *Subidor {
	return &Subidor{obo: obo, cliente: cliente, graphBase: strings.TrimRight(graphBase, "/")}
}

// oboMSAL hace el intercambio contra el tenant que emitio el token del
// usuario (claim tid): asi funciona tanto con cuentas de trabajo como con
// cuentas personales. Entra ID valida el token en el intercambio; aqui solo
// se lee tid para elegir el tenant y comprobar que este permitido.
type oboMSAL struct {
	clientID   string
	cred       confidential.Credential
	permitidos map[string]bool

	mu       sync.Mutex
	clientes map[string]confidential.Client // uno por tenant, para reusar su cache
}

func (o *oboMSAL) TokenGraph(ctx context.Context, tokenUsuario string) (string, error) {
	tenant, err := TenantDeToken(tokenUsuario)
	if err != nil {
		return "", err
	}
	if len(o.permitidos) > 0 && !o.permitidos[tenant] {
		return "", fmt.Errorf("onedrive: el tenant del usuario (%s) no esta entre los permitidos", tenant)
	}
	cca, err := o.cliente(tenant)
	if err != nil {
		return "", err
	}
	res, err := cca.AcquireTokenOnBehalfOf(ctx, tokenUsuario, []string{scopeGraph})
	if err != nil {
		return "", fmt.Errorf("intercambio On-Behalf-Of: %w", err)
	}
	return res.AccessToken, nil
}

func (o *oboMSAL) cliente(tenant string) (confidential.Client, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if c, ok := o.clientes[tenant]; ok {
		return c, nil
	}
	c, err := confidential.New("https://login.microsoftonline.com/"+tenant, o.clientID, o.cred)
	if err != nil {
		return confidential.Client{}, fmt.Errorf("onedrive: cliente de Entra ID: %w", err)
	}
	o.clientes[tenant] = c
	return c, nil
}

// TenantDeToken lee el claim tid de un JWT sin validarlo (lo valida Entra ID
// en el intercambio). Devuelve error si no es un JWT o no trae tid.
func TenantDeToken(token string) (string, error) {
	partes := strings.Split(token, ".")
	if len(partes) != 3 {
		return "", errors.New("onedrive: el token del usuario no es un JWT")
	}
	carga, err := base64.RawURLEncoding.DecodeString(partes[1])
	if err != nil {
		return "", errors.New("onedrive: no se puede leer el token del usuario")
	}
	var claims struct {
		Tid string `json:"tid"`
	}
	if err := json.Unmarshal(carga, &claims); err != nil || claims.Tid == "" {
		return "", errors.New("onedrive: el token del usuario no trae tenant (tid)")
	}
	return strings.ToLower(claims.Tid), nil
}

// Subir guarda el archivo local rutaLocal en <carpeta>/<nombre> dentro de la
// carpeta de la aplicacion del OneDrive del usuario. Si ya existe un archivo
// con ese nombre, Graph le agrega un sufijo. Devuelve el enlace web al
// archivo, que solo puede abrir el usuario.
func (s *Subidor) Subir(ctx context.Context, tokenUsuario, carpeta, nombre, rutaLocal string) (string, error) {
	if tokenUsuario == "" {
		return "", ErrSinToken
	}
	tokenGraph, err := s.obo.TokenGraph(ctx, tokenUsuario)
	if err != nil {
		return "", err
	}
	f, err := os.Open(rutaLocal)
	if err != nil {
		return "", fmt.Errorf("onedrive: abriendo el archivo a subir: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("onedrive: %w", err)
	}

	uploadURL, err := s.crearSesion(ctx, tokenGraph, path.Join(carpeta, nombre))
	if err != nil {
		return "", err
	}
	return s.subirPartes(ctx, uploadURL, f, info.Size())
}

func (s *Subidor) crearSesion(ctx context.Context, tokenGraph, destino string) (string, error) {
	partes := strings.Split(destino, "/")
	for i, p := range partes {
		partes[i] = url.PathEscape(p)
	}
	endpoint := s.graphBase + "/me/drive/special/approot:/" + strings.Join(partes, "/") + ":/createUploadSession"
	cuerpo := []byte(`{"item":{"@microsoft.graph.conflictBehavior":"rename"}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(cuerpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tokenGraph)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cliente.Do(req)
	if err != nil {
		return "", fmt.Errorf("onedrive: creando la sesion de carga: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errorGraph("creando la sesion de carga", resp)
	}
	var sesion struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sesion); err != nil || sesion.UploadURL == "" {
		return "", fmt.Errorf("onedrive: respuesta de la sesion de carga sin uploadUrl")
	}
	return sesion.UploadURL, nil
}

// subirPartes envia el archivo en partes a la URL de la sesion. Esa URL ya
// esta autorizada: no lleva el token (Graph lo rechaza si se envia).
func (s *Subidor) subirPartes(ctx context.Context, uploadURL string, r io.Reader, total int64) (string, error) {
	buf := make([]byte, tamanoParte)
	var enviado int64
	for enviado < total {
		n, err := io.ReadFull(r, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("onedrive: leyendo el archivo: %w", err)
		}
		if n == 0 {
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(buf[:n]))
		if err != nil {
			return "", err
		}
		req.ContentLength = int64(n)
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", enviado, enviado+int64(n)-1, total))
		resp, err := s.cliente.Do(req)
		if err != nil {
			return "", fmt.Errorf("onedrive: subiendo una parte: %w", err)
		}
		enviado += int64(n)
		switch {
		case resp.StatusCode == http.StatusAccepted:
			resp.Body.Close() // faltan partes
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
			var item struct {
				WebURL string `json:"webUrl"`
			}
			err := json.NewDecoder(resp.Body).Decode(&item)
			resp.Body.Close()
			if err != nil || item.WebURL == "" {
				return "", fmt.Errorf("onedrive: la subida termino pero la respuesta no trae el enlace")
			}
			return item.WebURL, nil
		default:
			e := errorGraph("subiendo una parte", resp)
			resp.Body.Close()
			return "", e
		}
	}
	return "", fmt.Errorf("onedrive: la subida termino sin confirmacion de Graph")
}

// errorGraph arma un error con el codigo y el mensaje que devuelve Graph.
func errorGraph(paso string, resp *http.Response) error {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&e)
	if e.Error.Code != "" {
		return fmt.Errorf("onedrive: %s: HTTP %d %s: %s", paso, resp.StatusCode, e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("onedrive: %s: HTTP %d", paso, resp.StatusCode)
}

// Guardar es Subir con el token del usuario tomado del contexto de la
// llamada (ver ConToken).
func (s *Subidor) Guardar(ctx context.Context, carpeta, nombre, rutaLocal string) (string, error) {
	return s.Subir(ctx, TokenDe(ctx), carpeta, nombre, rutaLocal)
}

type claveToken struct{}

// ConToken guarda el token del usuario en el contexto de la llamada.
func ConToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, claveToken{}, token)
}

// TokenDe devuelve el token del usuario guardado con ConToken, o "".
func TokenDe(ctx context.Context) string {
	t, _ := ctx.Value(claveToken{}).(string)
	return t
}

// TokenDeCabecera extrae el token de una cabecera "Authorization: Bearer ...".
func TokenDeCabecera(cabecera string) string {
	esquema, token, ok := strings.Cut(cabecera, " ")
	if !ok || !strings.EqualFold(esquema, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
