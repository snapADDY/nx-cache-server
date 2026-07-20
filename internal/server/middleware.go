package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// permission is the access level granted to an authenticated request.
type permission uint64

const (
	permRead permission = 1 << iota
	permWrite
)

type permissionCtxKey struct{}

func permissionFromContext(ctx context.Context) (permission, bool) {
	perm, ok := ctx.Value(permissionCtxKey{}).(permission)

	return perm, ok
}

// statusRecorder is a custom ResponseWriter that captures the status code
// so it can be logged later. It wraps the standard http.ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// Implement the WriteHeader method to capture the status code.
func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// ReadFrom forwards to the underlying ResponseWriter's io.ReaderFrom when it
// has one, preserving net/http's efficient streaming path (e.g. sendfile) for
// large cache artifacts instead of falling back to buffered copies. Wrapping
// the ResponseWriter would otherwise hide that capability from io.Copy.
//
// The error is returned unwrapped because an io.ReaderFrom must expose the
// concrete error type to io.Copy and net/http's streaming path.
//
//nolint:wrapcheck // io.ReaderFrom must return the underlying error unwrapped
func (sr *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := sr.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}

	return io.Copy(sr.ResponseWriter, src)
}

// middlewareLogging logs all incoming requests with their method, path, IP and duration.
func middlewareLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{w, 200}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		logger.InfoContext(r.Context(), "request completed",
			"method", r.Method,
			"route", r.Pattern,
			"path", r.URL.Path,
			"args", r.URL.Query(),
			"remote_addr", r.RemoteAddr,
			"duration_us", duration.Microseconds(),
			"status", rec.status,
		)
	})
}

// middlewareAuthN authenticates the request's bearer token and stores the
// granted permission in the request context. Requests without an
// Authorization header pass through anonymously so unauthenticated routes
// like /health keep working; middlewareAuthZ rejects anonymous requests on
// routes that require a permission. Tokens are compared as SHA-256 hashes so
// the comparison is constant-time regardless of token length.
func middlewareAuthN(next http.Handler, rwToken, roToken string) http.Handler {
	rwHash := sha256.Sum256([]byte(rwToken))
	roHash := sha256.Sum256([]byte(roToken))
	hasReadOnly := roToken != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			next.ServeHTTP(w, r)

			return
		}

		const prefix = "Bearer "
		if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
			returnError(w, "invalid authorization header", http.StatusUnauthorized)

			return
		}

		sum := sha256.Sum256([]byte(auth[len(prefix):]))

		// Evaluate both comparisons before branching so a timing side channel
		// cannot distinguish read-write from read-only tokens.
		isReadWrite := subtle.ConstantTimeCompare(sum[:], rwHash[:]) == 1
		isReadOnly := hasReadOnly && subtle.ConstantTimeCompare(sum[:], roHash[:]) == 1

		var perm permission

		switch {
		case isReadWrite:
			perm = permRead | permWrite
		case isReadOnly:
			perm = permRead
		default:
			returnError(w, "invalid token", http.StatusUnauthorized)

			return
		}

		r2 := r.WithContext(context.WithValue(r.Context(), permissionCtxKey{}, perm))
		next.ServeHTTP(w, r2)

		// The mux sets the route pattern on the copy; propagate it to the
		// original request so the logging middleware outside can read it.
		r.Pattern = r2.Pattern
	})
}

// middlewareAuthZ guards routes that require a specific permission. It reads the granted permission
// from the request context, which is set by middlewareAuthN. If the permission is missing or
// insufficient, it responds with 401 or 403 respectively.
func middlewareAuthZ(next http.Handler, perm permission) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := permissionFromContext(r.Context())
		if !ok {
			returnError(w, "missing bearer token", http.StatusUnauthorized)

			return
		}

		if p&perm != perm {
			returnError(w, "insufficient permission", http.StatusForbidden)

			return
		}

		next.ServeHTTP(w, r)
	})
}
