package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled, then
// gracefully shuts down. Read/idle timeouts are set defensively; the write
// timeout is intentionally 0 because the SSE endpoint holds connections open
// indefinitely (per-request deadlines guard the finite endpoints instead).
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		scheme := "http"
		if s.cfg.TLSEnabled() {
			scheme = "https"
		}
		s.logger.Printf("gomental serve listening on %s://%s (workspace %s)", scheme, s.cfg.Addr, s.cfg.WorkspaceRoot)
		var err error
		if s.cfg.TLSEnabled() {
			err = srv.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
