// Package handler implements the generated api.Handler and
// api.SecurityHandler interfaces on top of a store.Store.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/snapaddy/nx-cache-server/internal/api"
	"github.com/snapaddy/nx-cache-server/internal/store"
)

var (
	errMissingPermission    = errors.New("no permission in request context")
	errInvalidContentLength = errors.New("invalid content length")
)

// Handler implements api.Handler backed by a store.Store.
type Handler struct {
	store  store.Store
	logger *slog.Logger
}

var _ api.Handler = (*Handler)(nil)

func New(st store.Store, logger *slog.Logger) *Handler {
	return &Handler{store: st, logger: logger}
}

// Get downloads a cache artifact.
func (h *Handler) Get(ctx context.Context, params api.GetParams) (api.GetRes, error) {
	rc, size, err := h.store.Get(ctx, params.Hash)

	switch {
	case errors.Is(err, store.ErrNotFound):
		h.logger.DebugContext(ctx, "cache miss", slog.String("hash", params.Hash))

		return &api.GetNotFound{}, nil
	case err != nil:
		return nil, fmt.Errorf("get %q from store: %w", params.Hash, err)
	}

	h.logger.DebugContext(ctx, "cache hit", slog.String("hash", params.Hash), slog.Int64("size", size))

	return &api.GetOK{Data: rc}, nil
}

// Put uploads a cache artifact. Read-only tokens are rejected with 403,
// existing artifacts with 409.
func (h *Handler) Put(ctx context.Context, req api.PutReq, params api.PutParams) (api.PutRes, error) {
	perm, ok := permissionFromContext(ctx)
	if !ok {
		return nil, errMissingPermission
	}

	if perm == permissionReadOnly {
		h.logger.DebugContext(ctx, "write denied for read-only token", slog.String("hash", params.Hash))

		return &api.PutForbidden{Data: strings.NewReader("read-only token cannot upload cache artifacts")}, nil
	}

	// net/http validates the Content-Length header before ogen parses it,
	// so this only guards against out-of-range values.
	if params.ContentLength < 0 || params.ContentLength != math.Trunc(params.ContentLength) {
		return nil, fmt.Errorf("%w: %v", errInvalidContentLength, params.ContentLength)
	}

	size := int64(params.ContentLength)

	err := h.store.Put(ctx, params.Hash, size, req.Data)

	switch {
	case errors.Is(err, store.ErrAlreadyExists):
		h.logger.DebugContext(ctx, "cache entry already exists", slog.String("hash", params.Hash))

		return &api.PutConflict{}, nil
	case err != nil:
		return nil, fmt.Errorf("put %q to store: %w", params.Hash, err)
	}

	h.logger.DebugContext(ctx, "cache entry stored", slog.String("hash", params.Hash), slog.Int64("size", size))

	return &api.PutOK{}, nil
}
