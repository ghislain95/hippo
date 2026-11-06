package childproc

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestWatchMemory(t *testing.T) {
	t.Run("the watch samples at the interval it is given", func(t *testing.T) {
		const interval = 25 * time.Millisecond
		begin := time.Now()
		w := watchMemory(math.MaxUint64, interval, func() { t.Error("exit called below the limit") })
		// Waiting is the point here: the number of samples over a span of time is what is
		// measured. A loaded machine samples less, never more.
		<-time.After(6 * interval)
		w.Stop()
		elapsed := time.Since(begin)
		if most := int(elapsed/interval) + 2; w.reads < 1 || w.reads > most {
			t.Errorf("%d samples in %v, want 1 to %d at one every %v", w.reads, elapsed, most, interval)
		}
	})

	t.Run("the watch calls its exit above the limit", func(t *testing.T) {
		exited := make(chan struct{})
		var once sync.Once
		w := watchMemory(0, 10*time.Millisecond, func() { once.Do(func() { close(exited) }) })
		<-exited // the first sample is taken at once, and any runtime holds more than 0 bytes
		w.Stop()
		if w.max == 0 {
			t.Error("no peak recorded")
		}
	})
}
