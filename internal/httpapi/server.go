// Package httpapi exposes the transport-agnostic application.Service over HTTP:
// a JSON REST surface, a Server-Sent Events stream fed by the apphost event hub,
// and static serving of the embedded SPA. It is a thin adapter (Guardrail G2) —
// all business logic stays in application.Service. Handlers reuse the existing
// application DTOs verbatim.
package httpapi

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"

	"GoMental/internal/agentapi/mcp"
	"GoMental/internal/apphost"
	"GoMental/internal/auth"
	"GoMental/internal/serverconfig"
)

// maxRequestBody caps request bodies to accommodate the largest legitimate
// payload (a 25 MB image asset, base64-encoded ~33 MB) plus headroom. Handlers
// that expect smaller bodies enforce tighter caps of their own.
const maxRequestBody int64 = 40 << 20 // 40 MiB

// Server is the HTTP adapter over a single apphost.Host.
type Server struct {
	host         *apphost.Host
	cfg          serverconfig.Config
	staticFS     fs.FS
	handler      http.Handler
	logger       *log.Logger
	auth         auth.Authenticator
	audit        *auth.AuditLog
	keyStore     *auth.APIKeyStore
	reqLimiter   *rateLimiter
	writeLimiter *rateLimiter
	mcpServer    *mcp.Server

	allowedOrigins map[string]bool
	tlsEnabled     bool
	metrics        *metrics

	// readOnly rejects content-mutating routes (git-viewer mode: git is the
	// source of truth). gitManager and gitWebhookSecret are non-nil/non-empty
	// only in git-viewer mode; see git.go.
	readOnly         bool
	gitManager       GitManager
	gitWebhookSecret string
}

// Options configures a Server. StaticFS is the embedded SPA bundle (already
// sub-rooted at the dist directory); it may be nil in tests. Auth defaults to
// trust-all when nil; Audit may be nil (auditing disabled).
type Options struct {
	Host     *apphost.Host
	Config   serverconfig.Config
	StaticFS fs.FS
	Logger   *log.Logger
	Auth     auth.Authenticator
	Audit    *auth.AuditLog
	// KeyStore enables API-key management endpoints when non-nil.
	KeyStore *auth.APIKeyStore
	// RequestRate/WriteRate are per-actor token-bucket rates (requests/sec);
	// zero uses generous defaults.
	RequestRate float64
	WriteRate   float64
	// AllowedOrigins is the CORS allow-list (exact origins); empty = same-origin.
	AllowedOrigins []string
	// TLSEnabled toggles HSTS (the actual TLS listener lives in ListenAndServe).
	TLSEnabled bool
	// ReadOnly rejects content-mutating routes (git-viewer mode).
	ReadOnly bool
	// GitManager, when non-nil, enables the /api/git/* endpoints and the git
	// fields on /api/info. GitWebhookSecret authenticates keyless sync calls.
	GitManager       GitManager
	GitWebhookSecret string
}

// NewServer builds the router and returns a ready Server.
func NewServer(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	authenticator := opts.Auth
	if authenticator == nil {
		authenticator = auth.NewTrustAll()
	}
	reqRate := opts.RequestRate
	if reqRate <= 0 {
		reqRate = 50 // generous default; humans never hit it, runaway agents do
	}
	writeRate := opts.WriteRate
	if writeRate <= 0 {
		writeRate = 10
	}
	origins := map[string]bool{}
	for _, o := range opts.AllowedOrigins {
		origins[o] = true
	}
	s := &Server{
		host:             opts.Host,
		cfg:              opts.Config,
		staticFS:         opts.StaticFS,
		logger:           logger,
		auth:             authenticator,
		audit:            opts.Audit,
		keyStore:         opts.KeyStore,
		reqLimiter:       newRateLimiter(reqRate, reqRate*4),
		writeLimiter:     newRateLimiter(writeRate, writeRate*4),
		allowedOrigins:   origins,
		tlsEnabled:       opts.TLSEnabled,
		metrics:          &metrics{},
		readOnly:         opts.ReadOnly,
		gitManager:       opts.GitManager,
		gitWebhookSecret: opts.GitWebhookSecret,
	}
	// The MCP endpoint is a thin adapter over the same Service the REST handlers
	// use (Guardrail G2/G3); it is only mounted when a Host is present.
	if opts.Host != nil {
		s.mcpServer = mcp.NewServer(opts.Host.Service())
	}
	s.handler = s.buildHandler()
	return s
}

// Handler returns the fully-wrapped http.Handler for the API + SPA.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// Note IDs may contain slashes, so note-scoped routes use the {id...}
	// trailing wildcard and sub-resources (backlinks/neighborhood/assets) get
	// their own path prefixes to stay unambiguous.
	// Coarse role gates are keyed to the endpoint map: viewer = read/search/graph,
	// editor = create/edit/delete/import/assets, admin = rebuild/workspace-open.
	// Under trust-all the actor is admin so every gate passes; the structure is
	// live so a real authenticator (API keys / OIDC) enforces immediately.
	mux.HandleFunc("POST /api/workspace/open", s.gate(auth.RoleAdmin, s.handleOpenWorkspace))
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	mux.HandleFunc("POST /api/notes", s.gate(auth.RoleEditor, s.contentWrite(s.handleCreateNote)))
	mux.HandleFunc("POST /api/keys", s.gate(auth.RoleAdmin, s.handleCreateKey))
	mux.HandleFunc("GET /api/keys", s.gate(auth.RoleAdmin, s.handleListKeys))
	mux.HandleFunc("DELETE /api/keys/{id}", s.gate(auth.RoleAdmin, s.handleRevokeKey))
	mux.HandleFunc("GET /api/recent", s.gate(auth.RoleViewer, s.handleRecent))
	mux.HandleFunc("GET /api/notes", s.gate(auth.RoleViewer, s.handleListNotes))
	mux.HandleFunc("POST /api/notes/move", s.gate(auth.RoleEditor, s.contentWrite(s.handleMoveNote)))
	mux.HandleFunc("GET /api/notes/{id...}", s.gate(auth.RoleViewer, s.handleReadNote))
	mux.HandleFunc("PUT /api/notes/{id...}", s.gate(auth.RoleEditor, s.contentWrite(s.handleSaveNote)))
	mux.HandleFunc("PUT /api/notes/favorite", s.gate(auth.RoleEditor, s.contentWrite(s.handleSetNoteFavorite)))
	mux.HandleFunc("DELETE /api/notes/{id...}", s.gate(auth.RoleEditor, s.contentWrite(s.handleDeleteNote)))
	mux.HandleFunc("POST /api/import", s.gate(auth.RoleEditor, s.contentWrite(s.handleImport)))
	mux.HandleFunc("POST /api/assets/{id...}", s.gate(auth.RoleEditor, s.contentWrite(s.handleSaveAsset)))
	mux.HandleFunc("GET /api/assets/{id...}", s.gate(auth.RoleViewer, s.handleLoadAsset))
	mux.HandleFunc("POST /api/search", s.gate(auth.RoleViewer, s.handleSearch))
	mux.HandleFunc("POST /api/graph", s.gate(auth.RoleViewer, s.handleGraph))
	mux.HandleFunc("POST /api/graph/query", s.gate(auth.RoleViewer, s.handleGraphQuery))
	mux.HandleFunc("GET /api/backlinks/{id...}", s.gate(auth.RoleViewer, s.handleBacklinks))
	mux.HandleFunc("GET /api/neighborhood/{id...}", s.gate(auth.RoleViewer, s.handleNeighborhood))
	mux.HandleFunc("GET /api/graph/layout", s.gate(auth.RoleViewer, s.handleLoadLayout))
	mux.HandleFunc("PUT /api/graph/layout", s.gate(auth.RoleEditor, s.handleSaveLayout))
	mux.HandleFunc("POST /api/rebuild", s.gate(auth.RoleAdmin, s.handleRebuild))
	mux.HandleFunc("GET /api/ui-state", s.gate(auth.RoleViewer, s.handleLoadUIState))
	mux.HandleFunc("PUT /api/ui-state", s.gate(auth.RoleViewer, s.handleSaveUIState))
	mux.HandleFunc("GET /api/settings", s.gate(auth.RoleViewer, s.handleLoadSettings))
	mux.HandleFunc("PUT /api/settings", s.gate(auth.RoleViewer, s.handleSaveSettings))
	mux.HandleFunc("GET /api/note-types", s.gate(auth.RoleViewer, s.handleListNoteTypes))
	mux.HandleFunc("POST /api/note-types/import", s.gate(auth.RoleEditor, s.contentWrite(s.handleImportNoteTypeCollection)))
	mux.HandleFunc("PUT /api/note-types/{id}", s.gate(auth.RoleEditor, s.contentWrite(s.handleSaveNoteType)))
	mux.HandleFunc("DELETE /api/note-types/{id}", s.gate(auth.RoleEditor, s.contentWrite(s.handleDeleteNoteType)))
	s.registerGitRoutes(mux)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/healthz", s.handleHealth)
	mux.HandleFunc("GET /api/readyz", s.handleReady)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)

	// MCP over HTTP for remote coding agents. Gated at viewer to connect/read;
	// write tools self-enforce editor inside the handler. GET declines the
	// optional server->client stream (no server-initiated messages here).
	mux.HandleFunc("POST /mcp", s.gate(auth.RoleViewer, s.handleMCP))
	mux.HandleFunc("GET /mcp", s.handleMCPStream)

	// Unmatched /api/* paths return a JSON 404 rather than the SPA shell.
	mux.HandleFunc("/api/", s.handleAPINotFound)

	// Everything else is the SPA (with client-route fallback to index.html).
	mux.Handle("/", s.spaHandler())

	// Order (outer→inner): metrics → recover → security headers → CORS →
	// body limit → authenticate → rate-limit → routes.
	return s.observe(s.recoverer(s.securityHeaders(s.cors(s.bodyLimit(s.authMiddleware(s.rateLimit(mux)))))))
}

// recoverer converts a panic in any handler into a 500 rather than crashing the
// server process, and logs it.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Printf("httpapi panic on %s %s: %v", r.Method, r.URL.Path, rec)
				writeErrorStatus(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// bodyLimit caps request body size for methods that carry a body. SSE and other
// GET requests are untouched.
func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeErrorStatus(w, http.StatusNotFound, "api.not_found", "no such API endpoint: "+r.Method+" "+r.URL.Path)
}

// decodeJSON parses the request body into v, writing a 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "request.invalid_json", "could not parse request body: "+err.Error())
		return false
	}
	return true
}
