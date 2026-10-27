package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// deadlineRecorder is a response writer that records the deadlines http.ResponseController
// sets on it, as the connection of a real server would apply them.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	read, write time.Time
}

func (d *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	d.read = deadline
	return nil
}

func (d *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	d.write = deadline
	return nil
}

func TestBoundTransfers(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	serve := func(t *testing.T, r *http.Request) *deadlineRecorder {
		t.Helper()
		w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		boundTransfers(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, r)
		return w
	}
	// A deadline of 5 minutes from the request, give or take the time of the test.
	assertTransferDeadline := func(t *testing.T, name string, deadline, start time.Time) {
		t.Helper()
		if deadline.Before(start.Add(transferTimeout)) || deadline.After(time.Now().Add(transferTimeout)) {
			t.Errorf("%s deadline = %v, want 5 minutes after %v", name, deadline, start)
		}
	}

	t.Run("an upload gets 5 minutes to be read and answered", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader("--b--"))
		r.Pattern = createItemPattern
		r.Header.Set("Content-Type", "multipart/form-data; boundary=b")
		start := time.Now()
		w := serve(t, r)
		assertTransferDeadline(t, "read", w.read, start)
		assertTransferDeadline(t, "write", w.write, start)
	})

	t.Run("a download gets 5 minutes to be written", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/assets/01JB8Z6Q4M1X2Y3Z4A5B6C7D8E", nil)
		r.Pattern = getAssetPattern
		start := time.Now()
		w := serve(t, r)
		assertTransferDeadline(t, "write", w.write, start)
		if !w.read.IsZero() {
			t.Errorf("read deadline = %v, want the server's", w.read)
		}
	})

	t.Run("a JSON capture keeps the server's timeouts", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader("{}"))
		r.Pattern = createItemPattern
		r.Header.Set("Content-Type", "application/json")
		w := serve(t, r)
		if !w.read.IsZero() || !w.write.IsZero() {
			t.Errorf("deadlines = %v, %v; want the server's", w.read, w.write)
		}
	})

	t.Run("a declared length over 21 MiB is refused before reading", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader("--b--"))
		r.Pattern = createItemPattern
		r.Header.Set("Content-Type", "multipart/form-data; boundary=b")
		r.ContentLength = maxMultipartBody + 1
		w := serve(t, r)
		if w.Code != http.StatusRequestEntityTooLarge || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("answer %d %q, want 413 problem+json", w.Code, w.Header().Get("Content-Type"))
		}
	})
}
