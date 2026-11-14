// Package backup makes a backup of Hippo on the NAS: a dated folder of the backup folder
// (BACKUP_DIR) holding a consistent copy of the database, the images and a manifest, every
// night and on demand with `hippo backup` (RG-98, ADR-0039, nightly-backup design D1).
package backup

import (
	"errors"
	"log/slog"
	"os"
	"time"
)

// Trigger is what started a backup.
type Trigger string

// Triggers of a backup: the schedule of the service, or the command `hippo backup`.
const (
	TriggerNightly Trigger = "nightly"
	TriggerCommand Trigger = "command"
)

// Outcome is how a backup ended, as its log line says (design D12).
type Outcome string

// Outcomes of a backup.
const (
	OutcomeOK        Outcome = "ok"
	OutcomeSkipped   Outcome = "skipped"    // the nightly backup found the lock taken
	OutcomeLocked    Outcome = "locked"     // the command waited for the lock in vain
	OutcomeNoRoom    Outcome = "no_room"    // the backup would not fit, nothing written
	OutcomeNameTaken Outcome = "name_taken" // a folder already has the backup's name
	OutcomeFailed    Outcome = "failed"
	OutcomeStopped   Outcome = "stopped" // the context ended during the backup
)

// Sentinel errors of Run, tested with errors.Is.
var (
	// ErrLocked: another backup holds the lock of the backup folder.
	ErrLocked = errors.New("another backup is running")
	// ErrNoRoom: the file system of the backup folder has not the room for the backup.
	ErrNoRoom = errors.New("not enough free space for the backup")
	// ErrNameTaken: a folder of the backup folder already has the name of the backup.
	ErrNameTaken = errors.New("a backup with this name already exists")
)

// Options are the inputs of one backup. The paths are given one by one, never DATA_DIR: the
// package cannot reach DATA_DIR/tmp.
type Options struct {
	Database string // DATA_DIR/hippo.db
	Assets   string // DATA_DIR/assets, the images folder
	Dir      string // BACKUP_DIR, the backup folder
	Location *time.Location
	Now      func() time.Time
	Version  string // the backend version, for the manifest and the log line
	Trigger  Trigger
	// LockWait is how long to wait for the lock: zero tries once and gives up with outcome
	// skipped; otherwise the lock is tried every second until it is taken or LockWait has
	// passed (outcome locked).
	LockWait time.Duration
	Logger   *slog.Logger

	hooks hooks // tests only (export_test.go)
}

// hooks are the seams of the tests; their zero value is production.
type hooks struct {
	freeSpace     func(path string) (free uint64, known bool, err error)
	link          func(root *os.Root, oldname, newname string) error
	afterDatabase func()
	failWrites    bool
}

// Result describes a backup, successful or not: the fields of its log line.
type Result struct {
	Outcome           Outcome
	Name              string // empty when the backup failed before taking the lock
	MigrationVersion  int64
	DatabaseBytes     int64
	Images            int // images in the backup: copied plus linked
	ImagesCopied      int
	ImagesLinked      int
	ImagesGone        int
	ImageBytesWritten int64
	BackupsRemoved    int
	LeftoversRemoved  int
	Duration          time.Duration
}
