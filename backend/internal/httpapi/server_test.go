package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/httpapi"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// testToken is a fake token, valid only in these tests.
const testToken = "test-token-0123456789abcdef012345"

type fixture struct {
	server *httptest.Server
	store  *store.Store
	logs   *bytes.Buffer
}

// newFixture starts the real handler on a real SQLite database (docs/dev/testing.md).
func newFixture(t *testing.T) fixture {
	t.Helper()
	st := storetest.Open(t)
	userID, err := st.EnsureSingleUser(context.Background(), auth.HashToken(testToken))
	if err != nil {
		t.Fatalf("EnsureSingleUser: %v", err)
	}
	logs := &bytes.Buffer{}
	handler := httpapi.NewServer(httpapi.Options{
		Store:   st,
		Auth:    auth.NewSingleUser(userID, auth.HashToken(testToken)),
		Logger:  slog.New(slog.NewJSONHandler(logs, nil)),
		Version: "test",
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return fixture{server: server, store: st, logs: logs}
}

func (f fixture) get(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var body T
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func assertProblem(t *testing.T, resp *http.Response, status int) httpapi.Problem {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d", resp.StatusCode, status)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	problem := decode[httpapi.Problem](t, resp)
	if problem.Status != status || problem.Type == "" || problem.Title == "" {
		t.Errorf("problem = %+v, want status %d with a type and a title", problem, status)
	}
	return problem
}

func TestHealthz(t *testing.T) {
	t.Run("reports ok without a token", func(t *testing.T) {
		f := newFixture(t)
		resp := f.get(t, "/healthz", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		health := decode[httpapi.Health](t, resp)
		if health.Status != httpapi.Ok || health.Version != "test" {
			t.Errorf("health = %+v, want status ok and version test", health)
		}
	})

	t.Run("reports unavailable when the database does not answer", func(t *testing.T) {
		f := newFixture(t)
		if err := f.store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertProblem(t, f.get(t, "/healthz", ""), http.StatusServiceUnavailable)
	})
}

func TestAuthentication(t *testing.T) {
	t.Run("rejects an API request without token", func(t *testing.T) {
		f := newFixture(t)
		resp := f.get(t, "/api/items", "")
		assertProblem(t, resp, http.StatusUnauthorized)
		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Error("WWW-Authenticate header missing on 401")
		}
	})

	t.Run("rejects an API request with a wrong token", func(t *testing.T) {
		f := newFixture(t)
		assertProblem(t, f.get(t, "/api/items", "wrong-token-0123456789abcdef0123"), http.StatusUnauthorized)
	})

	t.Run("lets an authenticated request through", func(t *testing.T) {
		f := newFixture(t)
		// No route exists under /api/ yet: getting past the middleware means a 404, not a 401.
		assertProblem(t, f.get(t, "/api/does-not-exist", testToken), http.StatusNotFound)
	})

	t.Run("never logs the token", func(t *testing.T) {
		f := newFixture(t)
		f.get(t, "/api/items", testToken)
		f.get(t, "/api/items?q=secret", "wrong-token-0123456789abcdef0123")
		logs := f.logs.String()
		for _, leak := range []string{testToken, "wrong-token", "secret"} {
			if strings.Contains(logs, leak) {
				t.Errorf("logs contain %q:\n%s", leak, logs)
			}
		}
		if !strings.Contains(logs, `"status":401`) {
			t.Errorf("logs do not record the request:\n%s", logs)
		}
	})
}

func TestUnknownRoute(t *testing.T) {
	t.Run("answers problem+json outside the API", func(t *testing.T) {
		f := newFixture(t)
		resp := f.get(t, "/nope", "")
		assertProblem(t, resp, http.StatusNotFound)
		_, _ = io.Copy(io.Discard, resp.Body)
	})
}
