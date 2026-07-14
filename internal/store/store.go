// Package store defines the storage abstraction for cache artifacts.
package store

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrNotFound is returned by Get when the key does not exist.
	ErrNotFound = errors.New("key not found")

	// ErrAlreadyExists is returned by Put when the key already exists.
	ErrAlreadyExists = errors.New("key already exists")
)

type Store interface {
	// Get streams the value stored under key. The caller must close rc.
	// Returns an error wrapping ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (rc io.ReadCloser, size int64, err error)

	// Put streams size bytes from r into the store under key.
	// Returns an error wrapping ErrAlreadyExists if the key already exists.
	Put(ctx context.Context, key string, size int64, r io.Reader) error
}
