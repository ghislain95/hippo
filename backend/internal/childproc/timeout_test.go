package childproc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

func TestRunRefusesANegativeTimeout(t *testing.T) {
	_, err := childproc.Run(context.Background(), childproc.Spec{Argv: []string{"/bin/true"}, Timeout: -time.Second})
	if !errors.Is(err, childproc.ErrUnavailable) {
		t.Fatalf("Run with a negative timeout = %v, want ErrUnavailable without starting the child", err)
	}
}
