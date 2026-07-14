package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/snapaddy/nx-cache-server/internal/api"
)

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// RequestLogger wraps srv and logs one line per handled request.
func RequestLogger(logger *slog.Logger, srv *api.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		srv.ServeHTTP(rec, r)

		var route string
		if rt, ok := srv.FindRoute(r.Method, r.URL.Path); ok {
			route = rt.PathPattern()
		}

		logger.InfoContext(r.Context(), "request handled",
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.String("remote_addr", r.RemoteAddr),
			slog.Int64("duration_us", time.Since(start).Microseconds()),
		)
	})
}
