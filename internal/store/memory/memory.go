// Package memory provides an in-memory Store implementation,
// used in tests and suitable for local development.
package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/snapaddy/nx-cache-server/internal/store"
)

var errSizeMismatch = errors.New("size mismatch")

// Store is a concurrency-safe in-memory store.Store implementation.
type Store struct {
	objects map[string][]byte
	mu      sync.RWMutex
}

var _ store.Store = (*Store)(nil)

func New() *Store {
	return &Store{objects: make(map[string][]byte)}
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.objects[key]
	if !ok {
		return nil, 0, fmt.Errorf("memory get %q: %w", key, store.ErrNotFound)
	}

	return io.NopCloser(bytes.NewReader(value)), int64(len(value)), nil
}

func (s *Store) Put(_ context.Context, key string, size int64, r io.Reader) error {
	value, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("memory put %q: read value: %w", key, err)
	}

	if int64(len(value)) != size {
		return fmt.Errorf("memory put %q: %w: got %d bytes, expected %d", key, errSizeMismatch, len(value), size)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.objects[key]; ok {
		return fmt.Errorf("memory put %q: %w", key, store.ErrAlreadyExists)
	}

	s.objects[key] = value

	return nil
}
