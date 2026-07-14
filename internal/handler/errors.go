package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
)

// NewErrorHandler returns an ogen error handler that logs errors before
// delegating to the default response encoding. Without it, errors returned
// from handlers (500) would never appear in the logs.
func NewErrorHandler(logger *slog.Logger) ogenerrors.ErrorHandler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
		var secErr *ogenerrors.SecurityError
		if errors.As(err, &secErr) {
			logger.DebugContext(ctx, "authentication failed", slog.String("path", r.URL.Path), slog.Any("err", err))
		} else {
			logger.ErrorContext(ctx, "request failed", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Any("err", err))
		}

		ogenerrors.DefaultErrorHandler(ctx, w, r, err)
	}
}
