package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"

	"github.com/snapaddy/nx-cache-server/internal/api"
)

// permission is the access level granted to an authenticated request.
type permission int

const (
	permissionReadWrite permission = iota
	permissionReadOnly
)

type permissionCtxKey struct{}

func permissionFromContext(ctx context.Context) (permission, bool) {
	perm, ok := ctx.Value(permissionCtxKey{}).(permission)

	return perm, ok
}

var errInvalidToken = errors.New("invalid token")

// Security implements api.SecurityHandler with a read-write access token
// and an optional read-only token. Tokens are stored as SHA-256 hashes so
// comparison is constant-time regardless of token length.
type Security struct {
	readWriteHash [sha256.Size]byte
	readOnlyHash  [sha256.Size]byte
	hasReadOnly   bool
}

var _ api.SecurityHandler = (*Security)(nil)

func NewSecurity(readWriteToken, readOnlyToken string) *Security {
	s := &Security{
		readWriteHash: sha256.Sum256([]byte(readWriteToken)),
		hasReadOnly:   readOnlyToken != "",
	}
	if s.hasReadOnly {
		s.readOnlyHash = sha256.Sum256([]byte(readOnlyToken))
	}

	return s
}

// HandleBearerToken authenticates the bearer token and stores the granted
// permission in the returned context. Returning an error makes ogen respond
// with 401; the read-only write restriction is enforced in the Put handler
// (403) instead.
func (s *Security) HandleBearerToken(ctx context.Context, _ api.OperationName, t api.BearerToken) (context.Context, error) {
	sum := sha256.Sum256([]byte(t.Token))

	// Evaluate both comparisons before branching so a timing side channel
	// cannot distinguish read-write from read-only tokens.
	isReadWrite := subtle.ConstantTimeCompare(sum[:], s.readWriteHash[:]) == 1
	isReadOnly := s.hasReadOnly && subtle.ConstantTimeCompare(sum[:], s.readOnlyHash[:]) == 1

	switch {
	case isReadWrite:
		return context.WithValue(ctx, permissionCtxKey{}, permissionReadWrite), nil
	case isReadOnly:
		return context.WithValue(ctx, permissionCtxKey{}, permissionReadOnly), nil
	default:
		// ogen discards the returned context when an error is returned.
		return nil, errInvalidToken
	}
}
