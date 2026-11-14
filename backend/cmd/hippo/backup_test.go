package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/schedule"
	"github.com/ghislain95/hippo/backend/internal/schedule/scheduletest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// The scenarios of openspec/changes/nightly-backup/specs/backup/spec.md that the command and
// the service answer; the others are in internal/backup.

const testToken = "test-token-0123456789abcdef012345"

// serviceEnv is the environment of a service on new data and backup folders.
func serviceEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"API_TOKEN":  testToken,
		"HTTP_ADDR":  "127.0.0.1:" + freePort(t),
		"DATA_DIR":   t.TempDir(),
		"BACKUP_DIR": t.TempDir(),
	}
}

// startService runs the service with env until it answers its healthcheck; stop cancels it and
// waits for its end.
func startService(t *testing.T, env map[string]string) (stop func()) {
	t.Helper()
	getenv := func(name string) string { return env[name] }
	ctx, cancel := context.WithCancel(context.Background())
	logs := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, getenv, logs) }()
	deadline := time.Now().Add(5 * time.Second)
	for healthcheck(context.Background(), getenv) != nil {
		if time.Now().After(deadline) {
			cancel()
			err := <-done
			t.Fatalf("server never became healthy (run: %v):\n%s", err, logs)
		}
		time.Sleep(20 * time.Millisecond) // polling a real server: no event to wait on
	}
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}
	t.Cleanup(stop)
	return stop
}

// backupCommandProcess runs `hippo backup` as the test binary with env, as `docker compose
// exec` does, and returns its exit code, standard output and standard error.
func backupCommandProcess(t *testing.T, env map[string]string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], backupCommandName)
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, out.String(), errOut.String()
	case errors.As(err, &exit):
		return exit.ExitCode(), out.String(), errOut.String()
	default:
		t.Fatalf("run the command: %v", err)
		return 0, "", ""
	}
}

// backupLines decodes the log lines « backup » of out.
func backupLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(out), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("line %q: %v", raw, err)
		}
		if line["msg"] == "backup" {
			lines = append(lines, line)
		}
	}
	return lines
}

// backupNames lists the folders of dir in the backup format.
func backupNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if _, err := time.Parse("2006-01-02T150405-0700", e.Name()); err == nil && e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// complete tells whether the backup name of dir holds its database, images and manifest.
func complete(t *testing.T, dir, name string) bool {
	t.Helper()
	for _, entry := range []string{"hippo.db", "assets", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name, entry)); err != nil {
			return false
		}
	}
	return true
}

// holdBackupLock takes the lock of the backup folder dir as another process would: its own
// open of .lock, which conflicts with any other (flock(2)). It returns the release.
func holdBackupLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			_ = f.Close() // closing releases the lock
		}
	}
	t.Cleanup(release)
	return release
}

// dataFolder creates, in env's DATA_DIR, the database with every migration and the images
// folder, as a service that ran once leaves them.
func dataFolder(t *testing.T, env map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(env["DATA_DIR"], "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(env["DATA_DIR"], "hippo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCommand(t *testing.T) {
	t.Run("The command makes a backup while the service runs", func(t *testing.T) {
		env := serviceEnv(t)
		startService(t, env)
		code, stdout, stderr := backupCommandProcess(t, env)
		if code != 0 {
			t.Fatalf("exit code %d, stderr %q, stdout:\n%s", code, stderr, stdout)
		}
		lines := backupLines(t, stdout)
		if len(lines) != 1 || lines[0]["outcome"] != "ok" || lines[0]["trigger"] != "command" {
			t.Fatalf("output lines %v, want one with outcome ok", lines)
		}
		name, _ := lines[0]["backup"].(string)
		if names := backupNames(t, env["BACKUP_DIR"]); len(names) != 1 || names[0] != name || !complete(t, env["BACKUP_DIR"], name) {
			t.Errorf("backups %v, want the complete backup %q", names, name)
		}
	})

	t.Run("The command exits non-zero when the backup fails", func(t *testing.T) {
		env := serviceEnv(t)
		startService(t, env)
		if err := os.RemoveAll(env["BACKUP_DIR"]); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := backupCommandProcess(t, env)
		if code == 0 {
			t.Fatalf("exit code 0, want non-zero; stdout:\n%s", stdout)
		}
		if lines := backupLines(t, stdout); len(lines) != 1 || lines[0]["outcome"] != "failed" {
			t.Errorf("output lines %v, want one with outcome failed", lines)
		}
		if _, err := os.Stat(env["BACKUP_DIR"]); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the backup folder: %v, want still missing", err)
		}
		if !strings.HasPrefix(stderr, "hippo: ") || strings.Contains(stderr, "/") {
			t.Errorf("error message %q, want one naming no file", stderr)
		}
		if strings.Contains(stdout, env["BACKUP_DIR"]) || strings.Contains(stdout, env["DATA_DIR"]) {
			t.Errorf("output names a folder:\n%s", stdout)
		}
	})

	t.Run("the command prints its line whatever LOG_LEVEL says", func(t *testing.T) {
		env := serviceEnv(t)
		env["LOG_LEVEL"] = "error"
		dataFolder(t, env)
		out := &syncBuffer{}
		if err := backupCommand(context.Background(), func(name string) string { return env[name] }, out, time.Second); err != nil {
			t.Fatalf("backupCommand: %v", err)
		}
		if lines := backupLines(t, out.String()); len(lines) != 1 || lines[0]["outcome"] != "ok" {
			t.Errorf("output lines %v, want one with outcome ok", lines)
		}
	})

	t.Run("The backup command waits for the lock, then fails", func(t *testing.T) {
		env := serviceEnv(t)
		dataFolder(t, env)
		holdBackupLock(t, env["BACKUP_DIR"])
		out := &syncBuffer{}
		err := backupCommand(context.Background(), func(name string) string { return env[name] }, out, 300*time.Millisecond)
		if err == nil {
			t.Fatal("backupCommand: want an error")
		}
		if lines := backupLines(t, out.String()); len(lines) != 1 || lines[0]["outcome"] != "locked" {
			t.Errorf("output lines %v, want one with outcome locked", lines)
		}
		if names := backupNames(t, env["BACKUP_DIR"]); len(names) != 0 {
			t.Errorf("backups %v, want none", names)
		}
	})

	t.Run("The backup command proceeds once the lock is released", func(t *testing.T) {
		env := serviceEnv(t)
		dataFolder(t, env)
		release := holdBackupLock(t, env["BACKUP_DIR"])
		out := &syncBuffer{}
		done := make(chan error, 1)
		go func() {
			done <- backupCommand(context.Background(), func(name string) string { return env[name] }, out, time.Minute)
		}()
		waitFor(t, func() bool { return strings.Contains(out.String(), `"msg":"waiting for the backup lock"`) }, "the command to wait for the lock")
		release()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("backupCommand: %v\n%s", err, out)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("the command never took the released lock")
		}
		if lines := backupLines(t, out.String()); len(lines) != 1 || lines[0]["outcome"] != "ok" {
			t.Errorf("output lines %v, want one with outcome ok", lines)
		}
		if names := backupNames(t, env["BACKUP_DIR"]); len(names) != 1 {
			t.Errorf("backups %v, want one", names)
		}
	})
}

// waitFor polls cond, bounded: a wait on a condition, never a sleep that hopes.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// nightlyEnv is a data folder with its database, a backup folder, and the nightly backup on a
// fake clock at now, its schedule 03:00 in Paris.
type nightlyEnv struct {
	clock     *scheduletest.Clock
	logs      *syncBuffer
	backupDir string
	opts      backupOptions
}

func newNightlyEnv(t *testing.T, now time.Time) *nightlyEnv {
	t.Helper()
	// time.Local is read once at start-up from TZ: the zone of the machine must not count.
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatal(err)
	}
	daily, err := schedule.ParseDaily("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	_, dbPath := storetest.OpenWithPath(t)
	dataDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(filepath.Join(dataDir, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	e := &nightlyEnv{clock: scheduletest.NewClock(now), logs: &syncBuffer{}, backupDir: t.TempDir()}
	e.opts = backupOptions{
		Schedule: daily, Location: paris, Clock: e.clock,
		Logger:   slog.New(slog.NewJSONHandler(e.logs, nil)),
		Database: dbPath, Assets: filepath.Join(dataDir, "assets"), Dir: e.backupDir, Version: "abc1234",
	}
	return e
}

func (e *nightlyEnv) start(t *testing.T) {
	t.Helper()
	stop := startBackup(context.Background(), e.opts)
	t.Cleanup(stop)
}

func TestNightlyBackup(t *testing.T) {
	t.Run("The nightly backup runs at the scheduled Paris time", func(t *testing.T) {
		e := newNightlyEnv(t, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))
		e.start(t)
		deadline := e.clock.Wait(t)
		if want := time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC); !deadline.Equal(want) {
			t.Fatalf("the schedule waits until %s, want %s", deadline, want)
		}
		if e.clock.Set(deadline.Add(-time.Second)) {
			t.Fatal("the schedule woke before its time")
		}
		if names := backupNames(t, e.backupDir); len(names) != 0 {
			t.Fatalf("backups %v before 03:00, want none", names)
		}
		e.clock.Set(deadline)
		e.clock.Wait(t) // the backup has run and the schedule waits for the next night
		names := backupNames(t, e.backupDir)
		if len(names) != 1 || names[0] != "2026-11-13T030000+0100" || !complete(t, e.backupDir, names[0]) {
			t.Fatalf("backups %v, want 2026-11-13T030000+0100", names)
		}
		raw, err := os.ReadFile(filepath.Join(e.backupDir, names[0], "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"trigger": "nightly"`) {
			t.Errorf("manifest %s, want trigger nightly", raw)
		}
		if !strings.Contains(e.logs.String(), `"msg":"backup scheduled","backup_schedule":"0 3 * * *","time_zone":"Europe/Paris","next_run":"2026-11-13T02:00:00Z"`) {
			t.Errorf("logs lack the start-up line:\n%s", e.logs)
		}
	})

	t.Run("A missed night is not caught up", func(t *testing.T) {
		e := newNightlyEnv(t, time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC))
		e.start(t)
		e.clock.Wait(t)
		e.clock.Set(time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)) // 09:00 in Paris
		next := e.clock.Wait(t)
		if want := time.Date(2026, 11, 14, 2, 0, 0, 0, time.UTC); !next.Equal(want) {
			t.Errorf("the schedule waits until %s, want %s", next, want)
		}
		if names := backupNames(t, e.backupDir); len(names) != 0 {
			t.Errorf("backups %v, want none", names)
		}
		if !strings.Contains(e.logs.String(), `"schedule":"backup","outcome":"missed_run"`) {
			t.Errorf("logs lack the missed run of the backup:\n%s", e.logs)
		}
	})

	t.Run("The nightly backup is skipped when another backup holds the lock", func(t *testing.T) {
		e := newNightlyEnv(t, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))
		release := holdBackupLock(t, e.backupDir)
		e.start(t)
		e.clock.Set(e.clock.Wait(t))
		next := e.clock.Wait(t)
		lines := backupLines(t, e.logs.String())
		if len(lines) != 1 || lines[0]["outcome"] != "skipped" || lines[0]["level"] != "WARN" {
			t.Errorf("log lines %v, want one skipped at warn", lines)
		}
		if names := backupNames(t, e.backupDir); len(names) != 0 {
			t.Errorf("backups %v, want none", names)
		}

		release()
		e.clock.Set(next)
		e.clock.Wait(t)
		if names := backupNames(t, e.backupDir); len(names) != 1 || names[0] != "2026-11-14T030000+0100" {
			t.Errorf("backups %v, want the next night's", names)
		}
	})
}

func TestBackupStartUp(t *testing.T) {
	t.Run("A schedule that is not daily refuses the start-up", func(t *testing.T) {
		env := serviceEnv(t)
		env["BACKUP_SCHEDULE"] = "0 3 * * 1"
		err := run(context.Background(), nil, func(name string) string { return env[name] }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "BACKUP_SCHEDULE") {
			t.Errorf("run() error = %v, want a BACKUP_SCHEDULE error", err)
		}
	})

	t.Run("The service refuses to start without a writable backup folder", func(t *testing.T) {
		env := serviceEnv(t)
		env["BACKUP_DIR"] = filepath.Join(t.TempDir(), "missing")
		err := run(context.Background(), nil, func(name string) string { return env[name] }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "backup folder") {
			t.Fatalf("run() error = %v, want an error about the backup folder", err)
		}
		if strings.Contains(err.Error(), env["BACKUP_DIR"]) {
			t.Errorf("error %q names the folder", err)
		}
		if _, err := os.Stat(env["BACKUP_DIR"]); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the backup folder was created: %v", err)
		}
		if _, err := os.Stat(filepath.Join(env["DATA_DIR"], "hippo.db")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the database was opened before the check: %v", err)
		}
	})

	t.Run("A backup folder inside the data folder is refused", func(t *testing.T) {
		env := serviceEnv(t)
		env["BACKUP_DIR"] = filepath.Join(env["DATA_DIR"], "backups")
		if err := os.MkdirAll(env["BACKUP_DIR"], 0o750); err != nil {
			t.Fatal(err)
		}
		err := run(context.Background(), nil, func(name string) string { return env[name] }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "BACKUP_DIR") {
			t.Errorf("run() error = %v, want a BACKUP_DIR error", err)
		}
	})
}

// capturedItem is what a capture answers that a restore must give back.
type capturedItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	IsPrivate bool   `json:"is_private"`
	Assets    []struct {
		ID        string `json:"id"`
		MimeType  string `json:"mime_type"`
		SizeBytes int64  `json:"size_bytes"`
	} `json:"assets"`
}

// api calls the service of env with the test token.
func api(t *testing.T, env map[string]string, method, path, contentType string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "http://"+env["HTTP_ADDR"]+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, content
}

// captureNote captures a note, or replays its capture, and returns the status and the item.
func captureNote(t *testing.T, env map[string]string, clientID, note string, private bool) (int, capturedItem) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"note": note, "is_private": private, "client_id": clientID})
	if err != nil {
		t.Fatal(err)
	}
	status, content := api(t, env, http.MethodPost, "/api/items", "application/json", body)
	return status, decodeItem(t, status, content)
}

// captureImage captures an image, or replays its capture, and returns the status and the item.
func captureImage(t *testing.T, env map[string]string, clientID string, image []byte) (int, capturedItem) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", "capture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("client_id", clientID); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	status, content := api(t, env, http.MethodPost, "/api/items", form.FormDataContentType(), body.Bytes())
	return status, decodeItem(t, status, content)
}

func decodeItem(t *testing.T, status int, content []byte) capturedItem {
	t.Helper()
	var item capturedItem
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("capture answered %d: %s", status, content)
	}
	if err := json.Unmarshal(content, &item); err != nil {
		t.Fatalf("capture answer: %v", err)
	}
	return item
}

// copyTree copies, never links, the folder src to dst, as the restore of the runbook does.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o640)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestore(t *testing.T) {
	t.Run("A restored backup serves the items and images as before", func(t *testing.T) {
		env := serviceEnv(t)
		stop := startService(t, env)
		ids := []string{ulid.Make().String(), ulid.Make().String(), ulid.Make().String()}
		image := imagetest.PNG(24, 16)
		_, public := captureNote(t, env, ids[0], "Une note publique fictive", false)
		_, private := captureNote(t, env, ids[1], "Une note privée fictive", true)
		_, captured := captureImage(t, env, ids[2], image)
		if len(captured.Assets) != 1 {
			t.Fatalf("image item %+v, want one asset", captured)
		}

		code, stdout, stderr := backupCommandProcess(t, env)
		if code != 0 {
			t.Fatalf("backup exit code %d, stderr %q", code, stderr)
		}
		name, _ := backupLines(t, stdout)[0]["backup"].(string)
		stop()

		// The restore of the runbook: an empty data folder, the backup's database and images
		// copied into it, nothing else.
		restored := serviceEnv(t)
		backupFolder := filepath.Join(env["BACKUP_DIR"], name)
		copyTree(t, filepath.Join(backupFolder, "assets"), filepath.Join(restored["DATA_DIR"], "assets"))
		db, err := os.ReadFile(filepath.Join(backupFolder, "hippo.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(restored["DATA_DIR"], "hippo.db"), db, 0o640); err != nil {
			t.Fatal(err)
		}
		startService(t, restored)

		// Each item read back by replaying its capture (RG-08): the same item, not a new one.
		for _, c := range []struct {
			want   capturedItem
			replay func() (int, capturedItem)
		}{
			{public, func() (int, capturedItem) {
				return captureNote(t, restored, ids[0], "Une note publique fictive", false)
			}},
			{private, func() (int, capturedItem) { return captureNote(t, restored, ids[1], "Une note privée fictive", true) }},
			{captured, func() (int, capturedItem) { return captureImage(t, restored, ids[2], image) }},
		} {
			status, got := c.replay()
			if status != http.StatusOK || got.ID != c.want.ID || got.Kind != c.want.Kind || got.Title != c.want.Title ||
				got.IsPrivate != c.want.IsPrivate || len(got.Assets) != len(c.want.Assets) {
				t.Errorf("replay answered %d %+v, want 200 %+v", status, got, c.want)
			}
		}
		if !private.IsPrivate {
			t.Error("the private note was not captured as private")
		}
		status, content := api(t, restored, http.MethodGet, "/api/assets/"+captured.Assets[0].ID, "", nil)
		if status != http.StatusOK || !bytes.Equal(content, image) {
			t.Errorf("GET the image answered %d with %d bytes, want 200 with the captured bytes", status, len(content))
		}
	})
}
