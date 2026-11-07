package analysis_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

	t.Run("Result logs carry no content", func(t *testing.T) {
		const (
			title       = "TITLE-RESULT-SECRET-5a9e"
			ficheText   = "FICHE-RESULT-SECRET-0c41"
			tag         = "tag-result-secret-77d2"
			category    = "CATEGORY-RESULT-SECRET-3b8f"
			invalidText = "INVALID-RESULT-SECRET-e61a"
			apiMessage  = "API-MESSAGE-RESULT-SECRET-94c0 near the request"
			// The values of a valid response the store refuses to write (design D8).
			refusedTitle    = "TITLE-REFUSED-SECRET-2d7c"
			refusedFiche    = "FICHE-REFUSED-SECRET-8a03"
			refusedTag      = "tag-refused-secret-51be"
			refusedCategory = "CATEGORY-REFUSED-SECRET-c94f"
		)
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 4)
		dev := e.categoryID(t, e.userID, "Dev")
		distinctive := func(title, fiche, tag, category string) input {
			return tipInput(dev).with("title", title).with("tags", []any{tag, "go"}).
				with("fiche", map[string]any{"template": "tip", "tip": fiche, "steps": []any{fiche}, "materials": []any{}, "warnings": []any{}}).
				with("category", map[string]any{"proposed": map[string]any{
					"name": category, "parent_id": dev, "description": category, "reason": category,
				}})
		}
		validIn := distinctive(title, ficheText, tag, category)
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[3], "analyzed")
		// Invalid: no title, the validator would quote the values around it.
		invalidIn := tipInput(dev).without("title").with("tags", []any{"go", invalidText})
		file := jsonLine(t, ids[0], validIn) + "\n" + jsonLine(t, ids[1], invalidIn) + "\n" +
			fmt.Sprintf(`{"custom_id":%q,"result":{"type":"errored","error":{"type":"error","error":`+
				`{"type":"invalid_request_error","message":%q}}}}`, ids[2], apiMessage) + "\n" +
			jsonLine(t, ids[3], distinctive(refusedTitle, refusedFiche, refusedTag, refusedCategory)) + "\n"
		cfg := e.cfg
		cfg.Analyzer = realClient(t, file)
		cfg.Logger = slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		pass := newFollowPass(t, cfg)
		if err := pass.Follow(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.wantStatus(t, e.userID, ids[0], "analyzed") // the values were applied, not only refused

		logs := e.logs.String()
		for _, s := range []string{
			title, ficheText, tag, category, invalidText, apiMessage, "API-MESSAGE", "sk-ant-test-key",
			refusedTitle, refusedFiche, refusedTag, refusedCategory, "REFUSED-SECRET",
		} {
			if strings.Contains(logs, s) {
				t.Errorf("the logs contain %q", s)
			}
		}
		line := e.lastFollowLine(t, b.ID)
		want := map[string]any{
			"outcome": "ended", "analyzed": float64(1), "invalid": float64(2), "errored": float64(1), "unknown": float64(0),
			"input_tokens": float64(300), "output_tokens": float64(30),
		}
		for key, value := range want {
			if line[key] != value {
				t.Errorf("follow-up line %s = %v, want %v", key, line[key], value)
			}
		}
		if cost, ok := line["cost_usd"].(float64); !ok || cost <= 0 {
			t.Errorf("follow-up line cost_usd = %v, want the batch's cost", line["cost_usd"])
		}
		for _, want := range []map[string]string{
			{"msg": "analysis follow-up item", "item_id": ids[0], "outcome": "analyzed"},
			{"msg": "analysis follow-up item", "item_id": ids[1], "outcome": "invalid", "reason": "schema"},
			{"msg": "analysis follow-up item", "item_id": ids[2], "outcome": "errored", "error_type": "invalid_request_error"},
			{"msg": "analysis follow-up item", "item_id": ids[3], "outcome": "invalid", "error_type": "store", "level": "ERROR"},
		} {
			e.wantLog(t, want)
		}
	})
}
