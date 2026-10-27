// Package httpapi is the HTTP transport of Hippo: the server generated from api/openapi.yaml,
// its handlers and middlewares. Handlers decode, call the domain and encode: no business rule
// lives here (docs/dev/conventions-backend.md).
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ghislain95/hippo/backend/internal/capture"
)

// Pinger reports whether the database answers.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Authenticator resolves a bearer token to its user (ADR-0016).
type Authenticator interface {
	Authenticate(token string) (userID string, ok bool)
}

// Capturer captures items (internal/capture).
type Capturer interface {
	Capture(ctx context.Context, userID string, req capture.Request) (capture.Result, error)
}

// Options are the dependencies of the HTTP server.
type Options struct {
	Store   Pinger
	Auth    Authenticator
	Capture Capturer
	Logger  *slog.Logger
	Version string
}

// NewServer returns the HTTP handler of the whole API: /healthz is public, everything under
// /api/ requires a valid bearer token.
func NewServer(opts Options) http.Handler {
	handlers := &handlers{store: opts.Store, capture: opts.Capture, version: opts.Version}
	strict := NewStrictHandlerWithOptions(handlers, nil, StrictHTTPServerOptions{
		// A body the generated decoder cannot read: 413 over the JSON limit, 400 otherwise.
		RequestErrorHandlerFunc: writeBodyError,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, errNotImplemented) {
				opts.Logger.DebugContext(r.Context(), "operation not implemented", slog.String("route", routeOf(r.Context())))
				writeProblem(w, r, http.StatusNotImplemented, problemTypeNotImplemented, "Not implemented", "Cette fonction n'est pas encore disponible.")
				return
			}
			opts.Logger.ErrorContext(r.Context(), "handler failed", slog.String("error", err.Error()))
			writeProblem(w, r, http.StatusInternalServerError, problemTypeBlank, "Internal server error", "Erreur interne du serveur.")
		},
	})

	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []MiddlewareFunc{strictJSONBodies},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeProblem(w, r, http.StatusBadRequest, problemTypeBlank, "Bad request", "La requête est invalide.")
		},
	})
	// Unknown routes answer problem+json too. Under /api/, authentication runs first, so an
	// unauthenticated client cannot probe which routes exist.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, problemTypeBlank, "Not found", "Cette ressource n'existe pas.")
	})

	handler := recordRoute(mux)
	handler = requireAuth(opts.Auth, "/api/", handler)
	handler = recoverPanics(opts.Logger, handler)
	handler = logRequests(opts.Logger, handler)
	return handler
}

// handlers implements StrictServerInterface. Operations no story has implemented yet come from
// the embedded notImplemented stubs (stubs.go).
type handlers struct {
	notImplemented
	store   Pinger
	capture Capturer
	version string
}

// healthTimeout bounds the database check of /healthz.
const healthTimeout = 2 * time.Second

// GetHealth reports the service up when the database answers (GET /healthz).
func (h *handlers) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		detail := "La base de données ne répond pas."
		return GetHealth503ApplicationProblemPlusJSONResponse{
			Type:   problemTypeBlank,
			Title:  "Service unavailable",
			Status: http.StatusServiceUnavailable,
			Detail: &detail,
		}, nil
	}
	return GetHealth200JSONResponse{Status: Ok, Version: h.version}, nil
}
