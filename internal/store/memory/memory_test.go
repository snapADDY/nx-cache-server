package memory_test

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/snapaddy/nx-cache-server/internal/store"
	"github.com/snapaddy/nx-cache-server/internal/store/memory"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	s := memory.New()
	value := "cache artifact content"

	err := s.Put(t.Context(), "hash1", int64(len(value)), strings.NewReader(value))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, size, err := s.Get(t.Context(), "hash1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	if size != int64(len(value)) {
		t.Errorf("size = %d, want %d", size, len(value))
	}

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != value {
		t.Errorf("value = %q, want %q", got, value)
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()

	s := memory.New()

	_, _, err := s.Get(t.Context(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPutAlreadyExists(t *testing.T) {
	t.Parallel()

	s := memory.New()

	if err := s.Put(t.Context(), "hash1", 1, strings.NewReader("a")); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	err := s.Put(t.Context(), "hash1", 1, strings.NewReader("b"))
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestPutSizeMismatch(t *testing.T) {
	t.Parallel()

	s := memory.New()

	err := s.Put(t.Context(), "hash1", 10, strings.NewReader("short"))
	if err == nil {
		t.Error("err = nil, want size mismatch error")
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := memory.New()

	var wg sync.WaitGroup
	for i := range 10 {

		wg.Go(func() {

			key := fmt.Sprintf("hash%d", i)
			value := fmt.Sprintf("value%d", i)

			if err := s.Put(t.Context(), key, int64(len(value)), strings.NewReader(value)); err != nil {
				t.Errorf("Put %q: %v", key, err)

				return
			}

			rc, _, err := s.Get(t.Context(), key)
			if err != nil {
				t.Errorf("Get %q: %v", key, err)

				return
			}
			defer rc.Close()

			got, err := io.ReadAll(rc)
			if err != nil {
				t.Errorf("ReadAll %q: %v", key, err)

				return
			}

			if string(got) != value {
				t.Errorf("value = %q, want %q", got, value)
			}
		})
	}

	wg.Wait()
}
