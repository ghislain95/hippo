package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
)

const testToken = "spike-test-token"

func send(t *testing.T, received, token, contentType string, body io.Reader) (*http.Response, map[string]any) {
	t.Helper()
	handler := newHandler(testToken, received, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/items", body)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return resp, got
}

func TestCaptures(t *testing.T) {
	t.Run("rejects a bad token", func(t *testing.T) {
		resp, _ := send(t, t.TempDir(), "wrong", "application/json", bytes.NewBufferString(`{"url":"https://example.com"}`))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("accepts a link with a capture note", func(t *testing.T) {
		resp, got := send(t, t.TempDir(), testToken, "application/json",
			bytes.NewBufferString(`{"url":"https://example.com/a?b=c","user_note":"à lire","is_private":true}`))
		if resp.StatusCode != http.StatusCreated || got["form"] != "link" || got["host"] != "example.com" ||
			got["user_note_length"] != float64(6) || got["is_private"] != true {
			t.Errorf("status = %d, answer = %v", resp.StatusCode, got)
		}
	})

	t.Run("accepts a note", func(t *testing.T) {
		resp, got := send(t, t.TempDir(), testToken, "application/json", bytes.NewBufferString(`{"note":"une idée"}`))
		if resp.StatusCode != http.StatusCreated || got["form"] != "note" || got["note_length"] != float64(8) {
			t.Errorf("status = %d, answer = %v", resp.StatusCode, got)
		}
	})

	t.Run("accepts an image and writes it", func(t *testing.T) {
		received := t.TempDir()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="image"; filename="capture.png"`)
		header.Set("Content-Type", "image/png")
		part, _ := writer.CreatePart(header)
		_, _ = part.Write([]byte("\x89PNG fake image"))
		_ = writer.WriteField("user_note", "recette")
		_ = writer.Close()

		resp, got := send(t, received, testToken, writer.FormDataContentType(), &body)
		if resp.StatusCode != http.StatusCreated || got["form"] != "image" || got["image_bytes"] != float64(15) ||
			got["image_type"] != "image/png" {
			t.Errorf("status = %d, answer = %v", resp.StatusCode, got)
		}
		files, _ := filepath.Glob(filepath.Join(received, "*.png"))
		if len(files) != 1 {
			t.Fatalf("images written = %v, want one", files)
		}
		if raw, _ := os.ReadFile(files[0]); string(raw) != "\x89PNG fake image" {
			t.Errorf("written image = %q", raw)
		}
	})

	for _, tc := range []struct{ name, body string }{
		{"rejects malformed JSON", `{"url":`},
		{"rejects a link and a note together", `{"url":"https://example.com","note":"x"}`},
		{"rejects a note with a capture note", `{"note":"x","user_note":"y"}`},
		{"rejects a non-http URL", `{"url":"ftp://example.com"}`},
		{"rejects an unknown field", `{"url":"https://example.com","tags":["go"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := send(t, t.TempDir(), testToken, "application/json", bytes.NewBufferString(tc.body))
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestPing(t *testing.T) {
	handler := newHandler(testToken, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
