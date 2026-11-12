// Package freespace measures the space left on a file system, for the writers that must not
// fill the NAS: the extraction before a PDF, the backup before a copy. It is a leaf: it imports
// the standard library only.
package freespace

// Margin is the space left free on the NAS after a write: the database, its WAL and the images
// share the volume, and SQLite fails on a full disk as well.
const Margin = 64 << 20
