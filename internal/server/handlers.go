package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/snapaddy/nx-cache-server/internal/store"
)

// cacheHashPattern matches an acceptable cache hash. The character set is
// deliberately narrow: the hash becomes part of the storage object key, and
// net/http's ServeMux URL-decodes the path segment, so without this guard a
// value like "..%2F..%2Fsecret" would decode to "../../secret" and escape the
// configured storage prefix (path.Join collapses the "..") — reading or
// overwriting artifacts outside the intended prefix. Forbidding "/" and "."
// makes such traversal impossible.
var cacheHashPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// cacheHash reads and validates the "hash" path value. On an invalid hash it
// writes a 400 response and returns ok=false.
func cacheHash(w http.ResponseWriter, r *http.Request) (string, bool) {
	hash := r.PathValue("hash")
	if !cacheHashPattern.MatchString(hash) {
		returnError(w, "invalid cache hash", http.StatusBadRequest)

		return "", false
	}

	return hash, true
}

// handleGet downloads a cache artifact.
func handleGet(st store.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := cacheHash(w, r)
		if !ok {
			return
		}

		rc, size, err := st.Get(r.Context(), hash)

		switch {
		case errors.Is(err, store.ErrNotFound):
			logger.DebugContext(r.Context(), "cache miss", slog.String("hash", hash))
			w.WriteHeader(http.StatusNotFound)

			return
		case err != nil:
			logger.ErrorContext(r.Context(), "get from store", slog.String("hash", hash), slog.Any("err", err))
			returnError(w, "internal server error", http.StatusInternalServerError)

			return
		}
		defer rc.Close()

		logger.DebugContext(r.Context(), "cache hit", slog.String("hash", hash), slog.Int64("size", size))

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))

		if _, err := io.Copy(w, rc); err != nil {
			// The status line is already sent; all we can do is log.
			logger.ErrorContext(r.Context(), "stream artifact", slog.String("hash", hash), slog.Any("err", err))
		}
	})
}

// handlePut uploads a cache artifact. Existing artifacts are rejected with
// 409; read-only tokens never reach this handler (403 in middlewareAuthZ).
// maxUploadBytes caps the artifact size; a value <= 0 disables the limit.
func handlePut(st store.Store, logger *slog.Logger, maxUploadBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash, ok := cacheHash(w, r)
		if !ok {
			return
		}

		// The spec marks Content-Length as required; -1 means the header is
		// missing (chunked transfer encoding).
		if r.ContentLength < 0 {
			returnError(w, "Content-Length header is required", http.StatusBadRequest)

			return
		}

		if maxUploadBytes > 0 {
			if r.ContentLength > maxUploadBytes {
				returnError(w, "cache artifact exceeds the maximum upload size", http.StatusRequestEntityTooLarge)

				return
			}

			// Defense in depth against a body larger than its declared
			// Content-Length: MaxBytesReader caps what the store can read.
			r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		}

		err := st.Put(r.Context(), hash, r.ContentLength, r.Body)

		switch {
		case errors.Is(err, store.ErrAlreadyExists):
			logger.DebugContext(r.Context(), "cache entry already exists", slog.String("hash", hash))
			w.WriteHeader(http.StatusConflict)

			return
		case err != nil:
			logger.ErrorContext(r.Context(), "put to store", slog.String("hash", hash), slog.Any("err", err))
			returnError(w, "internal server error", http.StatusInternalServerError)

			return
		}

		logger.DebugContext(r.Context(), "cache entry stored", slog.String("hash", hash), slog.Int64("size", r.ContentLength))
		w.WriteHeader(http.StatusOK)
	})
}
