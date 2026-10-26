// Command echo is the throwaway server of the E1 prototype (openspec/changes/ios-prototype,
// design D6). It accepts the captures of the share extension in the shapes of the real contract
// (POST /api/items, JSON or multipart) and logs what arrived, never the token nor the content.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

// maxImageBytes is the image limit of RG-02.
const maxImageBytes = 20 << 20

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address")
	tokenFile := flag.String("token-file", "../.spike-token", "file holding the Bearer token")
	received := flag.String("received", "received", "directory where received images are written")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	raw, err := os.ReadFile(*tokenFile)
	if err != nil {
		logger.Error("read token file: run `npm run configure` in spikes/e1-ios/app first", "err", err)
		os.Exit(1)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		logger.Error("empty token file", "file", *tokenFile)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(token, *received, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("echo server listening", "addr", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}

// capture is the JSON body of a link or a note (CaptureRequest in api/openapi.yaml).
type capture struct {
	URL      *string `json:"url"`
	Note     *string `json:"note"`
	UserNote *string `json:"user_note"`
	Private  bool    `json:"is_private"`
	ClientID *string `json:"client_id"` // accepted, not checked: no replay in the prototype
}

// receipt is what the server answers, and what it logs.
type receipt struct {
	Form         string `json:"form"`
	Host         string `json:"host,omitempty"`
	NoteLength   int    `json:"note_length,omitempty"`
	UserNoteLen  int    `json:"user_note_length,omitempty"`
	Private      bool   `json:"is_private"`
	ImageBytes   int64  `json:"image_bytes,omitempty"`
	ImageType    string `json:"image_type,omitempty"`
	ImageSavedAs string `json:"-"`
}

func newHandler(token, received string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/items", func(w http.ResponseWriter, r *http.Request) {
		got, err := readCapture(r, received)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("capture received", "form", got.Form, "host", got.Host, "note_length", got.NoteLength,
			"user_note_length", got.UserNoteLen, "is_private", got.Private, "image_bytes", got.ImageBytes,
			"image_type", got.ImageType, "saved_as", got.ImageSavedAs)
		writeJSON(w, http.StatusCreated, got)
	})
	return logRequests(requireToken(token, mux), logger)
}

func readCapture(r *http.Request, received string) (receipt, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return receipt{}, fmt.Errorf("content type: %w", err)
	}
	switch mediaType {
	case "application/json":
		var body capture
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			return receipt{}, fmt.Errorf("json body: %w", err)
		}
		return linkOrNote(body)
	case "multipart/form-data":
		return readImage(r, received)
	default:
		return receipt{}, fmt.Errorf("unsupported content type %q", mediaType)
	}
}

// linkOrNote checks the shape of RG-01, RG-02 and RG-07: exactly one of url and note, and no
// capture note on a note.
func linkOrNote(body capture) (receipt, error) {
	got := receipt{Private: body.Private}
	if body.UserNote != nil {
		got.UserNoteLen = len([]rune(*body.UserNote))
	}
	switch {
	case body.URL != nil && body.Note == nil:
		parsed, err := url.Parse(*body.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return receipt{}, errors.New("url must be an http or https URL")
		}
		got.Form, got.Host = "link", parsed.Host
	case body.Note != nil && body.URL == nil:
		if strings.TrimSpace(*body.Note) == "" {
			return receipt{}, errors.New("note is empty")
		}
		if body.UserNote != nil {
			return receipt{}, errors.New("a note holds no user_note")
		}
		got.Form, got.NoteLength = "note", len([]rune(*body.Note))
	default:
		return receipt{}, errors.New("exactly one of url and note is required")
	}
	return got, nil
}

func readImage(r *http.Request, received string) (receipt, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxImageBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return receipt{}, fmt.Errorf("multipart body: %w", err)
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		return receipt{}, fmt.Errorf("image part: %w", err)
	}
	defer file.Close()

	got := receipt{Form: "image", Private: r.FormValue("is_private") == "true"}
	got.UserNoteLen = len([]rune(r.FormValue("user_note")))
	got.ImageType = header.Header.Get("Content-Type")
	if err := os.MkdirAll(received, 0o750); err != nil {
		return receipt{}, fmt.Errorf("received directory: %w", err)
	}
	name := filepath.Join(received, time.Now().Format("20060102-150405.000")+filepath.Ext(header.Filename))
	out, err := os.Create(name) //nolint:gosec // a path built from a timestamp
	if err != nil {
		return receipt{}, fmt.Errorf("create image file: %w", err)
	}
	defer out.Close()
	if got.ImageBytes, err = io.Copy(out, file); err != nil {
		return receipt{}, fmt.Errorf("write image file: %w", err)
	}
	if got.ImageBytes == 0 {
		return receipt{}, errors.New("image is empty")
	}
	got.ImageSavedAs = name
	return got, nil
}

func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func logRequests(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", recorder.status,
			"bytes_in", r.ContentLength, "duration_ms", time.Since(start).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
