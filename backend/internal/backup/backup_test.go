package backup_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/backup"
	"github.com/ghislain95/hippo/backend/internal/freespace"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// The scenarios of openspec/changes/nightly-backup/specs/backup/spec.md that internal/backup
// answers alone; those of the command and the schedule are in cmd/hippo.

func TestBackupContent(t *testing.T) {
	t.Run("A backup holds the database and the images", func(t *testing.T) {
		f := newFixture(t)
		noteID := f.note(t, "Une note fictive", false)
		imageID, rel := f.image(t, "Une capture fictive", pngOf(1), false)

		res := f.run(t)

		name := f.nameAt(start)
		if got := f.backups(t); !slices.Equal(got, []string{name}) || res.Name != name {
			t.Fatalf("backups = %v (result %q), want one named %s", got, res.Name, name)
		}
		dir := filepath.Join(f.backupDir, name)
		for _, entry := range []string{"hippo.db", "assets", "manifest.json"} {
			if !exists(t, filepath.Join(dir, entry)) {
				t.Errorf("the backup lacks %s", entry)
			}
		}
		db := filepath.Join(dir, "hippo.db")
		if got := storetest.IntegrityCheck(t, db); got != "ok" {
			t.Errorf("integrity check = %q, want ok", got)
		}
		if n := storetest.ItemCount(t, db, f.userID); n != 2 {
			t.Errorf("the backup holds %d items, want 2 (%s, %s)", n, noteID, imageID)
		}
		images := 0
		for p, mode := range tree(t, filepath.Join(dir, "assets")) {
			if !mode.IsRegular() {
				continue
			}
			images++
			if !bytes.Equal(readFile(t, filepath.Join(dir, "assets", p)), readFile(t, filepath.Join(f.assetsDir, p))) {
				t.Errorf("image %s differs from the images folder", p)
			}
		}
		if images != 1 || !exists(t, filepath.Join(dir, "assets", rel)) {
			t.Errorf("the backup holds %d images, want the one at its relative path", images)
		}
	})

	t.Run("A private item is backed up like the others", func(t *testing.T) {
		f := newFixture(t)
		id, rel := f.image(t, "Une capture privée fictive", pngOf(2), true)
		f.run(t)
		dir := filepath.Join(f.backupDir, f.nameAt(start))
		copyOf := storetest.ItemCount(t, filepath.Join(dir, "hippo.db"), f.userID)
		if copyOf != 1 {
			t.Errorf("the backup holds %d items, want the private item %s", copyOf, id)
		}
		if !bytes.Equal(readFile(t, filepath.Join(dir, "assets", rel)), pngOf(2)) {
			t.Error("the backup lacks the image of the private item")
		}
	})

	t.Run("The backup name is the Paris time with its offset", func(t *testing.T) {
		tokyo, err := time.LoadLocation("Asia/Tokyo")
		if err != nil {
			t.Fatal(err)
		}
		// time.Local is read once at start-up from TZ: setting TZ here would prove nothing.
		for _, local := range []*time.Location{time.UTC, tokyo} {
			t.Run(local.String(), func(t *testing.T) {
				saved := time.Local
				time.Local = local
				t.Cleanup(func() { time.Local = saved })
				f := newFixture(t)
				f.set(time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC))
				first := f.run(t)
				f.set(time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC))
				second := f.run(t)
				if first.Name != "2026-07-01T030000+0200" || second.Name != "2026-11-13T030000+0100" {
					t.Errorf("names %q and %q", first.Name, second.Name)
				}
				if got := f.backups(t); !slices.Equal(got, []string{"2026-07-01T030000+0200", "2026-11-13T030000+0100"}) {
					t.Errorf("backups = %v", got)
				}
			})
		}
	})

	t.Run("Backup files are not readable by others", func(t *testing.T) {
		// The umask of the container (022) is the one the modes must hold under.
		saved := syscall.Umask(0o022)
		t.Cleanup(func() { syscall.Umask(saved) })
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(3), false)
		f.run(t)
		dir := filepath.Join(f.backupDir, f.nameAt(start))
		entries := tree(t, dir)
		if len(entries) < 5 { // the backup, hippo.db, manifest.json, assets/, the user's folder, the image
			t.Fatalf("backup tree %v", entries)
		}
		for rel, mode := range entries {
			switch {
			case mode.IsDir() && mode.Perm() != 0o750:
				t.Errorf("folder %s has mode %o, want 0750", rel, mode.Perm())
			case mode.IsRegular() && mode.Perm() != 0o640:
				t.Errorf("file %s has mode %o, want 0640", rel, mode.Perm())
			case !mode.IsDir() && !mode.IsRegular():
				t.Errorf("%s is neither a file nor a folder (%s)", rel, mode)
			}
		}
	})

	t.Run("A write in progress is not in the backup", func(t *testing.T) {
		f := newFixture(t)
		f.note(t, "Une note validée", false)
		id, err := f.st.NewID()
		if err != nil {
			t.Fatal(err)
		}
		rollback := storetest.HoldItemInsert(t, f.dbPath, f.userID, id)
		res, err := backup.Run(context.Background(), f.options())
		rollback()
		if err != nil || res.Outcome != backup.OutcomeOK {
			t.Fatalf("Run = %+v, %v", res, err)
		}
		db := filepath.Join(f.backupDir, res.Name, "hippo.db")
		if got := storetest.IntegrityCheck(t, db); got != "ok" {
			t.Errorf("integrity check = %q, want ok", got)
		}
		if n := storetest.ItemCount(t, db, f.userID); n != 1 {
			t.Errorf("the backup holds %d items, want the committed one", n)
		}
	})
}

func TestBackupLeavesTheData(t *testing.T) {
	t.Run("The database is left unchanged", func(t *testing.T) {
		f := newFixture(t)
		f.note(t, "Une note fictive", false)
		f.image(t, "Une capture fictive", pngOf(4), false)
		wal := f.dbPath + "-wal"
		before := map[string][]byte{f.dbPath: readFile(t, f.dbPath), wal: readFile(t, wal)}
		if len(before[wal]) == 0 {
			t.Fatal("no committed write in the write-ahead log: the test would prove nothing")
		}
		f.run(t)
		for path, content := range before {
			if !bytes.Equal(readFile(t, path), content) {
				t.Errorf("%s changed during the backup", filepath.Base(path))
			}
		}
	})

	t.Run("A database behind the binary is not migrated", func(t *testing.T) {
		f := newEmptyFixture(t)
		storetest.CreateAtVersion(t, f.dbPath, 5)
		res := f.run(t)
		if got := storetest.MigrationVersion(t, f.dbPath); got != 5 {
			t.Errorf("the database is at migration %d after the backup, want 5", got)
		}
		if got := number(t, f.manifest(t, res.Name), "migration_version"); got != 5 || res.MigrationVersion != 5 {
			t.Errorf("manifest migration_version = %d (result %d), want 5", got, res.MigrationVersion)
		}
	})

	t.Run("Temporary files stay in place and out of the backup", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(5), false)
		upload := filepath.Join(f.assetsDir, ".tmp-upload")
		nested := filepath.Join(f.assetsDir, f.userID, ".tmp-nested")
		left := filepath.Join(f.dataDir, "tmp", ".pdf-leftover")
		files := map[string][]byte{upload: []byte("upload in progress"), nested: []byte("nested temporary"), left: []byte("%PDF-1.7 left")}
		for path, content := range files {
			writeFile(t, path, content)
		}
		res := f.run(t)
		for path, content := range files {
			if !bytes.Equal(readFile(t, path), content) {
				t.Errorf("%s changed or removed", filepath.Base(path))
			}
		}
		dir := filepath.Join(f.backupDir, res.Name)
		for rel := range tree(t, dir) {
			if strings.HasPrefix(filepath.Base(rel), ".tmp-") || strings.HasPrefix(rel, "tmp") {
				t.Errorf("the backup holds %s", rel)
			}
		}
		for _, content := range files {
			if contains(t, dir, content) {
				t.Errorf("the backup holds the content of a temporary file")
			}
		}
	})

	t.Run("A symbolic link in the images folder is not followed", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(6), false)
		outside := t.TempDir()
		secret := []byte("content outside the images folder")
		writeFile(t, filepath.Join(outside, "secret.png"), secret)
		if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(f.assetsDir, f.userID, "link.png")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(f.assetsDir, "linked-folder")); err != nil {
			t.Fatal(err)
		}
		res := f.run(t)
		dir := filepath.Join(f.backupDir, res.Name)
		for rel, mode := range tree(t, dir) {
			if mode&fs.ModeSymlink != 0 || strings.Contains(rel, "link") {
				t.Errorf("the backup holds %s (%s)", rel, mode)
			}
		}
		if contains(t, dir, secret) {
			t.Error("the backup holds the content of the link's target")
		}
	})
}

func TestBackupImages(t *testing.T) {
	t.Run("An unchanged image is linked from the previous backup", func(t *testing.T) {
		f := newFixture(t)
		_, rel := f.image(t, "Une capture fictive", pngOf(7), false)
		first := f.run(t)
		f.set(start.Add(24 * time.Hour))
		second := f.run(t)
		if !sameFile(t, filepath.Join(f.backupDir, first.Name, "assets", rel), filepath.Join(f.backupDir, second.Name, "assets", rel)) {
			t.Error("the image of the new backup is not the previous backup's file")
		}
		m := f.manifest(t, second.Name)
		if number(t, m, "images_linked") != 1 || number(t, m, "images_copied") != 0 {
			t.Errorf("manifest %v, want 1 linked and 0 copied", m)
		}
	})

	t.Run("A changed image is copied", func(t *testing.T) {
		f := newFixture(t)
		_, rel := f.image(t, "Une capture fictive", pngOf(8), false)
		first := f.run(t)
		source := filepath.Join(f.assetsDir, rel)
		changed := pngOf(9)
		writeFile(t, source, changed)
		modified := time.Date(2026, 11, 13, 10, 0, 0, 123456789, time.UTC)
		if err := os.Chtimes(source, time.Time{}, modified); err != nil {
			t.Fatal(err)
		}
		f.set(start.Add(24 * time.Hour))
		second := f.run(t)
		copied := filepath.Join(f.backupDir, second.Name, "assets", rel)
		if sameFile(t, filepath.Join(f.backupDir, first.Name, "assets", rel), copied) {
			t.Error("the changed image is linked to the previous backup's file")
		}
		info, err := os.Stat(copied)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(readFile(t, copied), changed) || !info.ModTime().Equal(modified) {
			t.Errorf("the copy holds other bytes or the time %s, want %s", info.ModTime(), modified)
		}
		if m := f.manifest(t, second.Name); number(t, m, "images_copied") != 1 || number(t, m, "images_linked") != 0 {
			t.Errorf("manifest %v, want 1 copied and 0 linked", m)
		}
	})

	t.Run("A backup image is never the data folder's file", func(t *testing.T) {
		f := newFixture(t)
		original := pngOf(10)
		_, rel := f.image(t, "Une capture fictive", original, false)
		res := f.run(t)
		source := filepath.Join(f.assetsDir, rel)
		copied := filepath.Join(f.backupDir, res.Name, "assets", rel)
		if sameFile(t, source, copied) {
			t.Fatal("the backup's image is the images folder's file")
		}
		// Rewritten in place: same inode, other bytes.
		file, err := os.OpenFile(source, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(pngOf(11)); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(readFile(t, copied), original) {
			t.Error("the backup's image changed with the images folder's file")
		}
	})

	t.Run("A failed link falls back to a copy", func(t *testing.T) {
		f := newFixture(t)
		_, rel := f.image(t, "Une capture fictive", pngOf(12), false)
		first := f.run(t)
		f.set(start.Add(24 * time.Hour))
		opts := f.options()
		backup.SetLink(&opts, func(*os.Root, string, string) error {
			return &os.LinkError{Op: "link", Old: "old", New: "new", Err: syscall.EMLINK}
		})
		second, err := backup.Run(context.Background(), opts)
		if err != nil || second.Outcome != backup.OutcomeOK {
			t.Fatalf("Run = %+v, %v; want ok", second, err)
		}
		copied := filepath.Join(f.backupDir, second.Name, "assets", rel)
		if sameFile(t, filepath.Join(f.backupDir, first.Name, "assets", rel), copied) {
			t.Error("the image is the previous backup's file")
		}
		if !bytes.Equal(readFile(t, copied), pngOf(12)) {
			t.Error("the copy holds other bytes")
		}
		if m := f.manifest(t, second.Name); number(t, m, "images_copied") != 1 || number(t, m, "images_linked") != 0 {
			t.Errorf("manifest %v, want 1 copied and 0 linked", m)
		}
	})

	t.Run("An image removed during the backup is counted as gone", func(t *testing.T) {
		f := newFixture(t)
		_, kept := f.image(t, "Une capture gardée", pngOf(13), false)
		_, removed := f.image(t, "Une capture supprimée", pngOf(14), false)
		opts := f.options()
		backup.AfterDatabase(&opts, func() {
			if err := os.Remove(filepath.Join(f.assetsDir, removed)); err != nil {
				t.Error(err)
			}
		})
		res, err := backup.Run(context.Background(), opts)
		if err != nil || res.Outcome != backup.OutcomeOK {
			t.Fatalf("Run = %+v, %v; want ok", res, err)
		}
		dir := filepath.Join(f.backupDir, res.Name, "assets")
		if !exists(t, filepath.Join(dir, kept)) || exists(t, filepath.Join(dir, removed)) {
			t.Error("the backup does not hold the other image only")
		}
		if m := f.manifest(t, res.Name); number(t, m, "images_gone") != 1 || number(t, m, "images") != 1 {
			t.Errorf("manifest %v, want 1 image and 1 gone", m)
		}
		lines := f.logLines(t, "backup")
		if len(lines) != 1 || number(t, lines[0], "images_gone") != 1 {
			t.Errorf("log lines %v, want one counting 1 image gone", lines)
		}
	})
}

func TestBackupCompleteness(t *testing.T) {
	t.Run("A backup in progress is not visible under its final name", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(15), false)
		name := f.nameAt(start)
		opts := f.options()
		seen := false
		backup.AfterDatabase(&opts, func() {
			seen = true
			if !exists(t, filepath.Join(f.backupDir, ".partial-"+name)) || exists(t, filepath.Join(f.backupDir, name)) {
				t.Errorf("during the backup the folder holds %v, want .partial-%s and no %s", f.entries(t), name, name)
			}
		})
		if res, err := backup.Run(context.Background(), opts); err != nil || res.Outcome != backup.OutcomeOK {
			t.Fatalf("Run = %+v, %v; want ok", res, err)
		}
		if !seen {
			t.Fatal("the backup never reached its images")
		}
	})

	t.Run("A temporary folder left by a crash is removed by the next backup", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(16), false)
		leftover := filepath.Join(f.backupDir, ".partial-2026-11-12T030000+0100")
		writeFile(t, filepath.Join(leftover, "hippo.db"), []byte("half a database"))
		notes := filepath.Join(f.backupDir, "notes.txt")
		oldCopy := filepath.Join(f.backupDir, "old-copy", "hippo.db")
		writeFile(t, notes, []byte("notes"))
		writeFile(t, oldCopy, []byte("an old copy"))
		res := f.run(t)
		if exists(t, leftover) {
			t.Error("the .partial- folder is still there")
		}
		if !bytes.Equal(readFile(t, notes), []byte("notes")) || !bytes.Equal(readFile(t, oldCopy), []byte("an old copy")) {
			t.Error("notes.txt or old-copy changed")
		}
		if !exists(t, filepath.Join(f.backupDir, res.Name, "manifest.json")) {
			t.Error("the new backup is not complete")
		}
		if lines := f.logLines(t, "backup"); len(lines) != 1 || number(t, lines[0], "leftovers_removed") != 1 {
			t.Errorf("log lines %v, want one counting 1 leftover removed", lines)
		}
	})

	t.Run("A name already taken fails the backup", func(t *testing.T) {
		f := newFixture(t)
		taken := filepath.Join(f.backupDir, f.nameAt(start))
		writeFile(t, filepath.Join(taken, "marker"), []byte("already here"))
		res, err := backup.Run(context.Background(), f.options())
		if !errors.Is(err, backup.ErrNameTaken) || res.Outcome != backup.OutcomeNameTaken {
			t.Fatalf("Run = %+v, %v; want name_taken", res, err)
		}
		if got := tree(t, taken); len(got) != 2 || !bytes.Equal(readFile(t, filepath.Join(taken, "marker")), []byte("already here")) {
			t.Errorf("the existing folder changed: %v", got)
		}
		if p := f.partials(t); len(p) != 0 {
			t.Errorf("left %v", p)
		}
	})

	t.Run("A failed write removes the temporary folder", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(17), false)
		f.set(start.Add(-48 * time.Hour))
		older := f.run(t)
		f.set(start.Add(-24 * time.Hour))
		previous := f.run(t)
		f.image(t, "Une autre capture", pngOf(18), false)
		f.set(start)
		opts := f.options()
		backup.FailWritesAfterDatabase(&opts)
		res, err := backup.Run(context.Background(), opts)
		if err == nil || res.Outcome != backup.OutcomeFailed {
			t.Fatalf("Run = %+v, %v; want failed", res, err)
		}
		if got := f.backups(t); !slices.Equal(got, []string{older.Name, previous.Name}) {
			t.Errorf("backups = %v, want the previous two only", got)
		}
		if p := f.partials(t); len(p) != 0 {
			t.Errorf("left %v", p)
		}
		for _, name := range []string{older.Name, previous.Name} {
			if !exists(t, filepath.Join(f.backupDir, name, "manifest.json")) {
				t.Errorf("backup %s damaged", name)
			}
		}
	})

	t.Run("A stop during the backup removes the temporary folder", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(19), false)
		f.image(t, "Une autre capture", pngOf(20), false)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		opts := f.options()
		backup.AfterDatabase(&opts, cancel)
		res, err := backup.Run(ctx, opts)
		if err == nil || res.Outcome != backup.OutcomeStopped {
			t.Fatalf("Run = %+v, %v; want stopped", res, err)
		}
		if b, p := f.backups(t), f.partials(t); len(b) != 0 || len(p) != 0 {
			t.Errorf("left backups %v and partial folders %v", b, p)
		}
	})
}

func TestBackupRoom(t *testing.T) {
	t.Run("A backup that cannot fit does not start", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(21), false)
		opts := f.options()
		backup.SetFreeSpace(&opts, func(string) (uint64, bool, error) { return freespace.Margin, true, nil })
		res, err := backup.Run(context.Background(), opts)
		if !errors.Is(err, backup.ErrNoRoom) || res.Outcome != backup.OutcomeNoRoom {
			t.Fatalf("Run = %+v, %v; want no_room", res, err)
		}
		if b, p := f.backups(t), f.partials(t); len(b) != 0 || len(p) != 0 {
			t.Errorf("left backups %v and partial folders %v", b, p)
		}
	})

	t.Run("Linked images do not count toward the space needed", func(t *testing.T) {
		f := newFixture(t)
		f.image(t, "Une capture fictive", pngOf(22), false)
		f.image(t, "Une autre capture", pngOf(23), false)
		database := uint64(size(t, f.dbPath) + size(t, f.dbPath+"-wal"))
		tight := func(string) (uint64, bool, error) { return database + freespace.Margin, true, nil }

		// Without a previous backup the images must be copied: they do not fit.
		opts := f.options()
		backup.SetFreeSpace(&opts, tight)
		if res, err := backup.Run(context.Background(), opts); res.Outcome != backup.OutcomeNoRoom {
			t.Fatalf("first Run = %+v, %v; want no_room", res, err)
		}
		f.run(t)
		f.set(start.Add(24 * time.Hour))
		opts = f.options()
		backup.SetFreeSpace(&opts, tight)
		res, err := backup.Run(context.Background(), opts)
		if err != nil || res.Outcome != backup.OutcomeOK {
			t.Fatalf("Run = %+v, %v; want ok", res, err)
		}
		if res.ImagesLinked != 2 || res.ImagesCopied != 0 {
			t.Errorf("result %+v, want every image linked", res)
		}
	})
}

func TestBackupLock(t *testing.T) {
	t.Run("The lock of a killed process is released", func(t *testing.T) {
		f := newFixture(t)
		child := exec.Command(os.Args[0], "-test.run=^$")
		child.Env = append(os.Environ(), holdLockEnv+"="+f.backupDir)
		stdin, err := child.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stdin.Close() }()
		stdout, err := child.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil || line != "locked\n" {
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatalf("child said %q, %v; want locked", line, err)
		}
		opts := f.options()
		opts.Trigger = backup.TriggerNightly
		if res, err := backup.Run(context.Background(), opts); !errors.Is(err, backup.ErrLocked) || res.Outcome != backup.OutcomeSkipped {
			t.Fatalf("Run while the child holds the lock = %+v, %v; want skipped", res, err)
		}

		if err := child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = child.Wait() // killed: the error is the signal
		res, err := backup.Run(context.Background(), opts)
		if err != nil || res.Outcome != backup.OutcomeOK {
			t.Fatalf("Run after the kill = %+v, %v; want ok", res, err)
		}
	})

	t.Run("a second open of the lock in the same process cannot take it", func(t *testing.T) {
		dir := t.TempDir()
		release, err := backup.HoldLock(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backup.HoldLock(dir); !errors.Is(err, backup.ErrLocked) {
			t.Errorf("second HoldLock = %v, want ErrLocked", err)
		}
		if err := release(); err != nil {
			t.Fatal(err)
		}
		again, err := backup.HoldLock(dir)
		if err != nil {
			t.Fatalf("HoldLock after the release: %v", err)
		}
		if err := again(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("the command waits for the lock, then fails with outcome locked", func(t *testing.T) {
		f := newFixture(t)
		release, err := backup.HoldLock(f.backupDir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = release() }()
		opts := f.options()
		opts.LockWait = 50 * time.Millisecond
		res, err := backup.Run(context.Background(), opts)
		if !errors.Is(err, backup.ErrLocked) || res.Outcome != backup.OutcomeLocked {
			t.Fatalf("Run = %+v, %v; want locked", res, err)
		}
		if b := f.backups(t); len(b) != 0 {
			t.Errorf("backups %v, want none", b)
		}
	})
}

func TestBackupRetention(t *testing.T) {
	// made makes, as a backup would have, an empty backup started days before start.
	made := func(t *testing.T, f *fixture, days int) string {
		t.Helper()
		name := f.nameAt(start.Add(-time.Duration(days) * 24 * time.Hour))
		writeFile(t, filepath.Join(f.backupDir, name, "manifest.json"), []byte("{}"))
		return name
	}

	t.Run("Backups older than 14 days are removed after a successful backup", func(t *testing.T) {
		f := newFixture(t)
		names := map[int]string{}
		for _, days := range []int{20, 15, 13, 2, 1} {
			names[days] = made(t, f, days)
		}
		res := f.run(t)
		want := []string{names[13], names[2], names[1], res.Name}
		if got := f.backups(t); !slices.Equal(got, want) {
			t.Errorf("backups = %v, want %v", got, want)
		}
		if lines := f.logLines(t, "backup"); len(lines) != 1 || number(t, lines[0], "backups_removed") != 2 {
			t.Errorf("log lines %v, want one counting 2 removed", lines)
		}
	})

	t.Run("The 3 most recent backups are kept whatever their age", func(t *testing.T) {
		f := newFixture(t)
		names := map[int]string{}
		for _, days := range []int{33, 32, 31, 30} {
			names[days] = made(t, f, days)
		}
		res := f.run(t)
		if got, want := f.backups(t), []string{names[31], names[30], res.Name}; !slices.Equal(got, want) {
			t.Errorf("backups = %v, want %v", got, want)
		}
		if res.BackupsRemoved != 2 {
			t.Errorf("result %+v, want 2 removed", res)
		}
	})

	t.Run("Only folders in the backup format are touched", func(t *testing.T) {
		f := newFixture(t)
		old := start.Add(-30 * 24 * time.Hour)
		paths := map[string][]byte{
			filepath.Join(f.backupDir, "notes.txt"):                          []byte("notes"),
			filepath.Join(f.backupDir, "old-copy", "hippo.db"):               []byte("an old copy"),
			filepath.Join(f.backupDir, "2026-01-01", "hippo.db"):             []byte("a dated copy"),
			filepath.Join(f.backupDir, f.nameAt(old.Add(-24*time.Hour))):     []byte("a file named like a backup"),
			filepath.Join(f.backupDir, f.nameAt(old.Add(-48*time.Hour))+"x"): []byte("a name almost in the format"),
		}
		for path, content := range paths {
			writeFile(t, path, content)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
		res := f.run(t)
		for path, content := range paths {
			if !bytes.Equal(readFile(t, path), content) {
				t.Errorf("%s changed", filepath.Base(path))
			}
		}
		if res.BackupsRemoved != 0 {
			t.Errorf("result %+v, want nothing removed", res)
		}
	})
}

func TestBackupReport(t *testing.T) {
	t.Run("The manifest describes the backup", func(t *testing.T) {
		f := newFixture(t)
		_, unchanged := f.image(t, "Une capture inchangée", pngOf(24), false)
		f.run(t)
		f.set(start.Add(24 * time.Hour))
		added := pngOf(25)
		_, newRel := f.image(t, "Une capture nouvelle", added, false)
		res := f.run(t)
		raw := readFile(t, filepath.Join(f.backupDir, res.Name, "manifest.json"))
		m := f.manifest(t, res.Name)
		want := map[string]any{
			"format":              float64(1),
			"name":                res.Name,
			"started_at":          "2026-11-14T03:00:00+01:00",
			"trigger":             "command",
			"backend_version":     "abc1234",
			"migration_version":   float64(storetest.MigrationVersion(t, f.dbPath)),
			"database_bytes":      float64(size(t, filepath.Join(f.backupDir, res.Name, "hippo.db"))),
			"images":              float64(2),
			"images_copied":       float64(1),
			"images_linked":       float64(1),
			"images_gone":         float64(0),
			"image_bytes_written": float64(len(added)),
		}
		if len(m) != len(want) {
			t.Errorf("manifest has %d fields, want %d: %s", len(m), len(want), raw)
		}
		for key, value := range want {
			if m[key] != value {
				t.Errorf("manifest %s = %v, want %v", key, m[key], value)
			}
		}
		for _, secret := range []string{unchanged, newRel, filepath.Base(unchanged), "Une capture"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("the manifest holds %q", secret)
			}
		}
		if mode := info(t, filepath.Join(f.backupDir, res.Name, "manifest.json")).Mode().Perm(); mode&0o007 != 0 {
			t.Errorf("manifest mode %o, readable by others", mode)
		}
	})

	t.Run("One log line per backup, without file names nor content", func(t *testing.T) {
		f := newFixture(t)
		title := "Un titre fictif à ne jamais loguer"
		_, rel := f.image(t, title, pngOf(26), false)
		res := f.run(t)
		lines := f.logLines(t, "backup")
		if len(lines) != 1 {
			t.Fatalf("%d log lines backup, want 1:\n%s", len(lines), f.logs)
		}
		line := lines[0]
		if line["outcome"] != "ok" || line["trigger"] != "command" || line["backup"] != res.Name || line["backend_version"] != "abc1234" {
			t.Errorf("log line %v", line)
		}
		for _, key := range []string{
			"migration_version", "database_bytes", "images", "images_copied", "images_linked", "images_gone",
			"image_bytes_written", "duration_ms", "backups_removed", "leftovers_removed",
		} {
			number(t, line, key)
		}
		for _, secret := range []string{rel, filepath.Base(rel), strings.TrimSuffix(filepath.Base(rel), ".png"), title, f.assetsDir, f.dataDir} {
			if strings.Contains(f.logs.String(), secret) {
				t.Errorf("the logs hold %q", secret)
			}
		}
	})
}

func size(t *testing.T, path string) int64 {
	t.Helper()
	return info(t, path).Size()
}

func info(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func TestBackupErrors(t *testing.T) {
	t.Run("a name that cannot be checked fails the backup, not as name_taken", func(t *testing.T) {
		f := newFixture(t)
		opts := f.options()
		backup.SetFreeSpace(&opts, func(string) (uint64, bool, error) {
			// Readable but not searchable: the check of the name gets EACCES, not ENOENT.
			if err := os.Chmod(f.backupDir, 0o600); err != nil {
				t.Error(err)
			}
			return 1 << 40, true, nil
		})
		t.Cleanup(func() { _ = os.Chmod(f.backupDir, 0o750) })
		res, err := backup.Run(context.Background(), opts)
		if err == nil || errors.Is(err, backup.ErrNameTaken) || res.Outcome != backup.OutcomeFailed {
			t.Fatalf("Run = %+v, %v; want failed", res, err)
		}
		if strings.Contains(err.Error(), f.backupDir) {
			t.Errorf("error %q names the folder", err)
		}
	})
}

func TestCheckDir(t *testing.T) {
	t.Run("a writable folder is accepted and keeps only its lock file", func(t *testing.T) {
		dir := t.TempDir()
		if err := backup.CheckDir(dir); err != nil {
			t.Fatalf("CheckDir: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != ".lock" {
			t.Errorf("the folder holds %v, want .lock only", entries)
		}
	})

	t.Run("a missing folder is refused, and not created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing")
		err := backup.CheckDir(dir)
		if err == nil || strings.Contains(err.Error(), dir) {
			t.Errorf("CheckDir = %v, want an error naming no file", err)
		}
		if exists(t, dir) {
			t.Error("the folder was created")
		}
	})

	t.Run("a folder that cannot be written is refused, even with its lock file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".lock"), nil)
		if err := os.Chmod(dir, 0o550); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		err := backup.CheckDir(dir)
		if err == nil || strings.Contains(err.Error(), dir) {
			t.Errorf("CheckDir = %v, want an error naming no file", err)
		}
	})
}
