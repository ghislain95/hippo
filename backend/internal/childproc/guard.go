package childproc

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"
)

// WatchInterval is how often Guard samples the memory of its child: every millisecond, so that
// a page whose tree grows fast is stopped close to the limit.
const WatchInterval = time.Millisecond

// Guard bounds the memory of the child that calls it, at its start: soft is the limit of the
// garbage collector (debug.SetMemoryLimit), which works harder before the hard limit so that
// garbage is not taken for a heavy input; above hard, the child exits at once with ExitMemory.
// It sets GOMAXPROCS to 2 at least, so that the watch runs while the work loops. It samples
// every WatchInterval.
func Guard(hard, soft uint64) *Watch {
	return GuardEvery(hard, soft, WatchInterval)
}

// GuardEvery is Guard sampling every interval (WatchInterval when not positive). Each sample
// reads the runtime's metrics, which stops the world for a moment: a child whose work is long
// and whose memory grows by large, known steps (a decoded image) samples less often, at the
// cost of stopping a little further past its limit.
func GuardEvery(hard, soft uint64, interval time.Duration) *Watch {
	debug.SetMemoryLimit(int64(min(soft, math.MaxInt64)))
	if runtime.GOMAXPROCS(0) < 2 {
		runtime.GOMAXPROCS(2)
	}
	if interval <= 0 {
		interval = WatchInterval
	}
	return watchMemory(hard, interval, func() { os.Exit(ExitMemory) })
}

// Watch samples the memory the runtime holds and calls its exit above a limit.
type Watch struct {
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	max     uint64 // written by the watch only; read after Stop
	reads   int    // samples taken, for the tests; written by the watch only, read after Stop
}

// watchMemory starts a goroutine that reads, at once then every interval, the memory the
// runtime holds (all its classes, minus the heap returned to the system), and calls exit above
// limit.
func watchMemory(limit uint64, interval time.Duration, exit func()) *Watch {
	w := &Watch{done: make(chan struct{}), stopped: make(chan struct{})}
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	go func() {
		defer close(w.stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			metrics.Read(samples)
			w.reads++
			held := samples[0].Value.Uint64() - samples[1].Value.Uint64()
			w.max = max(w.max, held)
			if held > limit {
				exit()
			}
			select {
			case <-w.done:
				return
			case <-ticker.C:
			}
		}
	}()
	return w
}

// Stop ends the watch and waits for it; it may run more than once.
func (w *Watch) Stop() {
	w.once.Do(func() { close(w.done) })
	<-w.stopped
}

// Peak stops the watch and returns the highest memory it saw.
func (w *Watch) Peak() uint64 {
	w.Stop()
	return w.max
}
