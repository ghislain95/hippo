package httpapi

import (
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
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		userID, ok := "", false
		if found {
			userID, ok = authenticator.Authenticate(strings.TrimSpace(token))
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hippo"`)
			writeProblem(w, r, http.StatusUnauthorized, problemTypeBlank, "Unauthorized", "Token d'API absent ou invalide.")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
	})
}

// recoverPanics turns a panic in a handler into a 500 problem, logged with its route.
func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(r.Context(), "handler panicked", slog.String("path", r.URL.Path), slog.Any("panic", recovered))
				writeProblem(w, r, http.StatusInternalServerError, problemTypeBlank, "Internal server error", "Erreur interne du serveur.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests writes one log line per request: method, path, status, duration and request id.
// It never logs headers, query strings nor bodies, which may hold the token or item content.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-Id", requestID)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.InfoContext(r.Context(), "http request",
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
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

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand never fails on supported platforms
	return hex.EncodeToString(b[:])
}
