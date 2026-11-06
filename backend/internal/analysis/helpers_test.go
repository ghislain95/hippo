package analysis_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// testModel is the model of the tests' requests, the default of the configuration.
const testModel = "claude-haiku-4-5"

// TestMain makes the test binary the child that copies an image: the requests are built with
// the real child, as in production.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == imagecopy.ChildCommand {
		// syscall.Exit: under -race, os.Exit(0) waits a second for the race detector.
		syscall.Exit(imagecopy.RunChild(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// env is a real store, a real directory of images and a real temporary directory, with the
// test binary as the image copier.
type env struct {
	st       *store.Store
	path     string // of the database
	assetDir string
	assets   *assetfs.Dir
	temp     *extraction.TempDir
	limits   imagecopy.Limits
	logs     *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, path := storetest.OpenWithPath(t)
	return newEnvOn(t, st, path)
}

// newEnvWithClock is newEnv with a store that reads the time from now.
func newEnvWithClock(t *testing.T, now func() time.Time) *env {
	t.Helper()
	st, path := storetest.OpenWithClock(t, now)
	return newEnvOn(t, st, path)
}

func newEnvOn(t *testing.T, st *store.Store, path string) *env {
	t.Helper()
	assetDir := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		t.Fatal(err)
	}
	assets, err := assetfs.Open(assetDir)
	if err != nil {
		t.Fatalf("open assets: %v", err)
	}
	temp, _, err := extraction.OpenTempDir(filepath.Join(t.TempDir(), "tmp"))
	if err != nil {
		t.Fatalf("open temporary directory: %v", err)
	}
	t.Cleanup(func() {
		_ = assets.Close()
		_ = temp.Close()
	})
	return &env{
		st: st, path: path, assetDir: assetDir, assets: assets, temp: temp,
		limits: imagecopy.DefaultLimits(), logs: &bytes.Buffer{},
	}
}

func (e *env) user(t *testing.T, name string) string {
	t.Helper()
	id, err := e.st.CreateUser(context.Background(), name, "hash-"+name)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return id
}

// capture stores in for userID and writes its extraction, so that it waits for the batch.
func (e *env) capture(t *testing.T, userID string, in store.NewItem, ex store.Extraction) string {
	t.Helper()
	ctx := context.Background()
	item, _, err := e.st.CaptureItem(ctx, userID, in, nil)
	if err != nil {
		t.Fatalf("CaptureItem: %v", err)
	}
	written, err := e.st.WriteExtraction(ctx, userID, item.ID, ex)
	if err != nil || !written {
		t.Fatalf("WriteExtraction = %v, %v", written, err)
	}
	return item.ID
}

// link captures rawURL with its provisional title, the host, as capture does (RG-26).
func (e *env) link(t *testing.T, userID, rawURL string, userNote *string, ex store.Extraction) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	host := strings.ToLower(u.Hostname())
	return e.capture(t, userID, store.NewItem{
		Kind: "link", Source: "share", Status: "queued", Title: host, URL: &rawURL, UserNote: userNote,
	}, ex)
}

func (e *env) note(t *testing.T, userID, text string) string {
	t.Helper()
	title, _, _ := strings.Cut(text, "\n")
	return e.capture(t, userID, store.NewItem{
		Kind: "note", Source: "manual", Status: "queued", Title: title, UserNote: &text,
	}, store.Extraction{Mode: "none"})
}

// image stores content as the image of a new item of userID, in the directory of images, and
// returns the item id and the path of the stored file.
func (e *env) image(t *testing.T, userID string, content []byte, mime string, userNote *string) (string, string) {
	t.Helper()
	return e.imageWith(t, userID, content, mime, userNote, false)
}

// imageWith is image, private when private is set.
func (e *env) imageWith(t *testing.T, userID string, content []byte, mime string, userNote *string, private bool) (string, string) {
	t.Helper()
	id, err := e.st.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/heic": "heic", "image/webp": "webp"}[mime]
	rel := userID + "/" + id + "." + ext
	file := filepath.Join(e.assetDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, content, 0o640); err != nil {
		t.Fatal(err)
	}
	itemID := e.capture(t, userID, store.NewItem{
		Kind: "image", Source: "share", Status: "queued", UserNote: userNote, IsPrivate: private,
		TitleAt: func(now time.Time) string { return "Image du " + now.Format(time.DateOnly) },
		Asset:   &store.NewAsset{ID: id, Path: rel, MimeType: mime, SizeBytes: int64(len(content))},
	}, store.Extraction{Mode: "none"})
	return itemID, file
}

// requests returns the request writer of userID's batch, closed at the end of the test.
func (e *env) requests(t *testing.T, userID string, bodyLimit int64) *analysis.Requests {
	t.Helper()
	return e.requestsWith(t, userID, bodyLimit, []string{os.Args[0], imagecopy.ChildCommand})
}

// requestsWith is requests with the image copier running argv.
func (e *env) requestsWith(t *testing.T, userID string, bodyLimit int64, argv []string) *analysis.Requests {
	t.Helper()
	cfg := e.config(t, userID)
	cfg.BodyLimit = bodyLimit
	cfg.Copier = imagecopy.NewCopier(argv, e.limits)
	return e.open(t, cfg)
}

// config is the configuration of the request writer of userID's batch, on the env.
func (e *env) config(t *testing.T, userID string) analysis.RequestsConfig {
	t.Helper()
	ctx := context.Background()
	p, err := prompt.Load()
	if err != nil {
		t.Fatalf("prompt.Load: %v", err)
	}
	ref, err := analysis.LoadReferentiel(ctx, e.st, userID)
	if err != nil {
		t.Fatalf("LoadReferentiel: %v", err)
	}
	return analysis.RequestsConfig{
		UserID: userID, Model: testModel, Prompt: p, Referentiel: ref,
		Contents: e.st, Assets: e.assets, Temp: e.temp,
		Copier: imagecopy.NewCopier([]string{os.Args[0], imagecopy.ChildCommand}, e.limits),
		Logger: slog.New(slog.NewJSONHandler(e.logs, nil)),
	}
}

// open returns the request writer of cfg, closed at the end of the test.
func (e *env) open(t *testing.T, cfg analysis.RequestsConfig) *analysis.Requests {
	t.Helper()
	reqs, err := analysis.NewRequests(cfg)
	if err != nil {
		t.Fatalf("NewRequests: %v", err)
	}
	t.Cleanup(func() {
		if err := reqs.Close(); err != nil {
			t.Errorf("close requests: %v", err)
		}
	})
	return reqs
}

// build writes the request of every item waiting for userID and assembles the body of all of
// them: the bytes and their parsed form.
func (e *env) build(t *testing.T, userID string) ([]byte, body) {
	t.Helper()
	ctx := context.Background()
	queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
	if err != nil {
		t.Fatalf("AnalysisQueue: %v", err)
	}
	reqs := e.requests(t, userID, 0)
	for _, item := range queue {
		added, err := reqs.Add(ctx, item)
		if err != nil || !added {
			t.Fatalf("Add = %v, %v; want added", added, err)
		}
	}
	return assemble(t, reqs, reqs.IDs())
}

func assemble(t *testing.T, reqs *analysis.Requests, ids []string) ([]byte, body) {
	t.Helper()
	var buf bytes.Buffer
	size, err := reqs.WriteBody(&buf, ids)
	if err != nil {
		t.Fatalf("WriteBody: %v", err)
	}
	if size != int64(buf.Len()) {
		t.Errorf("WriteBody = %d bytes, wrote %d", size, buf.Len())
	}
	var parsed body
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}
	return buf.Bytes(), parsed
}

// body is the Message Batches create body, as the API reads it.
type body struct {
	Requests []request `json:"requests"`
}

type request struct {
	CustomID string `json:"custom_id"`
	Params   params `json:"params"`
}

func (r *request) UnmarshalJSON(raw []byte) error {
	var fields struct {
		CustomID string                     `json:"custom_id"`
		Params   map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var p params
	paramsRaw, err := json.Marshal(fields.Params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(paramsRaw, &p.parsed); err != nil {
		return err
	}
	p.raw = fields.Params
	r.CustomID, r.Params = fields.CustomID, p
	return nil
}

type params struct {
	parsed struct {
		Model      string            `json:"model"`
		MaxTokens  int               `json:"max_tokens"`
		Tools      []tool            `json:"tools"`
		ToolChoice map[string]string `json:"tool_choice"`
		System     []block           `json:"system"`
		Messages   []message         `json:"messages"`
	}
	raw map[string]json.RawMessage
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type block struct {
	Type         string            `json:"type"`
	Text         string            `json:"text"`
	CacheControl map[string]string `json:"cache_control"`
	Source       *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

// of returns the request of the item id, failing the test when the body has none.
func (b body) of(t *testing.T, id string) request {
	t.Helper()
	for _, r := range b.Requests {
		if r.CustomID == id {
			return r
		}
	}
	t.Fatalf("no request for %s among %d", id, len(b.Requests))
	return request{}
}

// referentiel is the text of the second system block of a request.
func (r request) referentiel(t *testing.T) string {
	t.Helper()
	if len(r.Params.parsed.System) != 2 {
		t.Fatalf("system of %d blocks, want 2", len(r.Params.parsed.System))
	}
	return r.Params.parsed.System[1].Text
}

// content is the content of the single user message of a request.
func (r request) content(t *testing.T) []block {
	t.Helper()
	if len(r.Params.parsed.Messages) != 1 {
		t.Fatalf("%d messages, want 1", len(r.Params.parsed.Messages))
	}
	return r.Params.parsed.Messages[0].Content
}

// images are the decoded image blocks of a request.
func (r request) images(t *testing.T) [][]byte {
	t.Helper()
	var images [][]byte
	for _, b := range r.content(t) {
		if b.Type != "image" {
			continue
		}
		if b.Source == nil || b.Source.Type != "base64" || b.Source.MediaType != "image/jpeg" {
			t.Fatalf("image block %+v, want a base64 image/jpeg source", b.Source)
		}
		data, err := base64.StdEncoding.DecodeString(b.Source.Data)
		if err != nil {
			t.Fatalf("image data is not base64: %v", err)
		}
		images = append(images, data)
	}
	return images
}

// logLines are the JSON log lines written so far.
func (e *env) logLines(t *testing.T) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(e.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		lines = append(lines, fields)
	}
	return lines
}

// hasLog tells whether a log line carries every field of want.
func (e *env) hasLog(t *testing.T, want map[string]string) bool {
	t.Helper()
	for _, line := range e.logLines(t) {
		match := true
		for key, value := range want {
			if line[key] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
