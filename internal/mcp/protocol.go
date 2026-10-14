// Package mcp implementa el subconjunto minimo del protocolo MCP (Model
// Context Protocol) necesario para correr un servidor de herramientas sobre
// stdio y que Claude Desktop pueda conectarse a el.
//
// Decision de diseno: no se usa el SDK oficial (modelcontextprotocol/go-sdk)
// para evitar una dependencia externa. El protocolo sobre stdio es JSON-RPC
// 2.0 con mensajes delimitados por salto de linea -- lo suficientemente
// simple como para implementarlo directo y mantener el binario auditable,
// sin depender de que la maquina donde se compila tenga acceso a un proxy
// de modulos de Go (relevante en un equipo corporativo con red
// restringida).
// Si mas adelante se prefiere el SDK oficial, este paquete se puede
// reemplazar sin tocar el resto del codigo (los tools no conocen el
// transporte).
package mcp

import "encoding/json"

// JSON-RPC 2.0 basico.

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tipos del protocolo MCP relevantes para un servidor de solo-tools (sin
// resources ni prompts, que no hacen falta para este caso de uso).

type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    ServerCapabilities     `json:"capabilities"`
	ServerInfo      ServerInfo             `json:"serverInfo"`
	Instructions    string                 `json:"instructions,omitempty"`
	Meta            map[string]interface{} `json:"_meta,omitempty"`
}

type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool es la definicion de una herramienta expuesta por el MCP -- el nombre
// y la descripcion son lo que el agente conversacional (Claude) lee para
// decidir cuando llamarla, asi que conviene que sean precisos.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TextResult es un helper para armar la respuesta mas comun: un unico
// bloque de texto (en nuestro caso, casi siempre JSON serializado a texto).
func TextResult(text string) CallToolResult {
	return CallToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func ErrorResult(msg string) CallToolResult {
	return CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}
}
