// Package mcp exposes the GoMental wiki as Model Context Protocol tools so
// coding agents (Claude Code, Cursor, MCP Inspector, …) can search, read,
// create, and edit notes natively. It is a thin adapter over
// application.Service (Guardrail G2) and speaks the MCP stdio transport:
// newline-delimited JSON-RPC 2.0 on stdin/stdout.
//
// The protocol layer is intentionally dependency-free and small so the tool
// surface stays stable if it is later re-hosted on an SDK or the Streamable-HTTP
// transport (which can reuse the same Tool handlers).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"GoMental/internal/application"
)

// protocolVersion is the MCP revision this server advertises.
const protocolVersion = "2024-11-05"

// Server dispatches MCP JSON-RPC messages to tool handlers backed by a Service.
type Server struct {
	service *application.Service
	name    string
	version string
	tools   []Tool
	byName  map[string]Tool
	writeMu sync.Mutex
}

// NewServer builds an MCP server over the given service.
func NewServer(service *application.Service) *Server {
	s := &Server{service: service, name: "gomental", version: "1.0.0"}
	s.tools = s.buildTools()
	s.byName = make(map[string]Tool, len(s.tools))
	for _, t := range s.tools {
		s.byName[t.Name] = t
	}
	return s
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidReq     = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Run reads newline-delimited JSON-RPC messages from in and writes responses to
// out until in is exhausted or ctx is cancelled.
func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 40*1024*1024) // large note bodies and base64 image uploads (25 MB asset ≈ 34 MB encoded)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.write(out, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "parse error"}})
			continue
		}
		s.dispatch(ctx, out, req)
	}
	return scanner.Err()
}

func (s *Server) dispatch(ctx context.Context, out io.Writer, req rpcRequest) {
	isNotification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		s.reply(out, req, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		})
	case "notifications/initialized", "notifications/cancelled":
		// notifications: no response
	case "ping":
		s.reply(out, req, map[string]any{})
	case "tools/list":
		s.reply(out, req, map[string]any{"tools": s.tools})
	case "tools/call":
		s.handleToolCall(ctx, out, req)
	default:
		if !isNotification {
			s.replyError(out, req, codeMethodNotFound, "method not found: "+req.Method)
		}
	}
}

func (s *Server) handleToolCall(ctx context.Context, out io.Writer, req rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.replyError(out, req, codeInvalidParams, "invalid tool call params")
		return
	}
	tool, ok := s.byName[params.Name]
	if !ok {
		s.replyError(out, req, codeInvalidParams, "unknown tool: "+params.Name)
		return
	}
	text, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		// Tool-level errors are reported as MCP tool results with isError=true so
		// the agent can read the message, rather than as protocol errors.
		s.reply(out, req, toolResult(err.Error(), true))
		return
	}
	s.reply(out, req, toolResult(text, false))
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func (s *Server) reply(out io.Writer, req rpcRequest, result any) {
	if len(req.ID) == 0 {
		return // notification: no response
	}
	s.write(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (s *Server) replyError(out io.Writer, req rpcRequest, code int, message string) {
	if len(req.ID) == 0 {
		return
	}
	s.write(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: message}})
}

func (s *Server) write(out io.Writer, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		data, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: resp.ID, Error: &rpcError{Code: codeInternal, Message: "encode error"}})
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = out.Write(append(data, '\n'))
}

func jsonString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
