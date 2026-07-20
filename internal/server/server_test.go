package server_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snapaddy/nx-cache-server/internal/server"
	"github.com/snapaddy/nx-cache-server/internal/store/memory"
)

const (
	readWriteToken = "rw-token"
	readOnlyToken  = "ro-token"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return newTestServerWithLimit(t, 0)
}

func newTestServerWithLimit(t *testing.T, maxUploadBytes int64) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(server.NewServerHandler(memory.New(), readWriteToken, readOnlyToken, maxUploadBytes))
	t.Cleanup(ts.Close)

	return ts
}

// response is a fully consumed HTTP response.
type response struct {
	header http.Header
	body   []byte
	status int
}

// doRequest performs a request against ts and consumes the response.
func doRequest(t *testing.T, ts *httptest.Server, method, path, token string, body []byte) response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	return response{status: resp.StatusCode, header: resp.Header, body: respBody}
}

func TestPutThenGetRoundTrip(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	content := []byte("cache artifact payload")

	putResp := doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readWriteToken, content)
	if putResp.status != http.StatusOK {
		t.Fatalf("Put status = %d, want 200", putResp.status)
	}

	getResp := doRequest(t, ts, http.MethodGet, "/v1/cache/abc123", readWriteToken, nil)
	if getResp.status != http.StatusOK {
		t.Fatalf("Get status = %d, want 200", getResp.status)
	}

	if ct := getResp.header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}

	if !bytes.Equal(getResp.body, content) {
		t.Errorf("content = %q, want %q", getResp.body, content)
	}
}

func TestGetMiss(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/v1/cache/missing", readWriteToken, nil)
	if resp.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.status)
	}
}

func TestPutConflict(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	content := []byte("payload")

	resp := doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readWriteToken, content)
	if resp.status != http.StatusOK {
		t.Fatalf("first Put status = %d, want 200", resp.status)
	}

	resp = doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readWriteToken, content)
	if resp.status != http.StatusConflict {
		t.Errorf("second Put status = %d, want 409", resp.status)
	}
}

func TestPutWithReadOnlyTokenForbidden(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readOnlyToken, []byte("payload"))
	if resp.status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.status)
	}

	if len(resp.body) == 0 {
		t.Error("403 body is empty, want explanation message")
	}
}

func TestGetWithReadOnlyToken(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	content := []byte("payload")

	resp := doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readWriteToken, content)
	if resp.status != http.StatusOK {
		t.Fatalf("Put status = %d, want 200", resp.status)
	}

	getResp := doRequest(t, ts, http.MethodGet, "/v1/cache/abc123", readOnlyToken, nil)
	if getResp.status != http.StatusOK {
		t.Fatalf("Get status = %d, want 200", getResp.status)
	}

	if !bytes.Equal(getResp.body, content) {
		t.Errorf("content = %q, want %q", getResp.body, content)
	}
}

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

func TestInvalidHashRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	// %2F decodes to "/" and survives ServeMux routing, so an unguarded hash
	// could carry path-traversal segments into the storage key. Each of these
	// must be rejected with 400 before reaching the store.
	paths := []string{
		"/v1/cache/..%2F..%2Fsecret",
		"/v1/cache/foo%2Fbar",
		"/v1/cache/%2e%2e%2fescape", // decodes to "../escape"
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			getResp := doRequest(t, ts, http.MethodGet, p, readWriteToken, nil)
			if getResp.status != http.StatusBadRequest {
				t.Errorf("GET %s status = %d, want 400", p, getResp.status)
			}

			putResp := doRequest(t, ts, http.MethodPut, p, readWriteToken, []byte("x"))
			if putResp.status != http.StatusBadRequest {
				t.Errorf("PUT %s status = %d, want 400", p, putResp.status)
			}
		})
	}
}

func TestUploadExceedsLimit(t *testing.T) {
	t.Parallel()

	ts := newTestServerWithLimit(t, 8)

	resp := doRequest(t, ts, http.MethodPut, "/v1/cache/abc123", readWriteToken, []byte("this body is far too large"))
	if resp.status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.status)
	}

	small := doRequest(t, ts, http.MethodPut, "/v1/cache/def456", readWriteToken, []byte("small"))
	if small.status != http.StatusOK {
		t.Errorf("under-limit status = %d, want 200", small.status)
	}
}

func TestHealthWithoutToken(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/health", "", nil)
	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.status)
	}
}

func TestPutWithoutContentLength(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)

	// An io.Reader that is not a *bytes.Reader (or similar) has an unknown
	// length, so net/http sends the body chunked without Content-Length.
	body := io.MultiReader(bytes.NewReader([]byte("payload")))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, ts.URL+"/v1/cache/abc123", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+readWriteToken)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
