package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"GoMental/internal/auth"
)

// The /mcp endpoint hosts the same MCP tool surface as the local `gomental mcp`
// stdio server, but over HTTP so remote/team coding agents can reach the central
// server's single Service (Guardrail G3) instead of opening the workspace on
// disk. It speaks the MCP Streamable-HTTP transport in stateless mode: the agent
// POSTs JSON-RPC messages and reads a JSON-RPC response; there is no
// server-initiated stream (GET returns 405) and no session id, because every
// tool call is synchronous and backed by the shared Service.
//
// Authorization reuses the API-key auth already wired for /api: the route is
// gated at viewer (enough to connect, list tools, search, and read), and the
// individual mutating tools (create_note, edit_note) additionally require the
// editor role. Under trust-all the actor is admin so both pass; with enforced
// keys a viewer key can read but not write, exactly as over REST.

// JSON-RPC error codes used by the HTTP transport. mcpErrRole is in the
// implementation-defined server range (-32000..-32099); mcpParseError is the
// standard JSON-RPC parse-error code.
const (
	mcpErrRole    = -32001
	mcpParseError = -32700
)

// handleMCP dispatches a POSTed JSON-RPC message (or JSON-RPC batch array) to the
// MCP tool server and writes the response. Notifications yield 202 with no body.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.mcpServer == nil {
		writeErrorStatus(w, http.StatusServiceUnavailable, "mcp.unavailable", "mcp endpoint is not configured")
		return
	}
	actor := actorFrom(r.Context())
	// The general per-actor request limiter also guards /mcp (the rateLimit
	// middleware only covers /api/*). Write tools consume the stricter write
	// budget too, enforced per message in dispatchMCPMessage.
	if !s.reqLimiter.allow(actor.ID) {
		writeErrorStatus(w, http.StatusTooManyRequests, "rate.limited", "request rate limit exceeded")
		return
	}

	// Body size is already capped by the bodyLimit middleware (maxRequestBody).
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "mcp.read_failed", "could not read request body")
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeErrorStatus(w, http.StatusBadRequest, "mcp.empty", "empty JSON-RPC message")
		return
	}

	// JSON-RPC permits a batch (array) or a single message object.
	if trimmed[0] == '[' {
		var msgs []json.RawMessage
		if err := json.Unmarshal(trimmed, &msgs); err != nil {
			writeMCPJSON(w, mcpErrorResponse(nil, mcpParseError, "parse error"))
			return
		}
		out := make([]json.RawMessage, 0, len(msgs))
		for _, m := range msgs {
			if resp, ok := s.dispatchMCPMessage(r, actor, m); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted) // batch of notifications only
			return
		}
		buf, _ := json.Marshal(out)
		writeMCPJSON(w, buf)
		return
	}

	resp, ok := s.dispatchMCPMessage(r, actor, trimmed)
	if !ok {
		w.WriteHeader(http.StatusAccepted) // notification: no response
		return
	}
	writeMCPJSON(w, resp)
}

// dispatchMCPMessage authorizes and rate-limits a single message, dispatches it
// to the tool server, and audits write tool calls. It returns the response bytes
// and whether a response exists (false for notifications).
func (s *Server) dispatchMCPMessage(r *http.Request, actor auth.Actor, raw []byte) ([]byte, bool) {
	call, isCall := parseMCPToolCall(raw)
	if isCall && s.mcpServer.IsWriteTool(call.Params.Name) {
		if s.readOnly {
			return mcpErrorResponse(call.ID, mcpErrRole, "workspace is read-only: content is managed in git (tool "+call.Params.Name+" rejected)"), true
		}
		if !actor.Can(auth.RoleEditor) {
			return mcpErrorResponse(call.ID, mcpErrRole, "insufficient role: tool "+call.Params.Name+" requires the 'editor' role"), true
		}
		if !s.writeLimiter.allow(actor.ID) {
			return mcpErrorResponse(call.ID, mcpErrRole, "write rate limit exceeded"), true
		}
	}

	resp, has := s.mcpServer.ServeMessage(r.Context(), raw)

	if isCall && s.mcpServer.IsWriteTool(call.Params.Name) {
		s.auditMCPWrite(r, call, resp)
	}
	return resp, has
}

// mcpToolCall is the minimal JSON-RPC envelope the HTTP layer needs to make
// authorization and audit decisions without re-implementing tool dispatch.
type mcpToolCall struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name      string `json:"name"`
		Arguments struct {
			ID string `json:"id"`
		} `json:"arguments"`
	} `json:"params"`
}

// parseMCPToolCall returns the envelope and whether the message is a tools/call.
func parseMCPToolCall(raw []byte) (mcpToolCall, bool) {
	var c mcpToolCall
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, false
	}
	return c, c.Method == "tools/call"
}

// auditMCPWrite records a write tool call in the audit log (no-op if unconfigured),
// mirroring the attribution the REST write handlers record. The result is derived
// from whether the tool reported an error in its content payload.
func (s *Server) auditMCPWrite(r *http.Request, call mcpToolCall, resp []byte) {
	if s.audit == nil {
		return
	}
	result := "ok"
	if mcpResponseIsError(resp) {
		result = "error"
	}
	s.recordAudit(r, "mcp."+call.Params.Name, call.Params.Arguments.ID, "", result, "")
}

// mcpResponseIsError reports whether a tools/call response carried isError=true.
func mcpResponseIsError(resp []byte) bool {
	var env struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(resp, &env); err != nil {
		return true
	}
	return env.Result.IsError || len(env.Error) > 0
}

// mcpErrorResponse builds a marshaled JSON-RPC error response for the given id.
func mcpErrorResponse(id json.RawMessage, code int, message string) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	return b
}

func writeMCPJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleMCPStream answers the Streamable-HTTP GET request. This server has no
// server-initiated messages, so per the transport spec it declines the stream
// with 405 rather than holding an empty SSE connection open.
func (s *Server) handleMCPStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	writeErrorStatus(w, http.StatusMethodNotAllowed, "mcp.no_stream", "this MCP endpoint has no server-initiated stream; POST JSON-RPC messages instead")
}
