package mcp

import (
	"bytes"
	"context"
	"encoding/json"
)

// writeTools names the tools that mutate the workspace. Network transports use
// it (via IsWriteTool) to require a stronger role before dispatch; the stdio
// transport is local and unauthenticated so it does not consult this set. Keep
// it in sync with buildTools — a tool that writes must be listed here.
var writeTools = map[string]bool{
	"create_note":  true,
	"edit_note":    true,
	"upload_asset": true,
}

// IsWriteTool reports whether the named tool mutates the workspace. It lets an
// authenticating transport (the HTTP endpoint) gate write tools to an editor
// role while leaving read tools open to viewers, without the tool layer itself
// having to know about auth (Guardrail G2).
func (s *Server) IsWriteTool(name string) bool { return writeTools[name] }

// ServeMessage dispatches a single JSON-RPC message delivered over a
// request/response transport (the HTTP endpoint) and returns the marshaled
// response. hasResponse is false for notifications, which carry no id and get no
// reply — the caller should answer 202 with an empty body in that case.
//
// It reuses the exact same dispatch path as the stdio transport (Run), so the
// tool surface, protocol version, and error semantics are identical regardless
// of how the agent connects. The per-message bytes.Buffer keeps concurrent HTTP
// calls independent; tool handlers run without holding any lock, so parallel
// agent requests are served concurrently.
func (s *Server) ServeMessage(ctx context.Context, raw []byte) (response []byte, hasResponse bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "parse error"}})
		return b, true
	}
	var buf bytes.Buffer
	s.dispatch(ctx, &buf, req)
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if len(out) == 0 {
		return nil, false // notification: no response
	}
	return out, true
}
