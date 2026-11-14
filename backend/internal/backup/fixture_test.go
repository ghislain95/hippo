package backup_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/backup"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// holdLockEnv makes the test binary a process that takes the lock of the backup folder it
// names, says so on its standard output, then waits to be killed.
const holdLockEnv = "HIPPO_BACKUP_TEST_HOLD_LOCK"

func TestMain(m *testing.M) {
	if dir := os.Getenv(holdLockEnv); dir != "" {
		os.Exit(holdLock(dir))
	}
	os.Exit(m.Run())
}

// holdLock is the child of « The lock of a killed process is released »: it never releases the
// lock itself; it returns only if its standard input is closed, which its parent never does.
func holdLock(dir string) int {
	if _, err := backup.HoldLock(dir); err != nil {
		_, _ = os.Stdout.WriteString("error\n")
		return 1
	}
	_, _ = os.Stdout.WriteString("locked\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

// start is the instant of the first backup of a fixture: 03:00 in Paris, 13 November 2026.
var start = time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC)

// fixture is a data folder as the service leaves it (the database opened by the store, with
// every migration, and images written through assetfs), a backup folder, a clock and the logs.
type fixture struct {
	dataDir, dbPath, assetsDir, backupDir string

	st     *store.Store
	assets *assetfs.Dir
	userID string
	paris  *time.Location
	logs   *syncBuffer

	mu  sync.Mutex
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := newEmptyFixture(t)
	st, err := store.Open(context.Background(), f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f.st = st
	if f.userID, err = st.CreateUser(context.Background(), "owner", "hash-owner"); err != nil {
		t.Fatal(err)
	}
	return f
}

// newEmptyFixture is a fixture whose database the test creates itself.
func newEmptyFixture(t *testing.T) *fixture {
	t.Helper()
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		dataDir:   t.TempDir(),
		backupDir: t.TempDir(),
		paris:     paris,
		logs:      &syncBuffer{},
		now:       start,
	}
	f.dbPath = filepath.Join(f.dataDir, "hippo.db")
	f.assetsDir = filepath.Join(f.dataDir, "assets")
	assets, err := assetfs.Open(f.assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = assets.Close() })
	f.assets = assets
	return f
}

func (f *fixture) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// set moves the clock of the next backup.
func (f *fixture) set(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = now
}

// nameAt is the name of a backup started at now: Paris time, its offset, no colon.
func (f *fixture) nameAt(now time.Time) string {
	return now.In(f.paris).Format("2006-01-02T150405-0700")
}

func (f *fixture) options() backup.Options {
	return backup.Options{
		Database: f.dbPath,
		Assets:   f.assetsDir,
		Dir:      f.backupDir,
		Location: f.paris,
		Now:      f.Now,
		Version:  "abc1234",
		Trigger:  backup.TriggerCommand,
		Logger:   slog.New(slog.NewJSONHandler(f.logs, nil)),
	}
}

// run makes a backup with the default options and fails the test unless it succeeds.
func (f *fixture) run(t *testing.T) backup.Result {
	t.Helper()
	res, err := backup.Run(context.Background(), f.options())
	if err != nil || res.Outcome != backup.OutcomeOK {
		t.Fatalf("Run = %+v, %v; want ok", res, err)
	}
	return res
}

// note captures a note titled title and returns its id.
func (f *fixture) note(t *testing.T, title string, private bool) string {
	t.Helper()
	text := title
	item, _, err := f.st.CaptureItem(context.Background(), f.userID, store.NewItem{
		Kind: "note", Source: "share", Status: "queued", Title: title, UserNote: &text, IsPrivate: private,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}

// image captures an image item titled title holding content, as the capture does (file
// committed through assetfs before the transaction), and returns its id and the relative path
// of its file in the images folder.
func (f *fixture) image(t *testing.T, title string, content []byte, private bool) (itemID, rel string) {
	t.Helper()
	assetID, err := f.st.NewID()
	if err != nil {
		t.Fatal(err)
	}
	rel = f.userID + "/" + assetID + ".png"
	upload, err := f.assets.CreateTemp()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upload.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := upload.Commit(rel); err != nil {
		t.Fatal(err)
	}
	upload.Keep()
	item, _, err := f.st.CaptureItem(context.Background(), f.userID, store.NewItem{
		Kind: "image", Source: "share", Status: "queued", Title: title, IsPrivate: private,
		Asset: &store.NewAsset{ID: assetID, Path: rel, MimeType: "image/png", SizeBytes: int64(len(content))},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return item.ID, rel
}

// pngOf is a distinct small PNG for each n.
func pngOf(n int) []byte { return imagetest.PNG(8+n, 8) }

// backups lists the names of the folders of the backup folder that are complete backups, in
// the backup format.
func (f *fixture) backups(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, e := range f.entries(t) {
		if _, err := time.Parse("2006-01-02T150405-0700", e.Name()); err == nil && e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// partials lists the .partial- entries of the backup folder.
func (f *fixture) partials(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, e := range f.entries(t) {
		if strings.HasPrefix(e.Name(), ".partial-") {
			names = append(names, e.Name())
		}
	}
	return names
}

func (f *fixture) entries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(f.backupDir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// manifest reads the manifest of the backup name.
func (f *fixture) manifest(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.backupDir, name, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

// logLines decodes the JSON log lines whose message is msg.
func (f *fixture) logLines(t *testing.T, msg string) []map[string]any {
	t.Helper()
	return decodeLines(t, f.logs.String(), msg)
}

func decodeLines(t *testing.T, out, msg string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", scanner.Text(), err)
		}
		if line["msg"] == msg {
			lines = append(lines, line)
		}
	}
	return lines
}

// number reads a JSON number of a decoded object as an int64.
func number(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s = %v, want a number", key, m[key])
	}
	return int64(v)
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(fa, fb)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// tree lists every entry under root, relative and slash-separated, with its Lstat mode.
func tree(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()
	entries := map[string]fs.FileMode{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = info.Mode()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// contains tells whether some regular file under root holds content.
func contains(t *testing.T, root string, content []byte) bool {
	t.Helper()
	for rel, mode := range tree(t, root) {
		if mode.IsRegular() && bytes.Equal(readFile(t, filepath.Join(root, rel)), content) {
			return true
		}
	}
	return false
}

// syncBuffer is a bytes.Buffer safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
