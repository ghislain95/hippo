package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// manifestFormat is the version of the manifest's layout.
const manifestFormat = 1

// manifest is manifest.json (design D8): what the restore needs (the backend version pins the
// image tag, the migration version tells whether a migration lies ahead) and the counts of the
// log line. Never a file name, never an item's content.
type manifest struct {
	Format            int     `json:"format"`
	Name              string  `json:"name"`
	StartedAt         string  `json:"started_at"`
	Trigger           Trigger `json:"trigger"`
	BackendVersion    string  `json:"backend_version"`
	MigrationVersion  int64   `json:"migration_version"`
	DatabaseBytes     int64   `json:"database_bytes"`
	Images            int     `json:"images"`
	ImagesCopied      int     `json:"images_copied"`
	ImagesLinked      int     `json:"images_linked"`
	ImagesGone        int     `json:"images_gone"`
	ImageBytesWritten int64   `json:"image_bytes_written"`
}

func (r *runner) writeManifest(root *os.Root, name string, start time.Time) (err error) {
	content, err := json.MarshalIndent(manifest{
		Format:            manifestFormat,
		Name:              r.res.Name,
		StartedAt:         start.In(r.opts.Location).Format(time.RFC3339),
		Trigger:           r.opts.Trigger,
		BackendVersion:    r.opts.Version,
		MigrationVersion:  r.res.MigrationVersion,
		DatabaseBytes:     r.res.DatabaseBytes,
		Images:            r.res.Images,
		ImagesCopied:      r.res.ImagesCopied,
		ImagesLinked:      r.res.ImagesLinked,
		ImagesGone:        r.res.ImagesGone,
		ImageBytesWritten: r.res.ImageBytesWritten,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("create manifest: %w", pathless(err))
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close manifest: %w", pathless(closeErr))
		}
	}()
	var w io.Writer = f
	if r.opts.hooks.failWrites {
		w = failingWriter{}
	}
	if _, err := w.Write(append(content, '\n')); err != nil {
		return fmt.Errorf("write manifest: %w", pathless(err))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync manifest: %w", pathless(err))
	}
	return nil
}
