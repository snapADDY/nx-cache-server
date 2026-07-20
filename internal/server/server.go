// Package server implements the Nx custom remote cache API on top of a
// store.Store using net/http.
package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/snapaddy/nx-cache-server/internal/store"
)

// authHandle wraps the given handler in middlewareAuthZ and registers it on the mux.
func authHandle(mux *http.ServeMux, pattern string, permission permission, handler http.Handler) {
	mux.Handle(pattern, middlewareAuthZ(handler, permission))
}

// NewServerHandler returns a http.Handler that serves the API on the given store.
// rwToken grants read-write access, roToken optionally grants read-only access.
// maxUploadBytes caps the size of a single uploaded artifact; <= 0 disables the limit.
func NewServerHandler(store store.Store, rwToken, roToken string, maxUploadBytes int64) http.Handler {
	logger := slog.Default()

	mux := http.NewServeMux()

	mux.Handle("GET /health", handleHealthCheck())

	authHandle(mux, "GET /v1/cache/{hash}", permRead, handleGet(store, logger))
	authHandle(mux, "PUT /v1/cache/{hash}", permWrite, handlePut(store, logger, maxUploadBytes))

	// Authentication runs on every route but only rejects invalid credentials;
	// routes that require a permission are wrapped in middlewareAuthZ above.
	// Logging is outermost so rejected requests are logged too.
	var handler http.Handler = mux
	handler = middlewareAuthN(handler, rwToken, roToken)
	handler = middlewareLogging(handler, logger)

	return handler
}

func handleHealthCheck() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}

// returnError writes an HTTP error response with content-type text/plain. The http.Error function
// can not be used here as it sets the content-type to "text/plain; charset=utf-8" which NX does
// not accept.
func returnError(w http.ResponseWriter, msg string, status int) {
	h := w.Header()
	// Copy the bahavior of http.Error
	h.Del("Content-Length")
	h.Set("Content-Type", "text/plain")

	w.WriteHeader(status)
	fmt.Fprintln(w, msg)
}
