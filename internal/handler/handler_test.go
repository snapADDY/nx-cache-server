package handler_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/snapaddy/nx-cache-server/internal/api"
	"github.com/snapaddy/nx-cache-server/internal/handler"
	"github.com/snapaddy/nx-cache-server/internal/store/memory"
)

const (
	readWriteToken = "rw-token"
	readOnlyToken  = "ro-token"
)

type staticToken string

func (s staticToken) BearerToken(context.Context, api.OperationName) (api.BearerToken, error) {
	return api.BearerToken{Token: string(s)}, nil
}

// contentLengthTransport promotes the Content-Length header that ogen encodes
// as an operation parameter to request.ContentLength. Go's transport ignores
// the header field and would otherwise send the body chunked, which the
// server rejects because the spec marks Content-Length as required.
type contentLengthTransport struct {
	base http.RoundTripper
}

func (t contentLengthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// ogen encodes the number-typed parameter as e.g. "7.0000000000",
	// so it must be parsed as a float.
	if cl := req.Header.Get("Content-Length"); cl != "" && req.ContentLength <= 0 {
		if n, err := strconv.ParseFloat(cl, 64); err == nil {
			req.ContentLength = int64(n)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return resp, nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	srv, err := api.NewServer(
		handler.New(memory.New(), logger),
		handler.NewSecurity(readWriteToken, readOnlyToken),
	)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return ts
}

func newTestClient(t *testing.T, url string, token string) *api.Client {
	t.Helper()

	httpClient := &http.Client{Transport: contentLengthTransport{base: http.DefaultTransport}}

	client, err := api.NewClient(url, staticToken(token), api.WithClient(httpClient))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return client
}

func putArtifact(t *testing.T, client *api.Client, hash string, content []byte) api.PutRes {
	t.Helper()

	res, err := client.Put(t.Context(),
		api.PutReq{Data: bytes.NewReader(content)},
		api.PutParams{ContentLength: float64(len(content)), Hash: hash},
	)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	return res
}

func TestPutThenGetRoundTrip(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	client := newTestClient(t, ts.URL, readWriteToken)
	content := []byte("cache artifact payload")

	putRes := putArtifact(t, client, "abc123", content)
	if _, ok := putRes.(*api.PutOK); !ok {
		t.Fatalf("Put res = %T, want *api.PutOK", putRes)
	}

	res, err := client.Get(t.Context(), api.GetParams{Hash: "abc123"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	ok, isOK := res.(*api.GetOK)
	if !isOK {
		t.Fatalf("Get res = %T, want *api.GetOK", res)
	}

	got, err := io.ReadAll(ok.Data)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestGetMiss(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	client := newTestClient(t, ts.URL, readWriteToken)

	res, err := client.Get(t.Context(), api.GetParams{Hash: "missing"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if _, ok := res.(*api.GetNotFound); !ok {
		t.Errorf("res = %T, want *api.GetNotFound", res)
	}
}

func TestPutConflict(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	client := newTestClient(t, ts.URL, readWriteToken)
	content := []byte("payload")

	putArtifact(t, client, "abc123", content)

	res := putArtifact(t, client, "abc123", content)
	if _, ok := res.(*api.PutConflict); !ok {
		t.Errorf("res = %T, want *api.PutConflict", res)
	}
}

func TestPutWithReadOnlyTokenForbidden(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	client := newTestClient(t, ts.URL, readOnlyToken)

	res := putArtifact(t, client, "abc123", []byte("payload"))

	forbidden, ok := res.(*api.PutForbidden)
	if !ok {
		t.Fatalf("res = %T, want *api.PutForbidden", res)
	}

	msg, err := io.ReadAll(forbidden.Data)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(msg) == 0 {
		t.Error("403 body is empty, want explanation message")
	}
}

func TestGetWithReadOnlyToken(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	content := []byte("payload")

	putArtifact(t, newTestClient(t, ts.URL, readWriteToken), "abc123", content)

	res, err := newTestClient(t, ts.URL, readOnlyToken).Get(t.Context(), api.GetParams{Hash: "abc123"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	ok, isOK := res.(*api.GetOK)
	if !isOK {
		t.Fatalf("res = %T, want *api.GetOK", res)
	}

	got, err := io.ReadAll(ok.Data)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// 401 responses are produced by ogen's security layer and are not part of
// the GET operation's spec, so the generated client cannot decode them.
// These cases go through net/http directly.
func TestUnauthorized(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	tests := []struct {
		name string
		auth string
	}{
		{name: "missing token", auth: ""},
		{name: "wrong token", auth: "Bearer wrong-token"},
		{name: "non-bearer scheme", auth: "Basic cnc6dG9rZW4="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/v1/cache/abc123", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}

			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}

			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}
