package anthropic_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/anthropic"
)

// leak is a distinctive message an error answer carries: it must never reach an error of Hippo.
const leak = "ECHO-LEAK-7f3a quoted request"

func batchState(status, archived, ended string) string {
	return fmt.Sprintf(`{"id":"msgbatch_1","type":"message_batch","processing_status":%q,"created_at":"2027-01-10T01:00:00Z",`+
		`"expires_at":"2027-01-11T01:00:00Z","archived_at":%s,"cancel_initiated_at":null,"ended_at":%s,"results_url":null,`+
		`"request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0}}`, status, archived, ended)
}

func errorAnswer(w http.ResponseWriter, status int, errorType string) {
	w.Header().Set("Retry-After-Ms", "0") // retried at once by the SDK
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":%q,"message":%q},"request_id":"req_1"}`, errorType, leak)
}

func wantClean(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, s := range []string{leak, testKey, "127.0.0.1", "http://"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error %q holds %q", err, s)
		}
	}
}

func TestGetBatch(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name, status string
	}{
		{"answers a batch in progress", analysis.ProcessingInProgress},
		{"answers a batch being canceled", analysis.ProcessingCanceling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path, key string
			s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				path, key = r.URL.Path, r.Header.Get("x-api-key")
				_, _ = io.WriteString(w, batchState(tc.status, "null", "null"))
			})
			state, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
			if err != nil || state.Processing != tc.status || !state.EndedAt.IsZero() {
				t.Errorf("GetBatch = %+v, %v; want %s", state, err, tc.status)
			}
			if path != "/v1/messages/batches/msgbatch_1" || key != testKey {
				t.Errorf("request to %s with key %q", path, key)
			}
		})
	}

	t.Run("answers an ended batch with its end", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, batchState("ended", "null", `"2027-01-10T01:56:00Z"`))
		})
		state, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
		want := time.Date(2027, 1, 10, 1, 56, 0, 0, time.UTC)
		if err != nil || state.Processing != analysis.ProcessingEnded || !state.EndedAt.Equal(want) {
			t.Errorf("GetBatch = %+v, %v; want ended at %s", state, err, want)
		}
	})

	t.Run("an archived batch has its results unavailable", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, batchState("ended", `"2027-02-08T01:00:00Z"`, `"2027-01-10T01:56:00Z"`))
		})
		if _, err := newClient(t, s).GetBatch(ctx, "msgbatch_1"); !errors.Is(err, analysis.ErrResultsUnavailable) {
			t.Errorf("GetBatch = %v, want ErrResultsUnavailable", err)
		}
	})

	t.Run("a batch unknown to the service has its results unavailable", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) { errorAnswer(w, http.StatusNotFound, "not_found_error") })
		_, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
		if !errors.Is(err, analysis.ErrResultsUnavailable) {
			t.Errorf("GetBatch = %v, want ErrResultsUnavailable", err)
		}
		wantClean(t, err)
	})

	t.Run("a 404 without a JSON body has its results unavailable", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<html>"+leak+"</html>")
		})
		_, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
		if !errors.Is(err, analysis.ErrResultsUnavailable) {
			t.Errorf("GetBatch = %v, want ErrResultsUnavailable", err)
		}
		wantClean(t, err)
	})

	for _, tc := range []struct {
		name      string
		status    int
		errorType string
		calls     int32
	}{
		{"a refused key is a query error with its status and type only", http.StatusUnauthorized, "authentication_error", 1},
		{"a server error is a query error with its status and type only, after the SDK's retries", http.StatusServiceUnavailable, "overloaded_error", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, func(w http.ResponseWriter, r *http.Request) { errorAnswer(w, tc.status, tc.errorType) })
			_, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
			var query *analysis.QueryError
			if !errors.As(err, &query) || query.Status != tc.status || query.Type != tc.errorType {
				t.Errorf("GetBatch = %v, want a query error %d %s", err, tc.status, tc.errorType)
			}
			if errors.Is(err, analysis.ErrResultsUnavailable) {
				t.Errorf("GetBatch = %v, must not close the batch", err)
			}
			wantClean(t, err)
			if got := s.requests.Load(); got != tc.calls {
				t.Errorf("%d requests, want %d", got, tc.calls)
			}
		})
	}

	t.Run("an error type outside the API's form is answered as other", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			errorAnswer(w, http.StatusUnauthorized, "Bad type: "+leak)
		})
		_, err := newClient(t, s).GetBatch(ctx, "msgbatch_1")
		var query *analysis.QueryError
		if !errors.As(err, &query) || query.Status != http.StatusUnauthorized || query.Type != "other" {
			t.Errorf("GetBatch = %v, want a query error 401 other", err)
		}
		wantClean(t, err)
	})

	t.Run("a done context is the context's error", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := newClient(t, s).GetBatch(cctx, "msgbatch_1")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("GetBatch = %v, want context.Canceled", err)
		}
		wantClean(t, err)
	})
}

// line is a line of a results file.
func succeededLine(customID, stopReason, content, usage string) string {
	return fmt.Sprintf(`{"custom_id":%q,"result":{"type":"succeeded","message":{"id":"msg_1","type":"message","role":"assistant",`+
		`"model":"claude-haiku-4-5","content":[%s],"stop_reason":%q,"stop_sequence":null,"usage":%s}}}`,
		customID, content, stopReason, usage)
}

const (
	toolUse   = `{"type":"text","text":"Je range."},{"type":"tool_use","id":"toolu_1","name":"classify_item","input":{"title":"Un titre","tags":["go","testing"]}}`
	fullUsage = `{"input_tokens":120,"output_tokens":340,"cache_creation_input_tokens":1500,"cache_read_input_tokens":9000,` +
		`"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":500},"service_tier":"batch"}`
)

// resultsServer serves body as the results file of msgbatch_1, recording the request.
func resultsServer(t *testing.T, body func(w http.ResponseWriter)) (*server, *http.Request) {
	t.Helper()
	seen := &http.Request{}
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		*seen = *r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/binary")
		body(w)
	})
	return s, seen
}

// readAll reads every line of the results of msgbatch_1, the results and the errors in order.
func readAll(t *testing.T, s *server) ([]analysis.Result, []error) {
	t.Helper()
	r, err := newClient(t, s).Results(context.Background(), "msgbatch_1")
	if err != nil {
		t.Fatalf("Results: %v", err)
	}
	defer func() { _ = r.Close() }()
	var (
		results []analysis.Result
		errs    []error
	)
	for range 100 {
		res, err := r.Next()
		if errors.Is(err, io.EOF) {
			return results, errs
		}
		if err != nil {
			errs = append(errs, err)
			if !errors.Is(err, analysis.ErrUnreadableLine) {
				return results, errs
			}
			continue
		}
		results = append(results, res)
	}
	t.Fatal("the reader never ended")
	return nil, nil
}

func TestResults(t *testing.T) {
	t.Run("asks the results file with the key and the JSONL type", func(t *testing.T) {
		s, seen := resultsServer(t, func(w http.ResponseWriter) {})
		if results, errs := readAll(t, s); len(results) != 0 || len(errs) != 0 {
			t.Errorf("empty file = %v, %v", results, errs)
		}
		if seen.URL.Path != "/v1/messages/batches/msgbatch_1/results" || seen.Header.Get("x-api-key") != testKey ||
			seen.Header.Get("Accept") != "application/x-jsonl" || seen.Method != http.MethodGet {
			t.Errorf("request %s %s, key %q, accept %q", seen.Method, seen.URL.Path, seen.Header.Get("x-api-key"), seen.Header.Get("Accept"))
		}
	})

	t.Run("results not found are unavailable", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) { errorAnswer(w, http.StatusNotFound, "not_found_error") })
		_, err := newClient(t, s).Results(context.Background(), "msgbatch_1")
		if !errors.Is(err, analysis.ErrResultsUnavailable) {
			t.Errorf("Results = %v, want ErrResultsUnavailable", err)
		}
		wantClean(t, err)
	})

	t.Run("another error status is a query error without its message", func(t *testing.T) {
		s := newServer(t, func(w http.ResponseWriter, r *http.Request) { errorAnswer(w, http.StatusForbidden, "permission_error") })
		_, err := newClient(t, s).Results(context.Background(), "msgbatch_1")
		var query *analysis.QueryError
		if !errors.As(err, &query) || query.Status != http.StatusForbidden || query.Type != "permission_error" {
			t.Errorf("Results = %v, want a query error 403", err)
		}
		wantClean(t, err)
	})

	t.Run("reads a succeeded line with its classify_item call and the cache breakdown", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, succeededLine("01JITEM", "tool_use", toolUse, fullUsage)+"\n")
		})
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 1 {
			t.Fatalf("read %v, %v; want one result", results, errs)
		}
		got := results[0]
		if got.CustomID != "01JITEM" || got.Kind != analysis.ResultSucceeded || got.StopReason != "tool_use" {
			t.Errorf("result = %+v", got)
		}
		if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "classify_item" ||
			string(got.ToolCalls[0].Input) != `{"title":"Un titre","tags":["go","testing"]}` {
			t.Errorf("tool calls = %+v, want the classify_item call only, its input as sent", got.ToolCalls)
		}
		want := analysis.Usage{Input: 120, Output: 340, CacheWrite5m: 1000, CacheWrite1h: 500, CacheRead: 9000}
		if got.Usage != want {
			t.Errorf("usage = %+v, want %+v", got.Usage, want)
		}
	})

	t.Run("a missing cache breakdown counts every cache write as five-minute", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, succeededLine("a", "max_tokens", toolUse,
				`{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":700,"cache_read_input_tokens":3}`)+"\n")
		})
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 1 {
			t.Fatalf("read %v, %v", results, errs)
		}
		if want := (analysis.Usage{Input: 1, Output: 2, CacheWrite5m: 700, CacheRead: 3}); results[0].Usage != want {
			t.Errorf("usage = %+v, want %+v", results[0].Usage, want)
		}
		if results[0].StopReason != "max_tokens" {
			t.Errorf("stop reason = %q", results[0].StopReason)
		}
	})

	t.Run("an errored line keeps its error type only", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = fmt.Fprintf(w, `{"custom_id":"a","result":{"type":"errored","error":{"type":"error","error":`+
				`{"type":"invalid_request_error","message":%q},"request_id":"req_1"}}}`+"\n", leak)
		})
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 1 {
			t.Fatalf("read %v, %v", results, errs)
		}
		got := results[0]
		if got.Kind != analysis.ResultErrored || got.ErrorType != "invalid_request_error" || got.Usage != (analysis.Usage{}) {
			t.Errorf("result = %+v", got)
		}
		if strings.Contains(fmt.Sprintf("%+v", got), "ECHO-LEAK") {
			t.Errorf("the result keeps the API message: %+v", got)
		}
	})

	t.Run("an errored line with an error type outside the API's form keeps other", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = fmt.Fprintf(w, `{"custom_id":"a","result":{"type":"errored","error":{"type":"error","error":`+
				`{"type":%q,"message":"m"}}}}`+"\n", "ECHO-LEAK "+strings.Repeat("x", 100))
		})
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 1 || results[0].ErrorType != "other" {
			t.Errorf("read %+v, %v; want one errored line of type other", results, errs)
		}
	})

	t.Run("reads expired and canceled lines", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"custom_id":"a","result":{"type":"expired"}}`+"\n"+`{"custom_id":"b","result":{"type":"canceled"}}`)
		})
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 2 || results[0].Kind != analysis.ResultExpired || results[1].Kind != analysis.ResultCanceled ||
			results[1].CustomID != "b" {
			t.Errorf("read %+v, %v; want expired then canceled (the last line without line break)", results, errs)
		}
	})

	t.Run("skips a non-JSON line, a line without custom_id and an unknown result type", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `not json at all `+leak+"\n"+
				`{"result":{"type":"expired"}}`+"\n"+
				`{"custom_id":"x","result":{"type":"paused"}}`+"\n"+
				`{"custom_id":"x","result":{"type":"succeeded"}}`+"\n"+
				"\n"+
				`{"custom_id":"ok","result":{"type":"expired"}}`+"\n")
		})
		results, errs := readAll(t, s)
		if len(results) != 1 || results[0].CustomID != "ok" {
			t.Errorf("results = %+v, want the last line only", results)
		}
		if len(errs) != 4 {
			t.Errorf("errors = %v, want 4 unreadable lines (a blank line is no line)", errs)
		}
		for _, err := range errs {
			if !errors.Is(err, analysis.ErrUnreadableLine) {
				t.Errorf("error %v, want ErrUnreadableLine", err)
			}
			wantClean(t, err)
		}
	})

	t.Run("a line of 4 MiB and one byte is unreadable, the next line is read", func(t *testing.T) {
		long := `{"custom_id":"long","result":{"type":"expired"},"pad":"` // a valid line once padded
		pad := anthropic.MaxResultLine + 1 - len(long) - len(`"}`)
		longLine := long + strings.Repeat("x", pad) + `"}`
		if len(longLine) != anthropic.MaxResultLine+1 {
			t.Fatalf("line of %d bytes", len(longLine))
		}
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, longLine+"\n"+`{"custom_id":"next","result":{"type":"canceled"}}`+"\n")
		})
		results, errs := readAll(t, s)
		if len(errs) != 1 || !errors.Is(errs[0], analysis.ErrUnreadableLine) {
			t.Errorf("errors = %v, want one unreadable line", errs)
		}
		if len(results) != 1 || results[0].CustomID != "next" {
			t.Errorf("results = %+v, want the next line", results)
		}
	})

	t.Run("a line of exactly 4 MiB is read", func(t *testing.T) {
		long := `{"custom_id":"long","result":{"type":"expired"},"pad":"`
		line := long + strings.Repeat("x", anthropic.MaxResultLine-len(long)-len(`"}`)) + `"}`
		s, _ := resultsServer(t, func(w http.ResponseWriter) { _, _ = io.WriteString(w, line+"\n") })
		results, errs := readAll(t, s)
		if len(errs) != 0 || len(results) != 1 || results[0].CustomID != "long" {
			t.Errorf("read %d results, %v; want the line", len(results), errs)
		}
	})

	t.Run("a body cut mid-line stops the read with an error that is not an unreadable line", func(t *testing.T) {
		first := `{"custom_id":"a","result":{"type":"expired"}}` + "\n"
		cut := `{"custom_id":"b","result":{"ty`
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", fmt.Sprint(len(first)+len(cut)+100))
			_, _ = io.WriteString(w, first+cut)
			// The handler returns with the announced length unwritten: the client sees the body cut.
		})
		results, errs := readAll(t, s)
		if len(results) != 1 || results[0].CustomID != "a" {
			t.Errorf("results = %+v, want the first line", results)
		}
		if len(errs) != 1 || errors.Is(errs[0], analysis.ErrUnreadableLine) || errors.Is(errs[0], io.EOF) {
			t.Errorf("errors = %v, want one read error", errs)
		}
		wantClean(t, errors.Join(errs...))
	})

	t.Run("a closed reader closes the body", func(t *testing.T) {
		s, _ := resultsServer(t, func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, strings.Repeat(`{"custom_id":"a","result":{"type":"expired"}}`+"\n", 3))
		})
		r, err := newClient(t, s).Results(context.Background(), "msgbatch_1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("Close = %v", err)
		}
		if _, err := r.Next(); err == nil {
			t.Error("Next after Close read a line")
		}
	})
}

func TestErrorTypeClass(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"invalid_request_error", "invalid_request_error"},
		{"overloaded_error", "overloaded_error"},
		{strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{strings.Repeat("a", 65), "other"},
		{"", ""}, // no type in the answer
		{"Invalid_request_error", "other"},
		{"error 42", "other"},
		{"api-error", "other"},
		{"erreur_é", "other"},
		{"type\n", "other"},
	} {
		if got := anthropic.ErrorTypeClass(tc.in); got != tc.want {
			t.Errorf("ErrorTypeClass(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestReaderMemory proves the reader holds one line, not the file: a file of many lines is
// read through a buffer that never grows past the longest line.
func TestReaderMemory(t *testing.T) {
	t.Run("reads many lines without holding the file", func(t *testing.T) {
		var file bytes.Buffer
		for i := range 2000 {
			fmt.Fprintf(&file, `{"custom_id":"i%d","result":{"type":"expired"}}`+"\n", i)
		}
		r := anthropic.NewResultReader(io.NopCloser(&file))
		n := 0
		for {
			_, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			n++
		}
		if n != 2000 {
			t.Errorf("read %d lines, want 2000", n)
		}
		if got := anthropic.BufferCap(r); got > 64<<10 {
			t.Errorf("line buffer of %d bytes, want at most one short line", got)
		}
	})

	t.Run("releases the buffer of a line longer than 64 KiB", func(t *testing.T) {
		// A readable line of 1 MiB (an unknown member is ignored), then a short one.
		long := `{"custom_id":"long","padding":"` + strings.Repeat("x", 1<<20) + `","result":{"type":"expired"}}`
		file := long + "\n" + `{"custom_id":"short","result":{"type":"expired"}}` + "\n"
		r := anthropic.NewResultReader(io.NopCloser(strings.NewReader(file)))
		first, err := r.Next()
		if err != nil || first.CustomID != "long" {
			t.Fatalf("first line %+v, %v; want the long one read", first, err)
		}
		if got := anthropic.BufferCap(r); got < 1<<20 {
			t.Fatalf("line buffer of %d bytes after the long line, want it to hold the line", got)
		}
		second, err := r.Next()
		if err != nil || second.CustomID != "short" {
			t.Fatalf("second line %+v, %v; want the short one", second, err)
		}
		if got := anthropic.BufferCap(r); got > 64<<10 {
			t.Errorf("line buffer of %d bytes after the short line, want the long line's released", got)
		}
	})
}
