package httpapi

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// metrics holds lightweight process counters exposed at /api/metrics.
type metrics struct {
	requests    atomic.Int64
	responses   [6]atomic.Int64 // index by status/100 (0..5); 0 unused
	rateLimited atomic.Int64
}

func (m *metrics) observe(status int) {
	m.requests.Add(1)
	class := status / 100
	if class < 0 || class >= len(m.responses) {
		class = 0
	}
	m.responses[class].Add(1)
	if status == http.StatusTooManyRequests {
		m.rateLimited.Add(1)
	}
}

// responseRecorder captures the status code written by downstream handlers.
type responseRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *responseRecorder) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush proxies to the underlying flusher so SSE keeps working through the wrapper.
func (w *responseRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// observe wraps the chain to record per-response metrics.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.metrics.observe(rec.status)
	})
}

// cors applies a configurable CORS policy for the browser SPA. With no allowed
// origins it is a no-op (same-origin only). Preflight OPTIONS is answered here.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.allowedOrigins[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, If-Match, X-API-Key")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds baseline hardening headers, including HSTS when TLS is on.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		if s.tlsEnabled {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// handleReady reports readiness: 200 once a workspace is open, else 503.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !s.service().IsWorkspaceOpen() {
		writeErrorStatus(w, http.StatusServiceUnavailable, "not_ready", "workspace not open")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleMetrics renders a minimal Prometheus-style text exposition.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m := s.metrics
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP gomental_http_requests_total Total HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE gomental_http_requests_total counter\n")
	fmt.Fprintf(w, "gomental_http_requests_total %d\n", m.requests.Load())
	for class := 1; class <= 5; class++ {
		fmt.Fprintf(w, "gomental_http_responses_total{class=\"%dxx\"} %d\n", class, m.responses[class].Load())
	}
	fmt.Fprintf(w, "gomental_http_rate_limited_total %d\n", m.rateLimited.Load())
}
