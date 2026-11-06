package anthropic_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/anthropic"
)

// testKey is a fictitious key: it must never appear in an error.
const testKey = "sk-ant-test-DISTINCTIVE-KEY-4b7e"

// server is a local fake of the Message Batches API: it never reaches the network.
type server struct {
	*httptest.Server
	requests atomic.Int32
}

func newServer(t *testing.T, handler http.HandlerFunc) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		w.Header().Set("Content-Type", "application/json") // as the API answers
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// defaultClient is a client with its own HTTP client, as in production, on the local server.
func defaultClient(t *testing.T, s *server) *anthropic.Client {
	t.Helper()
	c, err := anthropic.New(testKey, anthropic.WithBaseURL(s.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func newClient(t *testing.T, s *server) *anthropic.Client {
	t.Helper()
	c, err := anthropic.New(testKey, anthropic.WithBaseURL(s.URL), anthropic.WithHTTPClient(s.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func batchJSON(id string, created time.Time, counts [5]int) string {
	return fmt.Sprintf(`{"id":%q,"type":"message_batch","processing_status":"in_progress","created_at":%q,`+
		`"expires_at":%q,"archived_at":null,"cancel_initiated_at":null,"ended_at":null,"results_url":null,`+
		`"request_counts":{"processing":%d,"succeeded":%d,"errored":%d,"canceled":%d,"expired":%d}}`,
		id, created.Format(time.RFC3339Nano), created.Add(24*time.Hour).Format(time.RFC3339Nano),
		counts[0], counts[1], counts[2], counts[3], counts[4])
}

// gatedBody is a body whose bytes after the first half are given only once the server has
// received the first half: a client that buffers the whole body before sending never gets
// them, and the call times out.
type gatedBody struct {
	*bytes.Reader
	half     int64
	received chan struct{}
	read     int64
}

func (g *gatedBody) Read(p []byte) (int, error) {
	if g.read >= g.half {
		select {
		case <-g.received:
		case <-time.After(5 * time.Second):
			return 0, errors.New("the server never received the first half: the body is buffered")
		}
	} else if rest := g.half - g.read; int64(len(p)) > rest {
		p = p[:rest]
	}
	n, err := g.Reader.Read(p)
	g.read += int64(n)
	return n, err
}

func TestCreateBatch(t *testing.T) {
	created := time.Date(2027, 1, 10, 1, 0, 3, 0, time.UTC)

	t.Run("sends the body once, streamed, with its length, the key and the version", func(t *testing.T) {
		body := bytes.Repeat([]byte(`{"requests":[]}`+strings.Repeat(" ", 1<<10)), 4<<10) // ~4 MiB
		gate := &gatedBody{Reader: bytes.NewReader(body), half: int64(len(body) / 2), received: make(chan struct{})}
		var once sync.Once
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/messages/batches" {
				t.Errorf("request %s %s, want POST /v1/messages/batches", r.Method, r.URL.Path)
			}
			if r.Header.Get("X-Api-Key") != testKey || r.Header.Get("Anthropic-Version") == "" {
				t.Errorf("x-api-key %q, anthropic-version %q", r.Header.Get("X-Api-Key"), r.Header.Get("Anthropic-Version"))
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("the key is also sent as a bearer token")
			}
			if r.ContentLength != int64(len(body)) || len(r.TransferEncoding) != 0 ||
				r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Length %d, Transfer-Encoding %v, Content-Type %q; want %d, none, application/json",
					r.ContentLength, r.TransferEncoding, r.Header.Get("Content-Type"), len(body))
			}
			got := make([]byte, 0, len(body))
			buf := make([]byte, 64<<10)
			for {
				n, err := r.Body.Read(buf)
				got = append(got, buf[:n]...)
				if int64(len(got)) >= gate.half {
					once.Do(func() { close(gate.received) })
				}
				if err != nil {
					break
				}
			}
			if !bytes.Equal(got, body) {
				t.Errorf("received %d bytes, not the body of %d", len(got), len(body))
			}
			_, _ = io.WriteString(w, batchJSON("msgbatch_01", created, [5]int{2, 0, 0, 0, 0}))
		})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ref, err := newClient(t, s).CreateBatch(ctx, gate, int64(len(body)))
		if err != nil {
			t.Fatalf("CreateBatch: %v", err)
		}
		if ref.ID != "msgbatch_01" || !ref.CreatedAt.Equal(created) {
			t.Errorf("CreateBatch = %+v", ref)
		}
		if n := s.requests.Load(); n != 1 {
			t.Errorf("%d requests, want 1", n)
		}
	})

	errorAnswer := func(status int, errType string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("X-Should-Retry", "true")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":"ECHO-OF-THE-REQUEST-91c4"}}`, errType)
		}
	}
	noLeak := func(t *testing.T, s *server, err error) {
		t.Helper()
		for _, leak := range []string{"ECHO-OF-THE-REQUEST", testKey, s.URL} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("the error %q contains %q", err, leak)
			}
		}
		if n := s.requests.Load(); n != 1 {
			t.Errorf("%d requests, want 1: a create is never retried", n)
		}
	}
	body := []byte(`{"requests":[{"custom_id":"ECHO-OF-THE-REQUEST-91c4"}]}`)

	for _, tc := range []struct {
		name, errType string
		status        int
	}{
		{"an invalid request is rejected once with its status and type, never its message", "invalid_request_error", 400},
		{"a refused key is rejected once", "authentication_error", 401},
		{"a rate limit is rejected once, never retried", "rate_limit_error", 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, errorAnswer(tc.status, tc.errType))
			_, err := newClient(t, s).CreateBatch(context.Background(), bytes.NewReader(body), int64(len(body)))
			var rej *analysis.RejectedError
			if !errors.Is(err, analysis.ErrRejected) || !errors.As(err, &rej) || rej.Status != tc.status || rej.Type != tc.errType {
				t.Fatalf("CreateBatch error = %v, want rejected %d %s", err, tc.status, tc.errType)
			}
			noLeak(t, s, err)
		})
	}

	// A server error may follow a batch the service created (a gateway answering after a long
	// upload): the outcome is unknown, the batch is reconciled, never put back at once.
	for _, tc := range []struct {
		name, errType string
		status        int
	}{
		{"a server error is an unknown outcome with its status, never retried", "api_error", 500},
		{"a bad gateway is an unknown outcome", "", 502},
		{"an unavailable service is an unknown outcome", "", 503},
		{"a gateway timeout is an unknown outcome", "", 504},
		{"an overloaded service is an unknown outcome, never retried", "overloaded_error", 529},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, errorAnswer(tc.status, tc.errType))
			_, err := newClient(t, s).CreateBatch(context.Background(), bytes.NewReader(body), int64(len(body)))
			var server *analysis.ServerError
			if err == nil || errors.Is(err, analysis.ErrRejected) || !errors.As(err, &server) || server.Status != tc.status || server.Type != tc.errType {
				t.Fatalf("CreateBatch error = %v, want an unknown outcome with status %d", err, tc.status)
			}
			noLeak(t, s, err)
		})
	}

	t.Run("a redirect is never followed, and the key never leaves for another host", func(t *testing.T) {
		other := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("the redirect was followed, x-api-key %q", r.Header.Get("X-Api-Key"))
		})
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
		})
		for name, c := range map[string]*anthropic.Client{"default client": defaultClient(t, s), "given client": newClient(t, s)} {
			_, err := c.CreateBatch(context.Background(), bytes.NewReader(body), int64(len(body)))
			if err == nil || errors.Is(err, analysis.ErrRejected) {
				t.Errorf("%s: CreateBatch error = %v, want an unknown outcome", name, err)
			}
			if _, err := c.ListBatches(context.Background(), created); err == nil {
				t.Errorf("%s: ListBatches followed the redirect", name)
			}
		}
		if n := other.requests.Load(); n != 0 {
			t.Errorf("the other host received %d requests", n)
		}
	})

	t.Run("a cut connection is an unknown outcome, never retried", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		})
		body := []byte(`{"requests":[]}`)
		_, err := newClient(t, s).CreateBatch(context.Background(), bytes.NewReader(body), int64(len(body)))
		if err == nil || errors.Is(err, analysis.ErrRejected) {
			t.Fatalf("CreateBatch error = %v, want an unknown outcome", err)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("the error %q contains the key", err)
		}
		if n := s.requests.Load(); n != 1 {
			t.Errorf("%d requests, want 1", n)
		}
	})

	t.Run("a done context is an unknown outcome", func(t *testing.T) {
		release := make(chan struct{})
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			<-release
		})
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		body := []byte(`{"requests":[]}`)
		_, err := newClient(t, s).CreateBatch(ctx, bytes.NewReader(body), int64(len(body)))
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, analysis.ErrRejected) {
			t.Errorf("CreateBatch error = %v, want the deadline", err)
		}
	})

	t.Run("ignores the SDK's environment variables", func(t *testing.T) {
		t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Leak: yes")
		t.Setenv("ANTHROPIC_BASE_URL", "http://192.0.2.1:9")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-token")
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Leak") != "" || r.Header.Get("Authorization") != "" {
				t.Error("a header comes from the environment")
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, batchJSON("msgbatch_01", created, [5]int{}))
		})
		body := []byte(`{"requests":[]}`)
		if _, err := newClient(t, s).CreateBatch(context.Background(), bytes.NewReader(body), int64(len(body))); err != nil {
			t.Errorf("CreateBatch: %v", err)
		}
	})
}

func TestListBatches(t *testing.T) {
	now := time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC)

	t.Run("pages until a page whose batches are all older than since, summing the request counts", func(t *testing.T) {
		pages := map[string]string{
			"":   `{"data":[` + batchJSON("b3", now, [5]int{1, 1, 0, 0, 0}) + `,` + batchJSON("b2", now.Add(-time.Hour), [5]int{0, 2, 1, 1, 1}) + `],"has_more":true,"first_id":"b3","last_id":"b2"}`,
			"b2": `{"data":[` + batchJSON("b1", now.Add(-2*time.Hour), [5]int{0, 3, 0, 0, 0}) + `,` + batchJSON("b0", now.Add(-3*time.Hour), [5]int{0, 1, 0, 0, 0}) + `],"has_more":true,"first_id":"b1","last_id":"b0"}`,
			"b0": `{"data":[` + batchJSON("old", now.Add(-48*time.Hour), [5]int{0, 1, 0, 0, 0}) + `],"has_more":true,"first_id":"old","last_id":"old"}`,
		}
		var afters []string
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/messages/batches" || r.Header.Get("X-Api-Key") != testKey {
				t.Errorf("request %s %s", r.Method, r.URL.Path)
			}
			if limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); limit != anthropic.ListPageSize {
				t.Errorf("limit %q, want %d", r.URL.Query().Get("limit"), anthropic.ListPageSize)
			}
			after := r.URL.Query().Get("after_id")
			afters = append(afters, after)
			_, _ = io.WriteString(w, pages[after])
		})
		got, err := newClient(t, s).ListBatches(context.Background(), now.Add(-150*time.Minute))
		if err != nil {
			t.Fatalf("ListBatches: %v", err)
		}
		want := []analysis.BatchInfo{
			{ID: "b3", CreatedAt: now, RequestCount: 2},
			{ID: "b2", CreatedAt: now.Add(-time.Hour), RequestCount: 5},
			{ID: "b1", CreatedAt: now.Add(-2 * time.Hour), RequestCount: 3},
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("ListBatches = %+v, want %+v", got, want)
		}
		if fmt.Sprint(afters) != fmt.Sprint([]string{"", "b2", "b0"}) {
			t.Errorf("pages read after %q, want the three pages", afters)
		}
	})

	t.Run("fails rather than answer a list cut at 10 pages", func(t *testing.T) {
		var n atomic.Int32
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			i := n.Add(1)
			id := fmt.Sprintf("b%d", i)
			_, _ = fmt.Fprintf(w, `{"data":[%s],"has_more":true,"first_id":%q,"last_id":%q}`, batchJSON(id, now, [5]int{1}), id, id)
		})
		got, err := newClient(t, s).ListBatches(context.Background(), now.Add(-time.Hour))
		if err == nil || got != nil {
			t.Errorf("ListBatches = %d batches, %v; want an error, never a partial list", len(got), err)
		}
		if n.Load() != 10 {
			t.Errorf("%d pages read, want 10", n.Load())
		}
	})

	t.Run("answers 10 pages when the tenth is the last", func(t *testing.T) {
		var n atomic.Int32
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			i := n.Add(1)
			id := fmt.Sprintf("b%d", i)
			_, _ = fmt.Fprintf(w, `{"data":[%s],"has_more":%v,"first_id":%q,"last_id":%q}`, batchJSON(id, now, [5]int{1}), i < 10, id, id)
		})
		got, err := newClient(t, s).ListBatches(context.Background(), now.Add(-time.Hour))
		if err != nil || len(got) != 10 {
			t.Errorf("ListBatches = %d batches, %v; want 10", len(got), err)
		}
	})

	t.Run("a server that never answers fails at the caller's deadline", func(t *testing.T) {
		release := make(chan struct{})
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) { <-release })
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if _, err := newClient(t, s).ListBatches(ctx, now); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("ListBatches error = %v, want the deadline", err)
		}
	})

	t.Run("fails when a page fails, without its message", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"ECHO-91c4"}}`)
		})
		_, err := newClient(t, s).ListBatches(context.Background(), now)
		if err == nil || strings.Contains(err.Error(), "ECHO-91c4") || strings.Contains(err.Error(), testKey) {
			t.Errorf("ListBatches error = %v, want an error without the message nor the key", err)
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("refuses an empty key", func(t *testing.T) {
		if _, err := anthropic.New(""); err == nil {
			t.Error("New accepted an empty key")
		}
	})

	t.Run("implements the Analyzer", func(t *testing.T) {
		var _ analysis.Analyzer = (*anthropic.Client)(nil)
	})
}
