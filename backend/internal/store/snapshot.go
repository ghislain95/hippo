package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot copies the database at dbPath to a new file at outPath, consistent and without ever
// writing the source, and returns the last migration applied to the copy (nightly-backup design
// D2, D3). It is the database step of a backup, which runs beside the live service:
//
//   - its own connection, read-only (mode=ro), one at a time, with the busy timeout only: no
//     write transaction, no journal mode, no migration, no fill of Open;
//   - VACUUM INTO, which opens a read transaction only on the source, so the copy holds what was
//     committed when it started and an uncommitted write of the service is not in it; it refuses
//     a target that exists and is not empty;
//   - the copy is synced, then read back read-only for its migration version (goose reads
//     goose_db_version only).
//
// The copy is in rollback-journal mode; the service turns it back to WAL when it opens it after
// a restore. Its errors name no file.
func Snapshot(ctx context.Context, dbPath, outPath string) (migrationVersion int64, err error) {
	if err := vacuumInto(ctx, dbPath, outPath); err != nil {
		return 0, err
	}
	if err := syncFile(outPath); err != nil {
		return 0, err
	}
	return copyMigrationVersion(ctx, outPath)
}

// openSnapshotSource opens the source of a snapshot read-only, one connection, waiting for the
// service's locks rather than failing. mode=ro never creates the file.
func openSnapshotSource(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", readOnlyDSN(path, true))
	if err != nil {
		return nil, fmt.Errorf("open database read-only: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// readOnlyDSN is the URI of a read-only connection to path, with the busy timeout when wait.
func readOnlyDSN(path string, wait bool) string {
	query := url.Values{}
	query.Set("mode", "ro")
	if wait {
		query.Add("_pragma", "busy_timeout(5000)")
	}
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}
	return uri.String()
}

func vacuumInto(ctx context.Context, dbPath, outPath string) (err error) {
	db, err := openSnapshotSource(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database: %w", closeErr)
		}
	}()
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, outPath); err != nil {
		// SQLite's message names the file it could not open: « unable to open database: <path> ».
		return fmt.Errorf("copy database: %w", withoutPaths(err, outPath, dbPath))
	}
	return nil
}

// syncFile flushes the copy to the disk: VACUUM INTO syncs it by the source's pager flags, which
// a backup does not rely on.
func syncFile(path string) error {
	f, err := os.Open(path) //nolint:gosec // the path of the copy, built by the backup
	if err != nil {
		return fmt.Errorf("sync database copy: %w", pathless(err))
	}
	syncErr := f.Sync()
	if err := f.Close(); syncErr == nil && err != nil {
		syncErr = err
	}
	if syncErr != nil {
		return fmt.Errorf("sync database copy: %w", pathless(syncErr))
	}
	return nil
}

// copyMigrationVersion reads the last migration applied to the copy at path, read-only: the
// version of exactly what was copied.
func copyMigrationVersion(ctx context.Context, path string) (version int64, err error) {
	db, err := sql.Open("sqlite", readOnlyDSN(path, false))
	if err != nil {
		return 0, fmt.Errorf("open database copy: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database copy: %w", closeErr)
		}
	}()
	db.SetMaxOpenConns(1)
	provider, err := migrationProvider(db)
	if err != nil {
		return 0, err
	}
	version, err = provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read migration version of the copy: %w", withoutPaths(err, path))
	}
	return version, nil
}

// withoutPaths is err with every path of paths, and the folder of each, cut out of its message:
// the errors of a snapshot reach the logs and the output of `hippo backup`, which name no file
// (nightly-backup design D12). It still unwraps to err, for errors.Is.
func withoutPaths(err error, paths ...string) error {
	msg := err.Error()
	for _, p := range paths {
		if p != "" {
			msg = strings.ReplaceAll(msg, p, "<file>")
		}
		// The folder alone, only when absolute and followed by a separator: a short relative
		// folder such as "data" would otherwise cut words of the message ("database").
		if dir := filepath.Dir(p); filepath.IsAbs(dir) && dir != string(filepath.Separator) {
			msg = strings.ReplaceAll(msg, dir+string(filepath.Separator), "<folder>"+string(filepath.Separator))
		}
	}
	if msg == err.Error() {
		return err
	}
	return &redacted{msg: msg, err: err}
}

// redacted is an error whose message lost its paths.
type redacted struct {
	msg string
	err error
}

func (e *redacted) Error() string { return e.msg }
func (e *redacted) Unwrap() error { return e.err }

// pathless drops the file name of a file system error, keeping its cause.
func pathless(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// CreateAtVersion creates the database at path, or opens it, and applies the migrations up to
// version only, without the start-up fill of Open. Exported for storetest, which builds a
// database behind the binary for the tests of a backup (nightly-backup); the service always
// opens through Open.
func CreateAtVersion(ctx context.Context, path string, version int64) (err error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database: %w", closeErr)
		}
	}()
	provider, err := migrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("apply migrations up to %d: %w", version, err)
	}
	return nil
}
