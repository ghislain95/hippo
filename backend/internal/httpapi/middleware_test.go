package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/auth"
)

func TestRequireAuth(t *testing.T) {
	const token = "test-token-0123456789abcdef012345"
	authenticator := auth.NewSingleUser("01JB8Z6Q4M1X2Y3Z4A5B6C7D8E", auth.HashToken(token))

	var seenUserID string
	var seenOK bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenUserID, seenOK = auth.UserID(r.Context())
	})
	handler := requireAuth(authenticator, "/api/", next)

	t.Run("puts the authenticated user in the request context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if !seenOK || seenUserID != "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E" {
			t.Errorf("user in context = %q, %v; want the authenticated user", seenUserID, seenOK)
		}
	})

	t.Run("accepts the Bearer scheme in any case", func(t *testing.T) {
		seenOK = false
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("Authorization", "bearer "+token)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if !seenOK {
			t.Error("a lowercase bearer scheme was rejected")
		}
	})

	t.Run("rejects a token sent without the Bearer scheme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("Authorization", token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestRecoverPanics(t *testing.T) {
	t.Run("turns a panic into a 500 problem", func(t *testing.T) {
		handler := recoverPanics(slog.New(slog.NewJSONHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("got %d %q, want a 500 problem", rec.Code, rec.Header().Get("Content-Type"))
		}
	})
}
