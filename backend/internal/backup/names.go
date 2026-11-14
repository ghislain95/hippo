package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

// nameLayout is the backup format: the Paris time at which the backup started, with its
// offset, without colons (not valid in a name over SMB from a Mac). The offset keeps the names
// unique across the autumn overlap and makes each an exact instant (design D7).
const nameLayout = "2006-01-02T150405-0700"

// Retention (design D9): the backups older than retention are removed after a successful
// backup, except the keep most recent and the one just made.
const (
	retention = 14 * 24 * time.Hour
	keep      = 3
)

// parseName reads a name in the backup format; anything else, even close, is not a backup.
func parseName(name string) (time.Time, bool) {
	t, err := time.Parse(nameLayout, name)
	if err != nil || t.Format(nameLayout) != name {
		return time.Time{}, false
	}
	return t, true
}

// dated is a backup of the backup folder: a folder named in the backup format.
type dated struct {
	name string
	at   time.Time
}

// listBackups returns the backups of the backup folder, most recent first. A file or a link
// named in the backup format is not one.
func listBackups(root *os.Root) ([]dated, error) {
	entries, err := readDir(root, ".")
	if err != nil {
		return nil, err
	}
	var backups []dated
	for _, entry := range entries {
		at, ok := parseName(entry.Name())
		if !ok {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list backups: %w", pathless(err))
		}
		if info.IsDir() {
			backups = append(backups, dated{name: entry.Name(), at: at})
		}
	}
	slices.SortFunc(backups, func(a, b dated) int { return b.at.Compare(a.at) })
	return backups, nil
}

// latestBackup is the name of the most recent backup, the one images are linked from, or "".
func latestBackup(root *os.Root) (string, error) {
	backups, err := listBackups(root)
	if err != nil || len(backups) == 0 {
		return "", err
	}
	return backups[0].name, nil
}

// removeLeftovers removes the partial folders a crash left (.partial-<name in the backup
// format>), and nothing else, under the lock: no other backup is writing one.
func removeLeftovers(root *os.Root) (int, error) {
	entries, err := readDir(root, ".")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		rest, ok := strings.CutPrefix(entry.Name(), partialPrefix)
		if !ok {
			continue
		}
		if _, ok := parseName(rest); !ok {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("list partial folders: %w", pathless(err))
		}
		if !info.IsDir() {
			continue
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return removed, fmt.Errorf("remove partial folder: %w", pathless(err))
		}
		removed++
	}
	return removed, nil
}

// retain removes, after the backup name made at start, the backups older than retention,
// except the keep most recent and name itself, and returns how many it removed. A removal that
// fails is logged on its own line and does not fail the backup, which exists.
func (r *runner) retain(root *os.Root, name string, start time.Time) int {
	backups, err := listBackups(root)
	if err != nil {
		r.opts.Logger.Error("backup retention", slog.String("error", err.Error()))
		return 0
	}
	limit := start.Add(-retention)
	removed := 0
	for i, b := range backups {
		if i < keep || b.name == name || !b.at.Before(limit) {
			continue
		}
		if err := root.RemoveAll(b.name); err != nil {
			r.opts.Logger.Error("backup retention", slog.String("error", pathless(err).Error()))
			continue
		}
		removed++
	}
	return removed
}
