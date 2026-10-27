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
	"sync"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/httpapi"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// testToken and otherToken are fake tokens of two users, valid only in these tests.
const (
	testToken  = "test-token-0123456789abcdef012345"
	otherToken = "other-token-0123456789abcdef01234"
)

const notImplementedType = "urn:hippo:problem:not-implemented"

type fixture struct {
	server  *httptest.Server
	store   *store.Store
	dbPath  string
	logs    *syncBuffer
	userID  string // owner of testToken
	otherID string // owner of otherToken
}

// newFixture starts the real handler on a real SQLite database (docs/dev/testing.md), with two
// users: testToken authenticates the owner, otherToken a second user (RG-90).
func newFixture(t *testing.T) fixture {
	t.Helper()
	st, dbPath := storetest.OpenWithPath(t)
	ctx := context.Background()
	userID, err := st.EnsureSingleUser(ctx, auth.HashToken(testToken))
	if err != nil {
		t.Fatalf("EnsureSingleUser: %v", err)
	}
	otherID, err := st.CreateUser(ctx, "other", auth.HashToken(otherToken))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	logs := &syncBuffer{}
	handler := httpapi.NewServer(httpapi.Options{
		Store:   st,
		Auth:    twoUsers{auth.NewSingleUser(userID, auth.HashToken(testToken)), auth.NewSingleUser(otherID, auth.HashToken(otherToken))},
		Capture: capture.NewService(st),
		Logger:  slog.New(slog.NewJSONHandler(logs, nil)),
		Version: "test",
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return fixture{server: server, store: st, dbPath: dbPath, logs: logs, userID: userID, otherID: otherID}
}

// twoUsers authenticates the tokens of two users with the real single-user authenticator.
type twoUsers [2]auth.SingleUser

func (a twoUsers) Authenticate(token string) (string, bool) {
	for _, user := range a {
		if userID, ok := user.Authenticate(token); ok {
			return userID, true
		}
	}
	return "", false
}

// syncBuffer is a log buffer that concurrent requests can write to.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (f fixture) get(t *testing.T, path, token string) *http.Response {
	t.Helper()
	return f.do(t, http.MethodGet, path, token, "", "")
}

// do sends a request; authorization is the full header value ("" for none).
func (f fixture) do(t *testing.T, method, path, token, contentType, body string) *http.Response {
	t.Helper()
	authorization := ""
	if token != "" {
		authorization = "Bearer " + token
	}
	return f.send(t, method, path, authorization, contentType, body)
}

func (f fixture) send(t *testing.T, method, path, authorization, contentType, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
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

// TestAPIConventions follows openspec/changes/api-contract-v1/specs/api-conventions/spec.md:
// one subtest per scenario, named after it.
func TestAPIConventions(t *testing.T) {
	const wrongToken = "wrong-token-0123456789abcdef0123"

	t.Run("API routes require the bearer token", func(t *testing.T) {
		t.Run("Request without token is rejected", func(t *testing.T) {
			f := newFixture(t)
			resp := f.get(t, "/api/items", "")
			assertProblem(t, resp, http.StatusUnauthorized)
			if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", resp.Header.Get("WWW-Authenticate"))
			}
		})

		t.Run("Request with a wrong token is rejected", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.get(t, "/api/items", wrongToken), http.StatusUnauthorized)
		})

		t.Run("The bearer scheme is case-insensitive", func(t *testing.T) {
			f := newFixture(t)
			resp := f.send(t, http.MethodGet, "/api/items", "bearer "+testToken, "", "")
			// Reaching the route means the not-implemented answer of the declared operation.
			problem := assertProblem(t, resp, http.StatusNotImplemented)
			if problem.Type != notImplementedType {
				t.Errorf("type = %q, want %s", problem.Type, notImplementedType)
			}
		})

		t.Run("Unknown API paths are not revealed without a token", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.get(t, "/api/does-not-exist", ""), http.StatusUnauthorized)
		})
	})

	t.Run("The health check is public", func(t *testing.T) {
		t.Run("Health check without token", func(t *testing.T) {
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

		t.Run("Health check with the database down", func(t *testing.T) {
			f := newFixture(t)
			if err := f.store.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			assertProblem(t, f.get(t, "/healthz", ""), http.StatusServiceUnavailable)
		})
	})

	t.Run("Errors are problem documents", func(t *testing.T) {
		t.Run("A generic error uses about:blank", func(t *testing.T) {
			f := newFixture(t)
			problem := assertProblem(t, f.get(t, "/api/items", ""), http.StatusUnauthorized)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
		})

		t.Run("A business error uses its catalogue type", func(t *testing.T) {
			f := newFixture(t)
			problem := assertProblem(t, f.get(t, "/api/items", testToken), http.StatusNotImplemented)
			if problem.Type != notImplementedType {
				t.Errorf("type = %q, want %s", problem.Type, notImplementedType)
			}
		})
	})

	t.Run("Declared but not implemented routes answer 501", func(t *testing.T) {
		t.Run("Declared route not implemented yet", func(t *testing.T) {
			f := newFixture(t)
			problem := assertProblem(t, f.get(t, "/api/items", testToken), http.StatusNotImplemented)
			if problem.Type != notImplementedType {
				t.Errorf("type = %q, want %s", problem.Type, notImplementedType)
			}
		})
		// "Every declared operation answers 501 until implemented": TestEveryOperationAnswers501.
	})

	t.Run("Unknown routes answer 404", func(t *testing.T) {
		t.Run("Unknown path outside the API", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.get(t, "/nope", ""), http.StatusNotFound)
		})

		t.Run("Unknown path under the API with a valid token", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.get(t, "/api/does-not-exist", testToken), http.StatusNotFound)
		})

		t.Run("Wrong method on a declared path", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.do(t, http.MethodPut, "/api/items", testToken, "", ""), http.StatusNotFound)
		})
	})

	t.Run("Requests that break the contract answer 400", func(t *testing.T) {
		t.Run("Query parameter of the wrong type", func(t *testing.T) {
			f := newFixture(t)
			problem := assertProblem(t, f.get(t, "/api/items?limit=abc", testToken), http.StatusBadRequest)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
		})

		t.Run("Malformed JSON body", func(t *testing.T) {
			f := newFixture(t)
			resp := f.do(t, http.MethodPost, "/api/items", testToken, "application/json", "{not json")
			problem := assertProblem(t, resp, http.StatusBadRequest)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
		})

		t.Run("Undeclared member in the body of an implemented operation", func(t *testing.T) {
			f := newFixture(t)
			resp := f.do(t, http.MethodPost, "/api/items", testToken, "application/json", `{"url": "https://example.com/a", "unknown": 1}`)
			problem := assertProblem(t, resp, http.StatusBadRequest)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
		})
	})

	t.Run("JSON bodies are bounded", func(t *testing.T) {
		t.Run("Oversized JSON body", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://example.com/a", "user_note": "` + strings.Repeat("a", 2<<20) + `"}`
			resp := f.do(t, http.MethodPost, "/api/items", testToken, "application/json", body)
			problem := assertProblem(t, resp, http.StatusRequestEntityTooLarge)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})

		// Same rule on an operation without a strict body yet: the generated decoder hits it.
		t.Run("oversized JSON body of another operation", func(t *testing.T) {
			f := newFixture(t)
			body := `{"annotations": "` + strings.Repeat("a", 2<<20) + `"}`
			resp := f.do(t, http.MethodPatch, "/api/items/01JB8Z6Q4M1X2Y3Z4A5B6C7D8E", testToken, "application/json", body)
			problem := assertProblem(t, resp, http.StatusRequestEntityTooLarge)
			if problem.Type != "about:blank" {
				t.Errorf("type = %q, want about:blank", problem.Type)
			}
		})
	})
}

func TestRequestLogs(t *testing.T) {
	t.Run("never logs the token nor the requested path", func(t *testing.T) {
		f := newFixture(t)
		f.get(t, "/api/items/private-note-title", testToken)
		f.get(t, "/api/items?q=secret", "wrong-token-0123456789abcdef0123")
		f.get(t, "/nope", "")
		f.get(t, "/healthz", "")
		logs := f.logs.String()
		for _, leak := range []string{testToken, "wrong-token", "secret", "private-note-title", "nope"} {
			if strings.Contains(logs, leak) {
				t.Errorf("logs contain %q:\n%s", leak, logs)
			}
		}
		for _, want := range []string{`"status":401`, `"route":"-"`, `"route":"GET /healthz"`, `"route":"GET /api/items/{item_id}"`, `"route":"/"`} {
			if !strings.Contains(logs, want) {
				t.Errorf("logs do not contain %s:\n%s", want, logs)
			}
		}
	})

	t.Run("logs a not implemented route below the error level", func(t *testing.T) {
		f := newFixture(t)
		f.get(t, "/api/items", testToken)
		if strings.Contains(f.logs.String(), `"level":"ERROR"`) {
			t.Errorf("a 501 was logged at ERROR:\n%s", f.logs.String())
		}
	})
}
