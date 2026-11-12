//go:build linux || darwin

package freespace_test

import (
	"testing"

	"github.com/ghislain95/hippo/backend/internal/freespace"
)

func TestAvailable(t *testing.T) {
	free, known, err := freespace.Available(t.TempDir())
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if !known || free == 0 {
		t.Fatalf("Available = %d, known %v; want a known positive size", free, known)
	}
}

func TestAvailableOfMissingPath(t *testing.T) {
	if _, _, err := freespace.Available(t.TempDir() + "/missing"); err == nil {
		t.Fatal("Available of a missing path: want an error")
	}
}
