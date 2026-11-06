package analysis_test

import (
	"context"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // Asia/Tokyo without a time zone database on the machine

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/schedule"
	"github.com/ghislain95/hippo/backend/internal/schedule/scheduletest"
)

// scheduledEnv is a pass over the fake AI service run by schedule.Run, the store, the pass,
// the fake AI service and the scheduler sharing one fake clock.
type scheduledEnv struct {
	*passEnv
	sched *scheduletest.Clock

	mu      sync.Mutex
	starts  []time.Time // of each pass
	creates []time.Time // of each batch received
}

func newScheduledEnv(t *testing.T, start time.Time) *scheduledEnv {
	t.Helper()
	// The zone of the machine (time.Local, which the runtime reads from TZ) is far from Paris:
	// the schedule and the pass must not depend on it. time.Local is set once at start-up, so
	// t.Setenv("TZ", …) would change nothing; the test is not parallel, and nothing else runs.
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	local := time.Local
	time.Local = tokyo
	t.Cleanup(func() { time.Local = local })
	sched := scheduletest.NewClock(start)
	e := &scheduledEnv{
		passEnv: &passEnv{env: newEnvWithClock(t, sched.Now), clock: &fakeClock{now: start}, ai: analysistest.New(sched.Now)},
		sched:   sched,
	}
	e.ai.OnCreate(func(context.Context) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.creates = append(e.creates, sched.Now())
	})
	return e
}

// run starts the scheduler of expr over a pass of the env, stopped at the end of the test.
func (e *scheduledEnv) run(t *testing.T, expr string) {
	t.Helper()
	daily, err := schedule.ParseDaily(expr)
	if err != nil {
		t.Fatal(err)
	}
	cfg := e.passConfig(t)
	cfg.Now = e.sched.Now
	pass, err := analysis.NewPass(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		schedule.Run(ctx, daily, cfg.Location, e.sched, cfg.Logger, func(ctx context.Context) {
			e.mu.Lock()
			e.starts = append(e.starts, e.sched.Now())
			e.mu.Unlock()
			if err := pass.Run(ctx); err != nil {
				t.Errorf("pass: %v", err)
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	e.sched.Wait(t)
}

// walk moves the clock from its time to until, a quarter of an hour at a time, letting the
// scheduler run each pass due before moving on.
func (e *scheduledEnv) walk(t *testing.T, until time.Time) {
	t.Helper()
	for now := e.sched.Now().Add(15 * time.Minute); !now.After(until); now = now.Add(15 * time.Minute) {
		if e.sched.Set(now) {
			e.sched.Wait(t) // the pass has run and the scheduler waits for the next date
		}
	}
}

func (e *scheduledEnv) passStarts() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.starts...)
}

func (e *scheduledEnv) batchesReceived() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.creates...)
}

func wantInstants(t *testing.T, what string, got []time.Time, want ...time.Time) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s at %v, want %v", what, got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("%s %d at %s, want %s", what, i, got[i].UTC(), want[i])
		}
	}
}

func TestPassSchedule(t *testing.T) {
	t.Run("The pass runs at the scheduled Paris time", func(t *testing.T) {
		e := newScheduledEnv(t, time.Date(2027, 1, 9, 23, 0, 0, 0, time.UTC)) // midnight in Paris
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		e.run(t, "0 2 * * *")

		e.walk(t, time.Date(2027, 1, 10, 0, 45, 0, 0, time.UTC))
		if got := e.batchesReceived(); len(got) != 0 {
			t.Fatalf("batches received at %v, before 02:00 in Paris", got)
		}
		e.walk(t, time.Date(2027, 1, 10, 22, 45, 0, 0, time.UTC))
		at := time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC) // 02:00 in Paris
		wantInstants(t, "batch", e.batchesReceived(), at)
		if ids := e.sentIDs(); len(ids) != 1 || ids[0] != id {
			t.Errorf("requests sent for %v, want %s", ids, id)
		}
	})

	t.Run("A schedule inside the spring clock change runs once, after the gap", func(t *testing.T) {
		e := newScheduledEnv(t, time.Date(2027, 3, 27, 23, 0, 0, 0, time.UTC)) // midnight in Paris
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.run(t, "30 2 * * *")

		e.walk(t, time.Date(2027, 3, 28, 22, 0, 0, 0, time.UTC)) // midnight in Paris, CEST
		at := time.Date(2027, 3, 28, 1, 0, 0, 0, time.UTC)       // 03:00 in Paris, CEST
		wantInstants(t, "pass", e.passStarts(), at)
		wantInstants(t, "batch", e.batchesReceived(), at)
	})

	t.Run("A schedule inside the autumn clock change runs once", func(t *testing.T) {
		e := newScheduledEnv(t, time.Date(2027, 10, 30, 22, 0, 0, 0, time.UTC)) // midnight in Paris, CEST
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.run(t, "30 2 * * *")

		// Through both 02:30 of the date: 00:30 UTC (CEST), then 01:30 UTC (CET).
		e.walk(t, time.Date(2027, 10, 31, 23, 0, 0, 0, time.UTC)) // midnight in Paris, CET
		at := time.Date(2027, 10, 31, 0, 30, 0, 0, time.UTC)
		wantInstants(t, "pass", e.passStarts(), at)
		wantInstants(t, "batch", e.batchesReceived(), at)
	})

	t.Run("A restart after the scheduled time does not catch up", func(t *testing.T) {
		e := newScheduledEnv(t, time.Date(2027, 1, 10, 8, 0, 0, 0, time.UTC)) // 09:00 in Paris
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.run(t, "0 2 * * *")

		e.walk(t, time.Date(2027, 1, 11, 0, 45, 0, 0, time.UTC)) // 01:45 in Paris, the next day
		if got := e.batchesReceived(); len(got) != 0 {
			t.Fatalf("batches received at %v, before 02:00 in Paris the next day", got)
		}
		e.walk(t, time.Date(2027, 1, 11, 2, 0, 0, 0, time.UTC))
		wantInstants(t, "batch", e.batchesReceived(), time.Date(2027, 1, 11, 1, 0, 0, 0, time.UTC))
	})
}
