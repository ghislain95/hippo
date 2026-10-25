package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ghislain95/hippo/backend/internal/auth"
)

// requireAuth rejects with 401 every request under prefix without a valid bearer token, and
// puts the authenticated user in the request context (RG-90).
func requireAuth(authenticator Authenticator, prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		userID, ok := "", false
		if token, found := bearerToken(r.Header.Get("Authorization")); found {
			userID, ok = authenticator.Authenticate(token)
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hippo"`)
			writeProblem(w, r, http.StatusUnauthorized, problemTypeBlank, "Unauthorized", "Token d'API absent ou invalide.")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
	})
}

// bearerToken extracts the token of an "Authorization: Bearer <token>" header. The scheme is
// case-insensitive (RFC 9110).
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// recoverPanics turns a panic in a handler into a 500 problem, logged with its route.
func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(r.Context(), "handler panicked", slog.String("route", routeOf(r.Context())), slog.Any("panic", recovered))
				writeProblem(w, r, http.StatusInternalServerError, problemTypeBlank, "Internal server error", "Erreur interne du serveur.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests writes one log line per request: request id, method, route pattern, status and
// duration. It logs the route pattern ("GET /api/items/{id}"), never the raw path, headers,
// query string nor body, which may hold the token or item content.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-Id", requestID)
		route := &routeHolder{}
		r = r.WithContext(context.WithValue(r.Context(), routeKey{}, route))
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.InfoContext(r.Context(), "http request",
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("route", route.String()),
			slog.Int("status", recorder.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the underlying writer (Flush, deadlines).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

type routeKey struct{}

// routeHolder carries the route pattern matched by the router back to logRequests.
type routeHolder struct {
	pattern string
}

// String returns the matched pattern, or "-" when the request never reached the router
// (rejected by the authentication).
func (h *routeHolder) String() string {
	if h.pattern == "" {
		return "-"
	}
	return h.pattern
}

func routeOf(ctx context.Context) string {
	if holder, ok := ctx.Value(routeKey{}).(*routeHolder); ok {
		return holder.String()
	}
	return "-"
}

// recordRoute saves the pattern the router matched (http.Request.Pattern), even when the
// handler panics.
func recordRoute(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if holder, ok := r.Context().Value(routeKey{}).(*routeHolder); ok {
				holder.pattern = r.Pattern
			}
		}()
		router.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand never fails on supported platforms
	return hex.EncodeToString(b[:])
}
