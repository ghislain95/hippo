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

// watchInterval is how often a child samples its memory.
const watchInterval = time.Millisecond

// Guard bounds the memory of the child that calls it, at its start: soft is the limit of the
// garbage collector (debug.SetMemoryLimit), which works harder before the hard limit so that
// garbage is not taken for a heavy input; above hard, the child exits at once with ExitMemory.
// It sets GOMAXPROCS to 2 at least, so that the watch runs while the work loops.
func Guard(hard, soft uint64) *Watch {
	debug.SetMemoryLimit(int64(min(soft, math.MaxInt64)))
	if runtime.GOMAXPROCS(0) < 2 {
		runtime.GOMAXPROCS(2)
	}
	return watchMemory(hard, func() { os.Exit(ExitMemory) })
}

// Watch samples the memory the runtime holds and calls its exit above a limit.
type Watch struct {
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	max     uint64 // written by the watch only; read after Stop
}

// watchMemory starts a goroutine that reads, every watchInterval, the memory the runtime holds
// (all its classes, minus the heap returned to the system), and calls exit above limit.
func watchMemory(limit uint64, exit func()) *Watch {
	w := &Watch{done: make(chan struct{}), stopped: make(chan struct{})}
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	go func() {
		defer close(w.stopped)
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			metrics.Read(samples)
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
