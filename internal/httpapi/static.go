package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded single-page app. Requests that map to a real
// file are served as-is; everything else falls back to index.html so client-side
// routes work on refresh/deep-link. If no static FS was provided (e.g. tests),
// it returns a small placeholder for "/" and 404 otherwise.
func (s *Server) spaHandler() http.Handler {
	if s.staticFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte("<!doctype html><title>GoMental</title><p>GoMental server is running. No SPA bundle was embedded in this build.</p>"))
				return
			}
			http.NotFound(w, r)
		})
	}

	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if reqPath == "" {
			reqPath = "index.html"
		}
		if _, err := fs.Stat(s.staticFS, reqPath); err != nil {
			// Not a real asset: serve the SPA shell for client-side routing.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			serveIndex(w, r, s.staticFS)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
