package analysis_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis/anthropic"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

func TestPassLogs(t *testing.T) {
	t.Run("Logs carry no content", func(t *testing.T) {
		const apiKey = "sk-ant-test-LOGKEY-5e8d1c"
		secrets := []string{apiKey}
		secret := func(s string) string {
			secrets = append(secrets, s)
			return s
		}

		// The local fake of the API accepts the first batch and refuses the second with a
		// message quoting the request, as the API may.
		var creates atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if creates.Add(1) == 1 {
				_, _ = fmt.Fprintf(w, `{"id":"msgbatch_logs1","type":"message_batch","processing_status":"in_progress",`+
					`"created_at":%q,"request_counts":{"processing":2,"succeeded":0,"errored":0,"canceled":0,"expired":0}}`,
					passStart.Format(time.RFC3339))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request near TITLE-LOGS-SECRET-a17b"}}`)
		}))
		defer srv.Close()
		client, err := anthropic.New(apiKey, anthropic.WithBaseURL(srv.URL), anthropic.WithHTTPClient(srv.Client()))
		if err != nil {
			t.Fatal(err)
		}

		e := newPassEnv(t)
		userID := e.user(t, "owner")
		e.capture(t, userID, store.NewItem{
			Kind: "link", Source: "share", Status: "queued", Title: "example.com",
			URL:      ptr(secret("https://user:PASSWORD-LOGS-c3d4@logs-secret-host.example/PATH-LOGS-e5f6?q=QUERY-LOGS-0a1b")),
			UserNote: ptr(secret("NOTE-LOGS-SECRET-77aa")),
		}, store.Extraction{
			Mode: "full", Title: ptr(secret("TITLE-LOGS-SECRET-a17b")), SiteName: ptr(secret("SITE-LOGS-SECRET-90cd")),
			ContentText: ptr(secret("CONTENT-LOGS-SECRET-b2e3")),
		})
		secrets = append(secrets, "PASSWORD-LOGS-c3d4", "logs-secret-host", "PATH-LOGS-e5f6", "QUERY-LOGS-0a1b")
		// An image whose copy fails: a JPEG cut after its header (undecodable).
		e.image(t, userID, imagetest.JPEGCut(64, 48), "image/jpeg", ptr(secret("IMAGE-NOTE-LOGS-SECRET-f00d")))

		cfg := e.passConfig(t)
		cfg.Analyzer = client
		e.run(t, cfg)
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "sent", "anthropic_batch_id": "msgbatch_logs1"})
		e.wantLog(t, map[string]string{"user_id": userID, "reason": "undecodable"})

		// The next night: the first batch has its cost, a new item is refused by the service.
		storetest.SetBatchState(t, e.path, userID, e.batches(t, userID)[0].ID, store.BatchEnded, ptr(0.01))
		e.clock.Advance(24 * time.Hour)
		e.note(t, userID, secret("NOTE-TEXT-LOGS-SECRET-1234"))
		e.run(t, cfg)
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "rejected", "error_type": "invalid_request_error"})

		logs := e.logs.String()
		for _, s := range secrets {
			if strings.Contains(logs, s) {
				t.Errorf("the logs contain %q", s)
			}
		}
		if strings.Contains(logs, srv.URL) {
			t.Error("the logs contain the URL of the API")
		}
	})
}
