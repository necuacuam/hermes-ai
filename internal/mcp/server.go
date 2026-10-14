package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
)

// ToolHandler ejecuta una herramienta ya con los argumentos deserializados
// (cada handler decide su propio tipo de argumentos internamente).
type ToolHandler func(rawArgs json.RawMessage) CallToolResult

type registeredTool struct {
	def     Tool
	handler ToolHandler
}

// Server es un servidor MCP minimo sobre stdio: sin resources, sin prompts,
// sin sampling -- solo tools, que es todo lo que este proyecto necesita.
type Server struct {
	name    string
	version string
	tools   map[string]registeredTool
	order   []string // para listar tools en un orden estable
	logger  *log.Logger
}

func NewServer(name, version string, logger *log.Logger) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   map[string]registeredTool{},
		logger:  logger,
	}
}

// RegisterTool suma una tool al servidor. inputSchema debe ser un JSON
// Schema valido (como []byte ya serializado) describiendo los argumentos
// esperados -- Claude lo usa para saber que mandar.
func (s *Server) RegisterTool(name, description string, inputSchema json.RawMessage, handler ToolHandler) {
	s.tools[name] = registeredTool{
		def:     Tool{Name: name, Description: description, InputSchema: inputSchema},
		handler: handler,
	}
	s.order = append(s.order, name)
}

// Serve corre el loop principal: lee un mensaje JSON por linea de r, escribe
// la respuesta (si corresponde) como una linea de JSON a w. Bloquea hasta
// que r se cierra (stdin cerrado = Claude Desktop terminó la sesión).
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // hasta 8MB por mensaje
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.logf("mensaje no parseable, se ignora: %v", err)
			continue
		}

		resp := s.handle(req)
		// Las notificaciones (sin ID) no llevan respuesta.
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("error escribiendo respuesta: %w", err)
		}
	}
	return scanner.Err()
}

// NOTA (2026-09-11): este servidor tenia tambien ListenAndServe/
// ServeListener para servir MCP por TCP en vez de stdio -- existian solo
// para el escenario de tunel SSH inverso (el paquete internal/bridge, ya
// borrado). Se sacaron junto con ese escenario: esta v1 corre siempre
// en una sola maquina, sobre stdio, que es lo unico que Claude Desktop
// necesita.

func (s *Server) handle(req Request) *Response {
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		result := InitializeResult{
			ProtocolVersion: "2025-06-18",
			Capabilities:    ServerCapabilities{Tools: &ToolsCapability{ListChanged: false}},
			ServerInfo:      ServerInfo{Name: s.name, Version: s.version},
			Instructions:    "Servidor MCP de entidades Hermes tipo cuenta (holding/grupo/cliente/centro/punto/direccion). Usar diccionario_de_campos y relacion_entre_entidades para armar el pipeline de 'reporting' -- nunca inventar un campo que no este documentado ahi. modo='conteo' devuelve un numero (nunca archivo); modo='listado' devuelve filas en pantalla (recortadas, con aviso explicito si se recorto) y/o un archivo local completo.",
		}
		return s.ok(req, result)

	case "notifications/initialized":
		return nil // notificacion, no responde

	case "ping":
		return s.ok(req, map[string]interface{}{})

	case "tools/list":
		tools := make([]Tool, 0, len(s.order))
		for _, name := range s.order {
			tools = append(tools, s.tools[name].def)
		}
		return s.ok(req, ListToolsResult{Tools: tools})

	case "tools/call":
		if isNotification {
			return nil
		}
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errResp(req, -32602, "params invalidos: "+err.Error())
		}
		tool, found := s.tools[params.Name]
		if !found {
			return s.ok(req, ErrorResult("tool desconocida: "+params.Name))
		}
		result := tool.handler(params.Arguments)
		return s.ok(req, result)

	default:
		if isNotification {
			return nil
		}
		return s.errResp(req, -32601, "metodo no soportado: "+req.Method)
	}
}

func (s *Server) ok(req Request, result interface{}) *Response {
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) errResp(req Request, code int, msg string) *Response {
	return &Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: code, Message: msg}}
}

func (s *Server) logf(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}
