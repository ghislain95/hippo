package schedule_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/schedule"
	"github.com/ghislain95/hippo/backend/internal/schedule/scheduletest"
)

func paris(t *testing.T) *time.Location {
	t.Helper()
	loc, err := capture.LoadParis()
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func utc(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func TestParseDaily(t *testing.T) {
	accepted := []struct {
		expr         string
		hour, minute int
	}{
		{"0 2 * * *", 2, 0},
		{"59 23 * * *", 23, 59},
		{"0 0 * * *", 0, 0},
		{"05 07 * * *", 7, 5},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.expr, func(t *testing.T) {
			d, err := schedule.ParseDaily(tc.expr)
			if err != nil {
				t.Fatalf("ParseDaily(%q): %v", tc.expr, err)
			}
			if d.Hour != tc.hour || d.Minute != tc.minute {
				t.Errorf("ParseDaily(%q) = %+v, want %02d:%02d", tc.expr, d, tc.hour, tc.minute)
			}
		})
	}

	refused := []string{
		"",
		"nightly",
		"@daily",
		"0,30 2 * * *",   // list
		"0 1-3 * * *",    // range
		"*/5 * * * *",    // step
		"0 */2 * * *",    // step
		"* 2 * * *",      // every minute
		"0 * * * *",      // every hour
		"0 2 1 * *",      // day of month
		"0 2 * 1 *",      // month
		"0 2 * JAN *",    // name
		"0 2 * * 1",      // weekday
		"0 2 * * MON",    // name
		"0 2 * * * *",    // six fields
		"0 2 * *",        // four fields
		"60 2 * * *",     // minute out of range
		"0 24 * * *",     // hour out of range
		"0 25 * * *",     // hour out of range
		"-1 2 * * *",     // sign
		"+0 2 * * *",     // sign
		"000 2 * * *",    // three digits
		"0  2 * * *",     // two spaces between fields
		"0 2 * *  *",     // two spaces between fields
		" 0 2 * * *",     // leading space
		"0 2 * * * ",     // trailing space
		"0\t2 * * *",     // tab
		"0 2 ** * *",     // field with two stars
		"0 2 * * ?",      // question mark
		"0x1 2 * * *",    // hexadecimal
		"0 ２ * * *",      // full-width digit
		"0 2 * * *\n",    // newline
		"0 2 * * * 2027", // year field
	}
	for _, expr := range refused {
		t.Run("refuses "+strings.ReplaceAll(expr, "\t", `\t`), func(t *testing.T) {
			if d, err := schedule.ParseDaily(expr); err == nil {
				t.Errorf("ParseDaily(%q) = %+v, want an error", expr, d)
			}
		})
	}
}

func TestDailyString(t *testing.T) {
	d, err := schedule.ParseDaily("05 07 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != "5 7 * * *" {
		t.Errorf("String() = %q, want %q", got, "5 7 * * *")
	}
}

func TestRunAt(t *testing.T) {
	loc := paris(t)
	cases := []struct {
		name   string
		expr   string
		date   time.Time // its year, month and day
		want   time.Time
		inGap  bool
		wallHM string
	}{
		{"an ordinary winter date", "0 2 * * *", utc(2027, 1, 10, 0, 0), utc(2027, 1, 10, 1, 0), false, "02:00"},
		{"an ordinary summer date", "0 2 * * *", utc(2027, 7, 14, 0, 0), utc(2027, 7, 14, 0, 0), false, "02:00"},
		{"midnight belongs to its own date", "0 0 * * *", utc(2027, 7, 14, 0, 0), utc(2027, 7, 13, 22, 0), false, "00:00"},
		{"the last minute of a date", "59 23 * * *", utc(2027, 1, 10, 0, 0), utc(2027, 1, 10, 22, 59), false, "23:59"},
		{"28 March 2027, a time inside the gap gives the end of the gap", "30 2 * * *", utc(2027, 3, 28, 0, 0), utc(2027, 3, 28, 1, 0), true, "03:00"},
		{"28 March 2027, the first minute of the gap", "0 2 * * *", utc(2027, 3, 28, 0, 0), utc(2027, 3, 28, 1, 0), true, "03:00"},
		{"28 March 2027, the last minute of the gap", "59 2 * * *", utc(2027, 3, 28, 0, 0), utc(2027, 3, 28, 1, 0), true, "03:00"},
		{"28 March 2027, before the gap", "59 1 * * *", utc(2027, 3, 28, 0, 0), utc(2027, 3, 28, 0, 59), false, "01:59"},
		{"28 March 2027, right after the gap", "0 3 * * *", utc(2027, 3, 28, 0, 0), utc(2027, 3, 28, 1, 0), false, "03:00"},
		{"31 October 2027, a time inside the overlap gives its first occurrence", "30 2 * * *", utc(2027, 10, 31, 0, 0), utc(2027, 10, 31, 0, 30), false, "02:30"},
		{"31 October 2027, the first minute of the overlap", "0 2 * * *", utc(2027, 10, 31, 0, 0), utc(2027, 10, 31, 0, 0), false, "02:00"},
		{"31 October 2027, the last minute of the overlap", "59 2 * * *", utc(2027, 10, 31, 0, 0), utc(2027, 10, 31, 0, 59), false, "02:59"},
		{"31 October 2027, after the overlap", "0 3 * * *", utc(2027, 10, 31, 0, 0), utc(2027, 10, 31, 2, 0), false, "03:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := schedule.ParseDaily(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			got := d.RunAt(tc.date.Year(), tc.date.Month(), tc.date.Day(), loc)
			if !got.Equal(tc.want) {
				t.Errorf("RunAt = %s (%s Paris), want %s", got.UTC(), got.In(loc), tc.want)
			}
			if wall := got.In(loc).Format("15:04"); wall != tc.wallHM {
				t.Errorf("wall clock in Paris = %s, want %s", wall, tc.wallHM)
			}
			if y, m, day := got.In(loc).Date(); y != tc.date.Year() || m != tc.date.Month() || day != tc.date.Day() {
				t.Errorf("Paris date = %d-%02d-%02d, want the date asked", y, m, day)
			}
		})
	}
}

func TestNext(t *testing.T) {
	loc := paris(t)
	cases := []struct {
		name  string
		expr  string
		after time.Time
		want  time.Time
	}{
		{"before the time of the day gives today", "0 2 * * *", utc(2027, 1, 10, 0, 59), utc(2027, 1, 10, 1, 0)},
		{"at the instant itself gives tomorrow", "0 2 * * *", utc(2027, 1, 10, 1, 0), utc(2027, 1, 11, 1, 0)},
		{"after the time of the day gives tomorrow, no catch-up", "0 2 * * *", utc(2027, 1, 10, 8, 0), utc(2027, 1, 11, 1, 0)},
		{"the Paris date counts, not the UTC date", "30 0 * * *", utc(2027, 1, 9, 23, 15), utc(2027, 1, 9, 23, 30)},
		{"the eve of the spring change gives the end of the gap", "30 2 * * *", utc(2027, 3, 27, 12, 0), utc(2027, 3, 28, 1, 0)},
		{"the end of the gap gives the next date", "30 2 * * *", utc(2027, 3, 28, 1, 0), utc(2027, 3, 29, 0, 30)},
		{"the eve of the autumn change gives the first occurrence", "30 2 * * *", utc(2027, 10, 30, 12, 0), utc(2027, 10, 31, 0, 30)},
		{"the first occurrence gives the next date, not the second", "30 2 * * *", utc(2027, 10, 31, 0, 30), utc(2027, 11, 1, 1, 30)},
		{"between the two occurrences gives the next date", "30 2 * * *", utc(2027, 10, 31, 1, 0), utc(2027, 11, 1, 1, 30)},
		{"the last day of the year", "0 2 * * *", utc(2027, 12, 31, 8, 0), utc(2028, 1, 1, 1, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := schedule.ParseDaily(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got := d.Next(tc.after, loc); !got.Equal(tc.want) {
				t.Errorf("Next(%s) = %s, want %s", tc.after, got.UTC(), tc.want)
			}
		})
	}

	t.Run("a date of the year has one instant, whatever the time of the day", func(t *testing.T) {
		d, err := schedule.ParseDaily("30 2 * * *")
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]int{}
		next := d.Next(utc(2026, 12, 31, 12, 0), loc)
		for next.Year() < 2028 {
			seen[next.In(loc).Format(time.DateOnly)]++
			next = d.Next(next, loc)
		}
		if len(seen) != 365 {
			t.Errorf("%d Paris dates of 2027 got a run, want 365", len(seen))
		}
		for date, n := range seen {
			if n != 1 {
				t.Errorf("%s got %d runs, want 1", date, n)
			}
		}
	})
}

// runner runs schedule.Run on a fake clock and reports each pass on a channel.
type runner struct {
	clock  *scheduletest.Clock
	logs   *syncBuffer
	passes chan time.Time
	cancel context.CancelFunc
	done   chan struct{}
}

func start(t *testing.T, expr string, now time.Time) *runner {
	t.Helper()
	d, err := schedule.ParseDaily(expr)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{
		clock: scheduletest.NewClock(now), logs: &syncBuffer{},
		passes: make(chan time.Time, 16), done: make(chan struct{}),
	}
	logger := slog.New(slog.NewJSONHandler(r.logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		schedule.Run(ctx, d, paris(t), r.clock, logger, func(context.Context) { r.passes <- r.clock.Now() })
	}()
	t.Cleanup(func() {
		cancel()
		<-r.done
	})
	return r
}

// noPass fails the test when a pass ran.
func (r *runner) noPass(t *testing.T) {
	t.Helper()
	select {
	case at := <-r.passes:
		t.Errorf("a pass ran at %s, want none", at)
	default:
	}
}

func TestRun(t *testing.T) {
	t.Run("fires at the instant, then waits for the next date", func(t *testing.T) {
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 10, 1, 0)) {
			t.Fatalf("waits until %s, want 01:00 UTC", deadline)
		}
		if r.clock.Set(utc(2027, 1, 10, 0, 59)) {
			t.Fatal("the timer fired before its instant")
		}
		r.noPass(t)
		r.clock.Set(utc(2027, 1, 10, 1, 0))
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 11, 1, 0)) {
			t.Errorf("then waits until %s, want 11 January 01:00 UTC", deadline)
		}
		if at := <-r.passes; !at.Equal(utc(2027, 1, 10, 1, 0)) {
			t.Errorf("pass at %s, want 01:00 UTC", at)
		}
		r.noPass(t)
	})

	t.Run("waits again after a wall clock moved back", func(t *testing.T) {
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		r.clock.Wait(t)
		// The timer fires while the wall clock reads a time before the run: the clock was
		// moved back during the wait.
		r.clock.Set(utc(2027, 1, 9, 23, 0))
		r.clock.Fire()
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 10, 1, 0)) {
			t.Errorf("waits again until %s, want the same instant", deadline)
		}
		r.noPass(t)
	})

	t.Run("a wake-up within the tolerance runs the pass", func(t *testing.T) {
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		r.clock.Wait(t)
		late := utc(2027, 1, 10, 1, 0).Add(schedule.LateTolerance)
		r.clock.Set(late)
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 11, 1, 0)) {
			t.Errorf("then waits until %s, want 11 January 01:00 UTC", deadline)
		}
		if at := <-r.passes; !at.Equal(late) {
			t.Errorf("pass at %s, want %s", at, late)
		}
		if strings.Contains(r.logs.String(), "missed_run") {
			t.Errorf("logs = %s, want no missed run", r.logs)
		}
	})

	t.Run("a wake-up beyond the tolerance misses the run and is not caught up", func(t *testing.T) {
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		r.clock.Wait(t)
		r.clock.Set(utc(2027, 1, 10, 1, 0).Add(schedule.LateTolerance + time.Second))
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 11, 1, 0)) {
			t.Errorf("then waits until %s, want 11 January 01:00 UTC", deadline)
		}
		r.noPass(t)
		want := `"outcome":"missed_run","planned":"2027-01-10T01:00:00Z"`
		if !strings.Contains(r.logs.String(), want) {
			t.Errorf("logs = %s, want %s", r.logs, want)
		}
	})

	t.Run("a clock that jumps days ahead runs nothing until the next scheduled instant", func(t *testing.T) {
		// The NAS slept from the evening of 9 January to the afternoon of 13 January: the
		// timer fires on wake-up, at an hour that is not the schedule's.
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		r.clock.Wait(t)
		r.clock.Set(utc(2027, 1, 13, 15, 0))
		if deadline := r.clock.Wait(t); !deadline.Equal(utc(2027, 1, 14, 1, 0)) {
			t.Errorf("then waits until %s, want 14 January 01:00 UTC", deadline)
		}
		r.noPass(t)
		r.clock.Set(utc(2027, 1, 14, 1, 0))
		r.clock.Wait(t)
		if at := <-r.passes; !at.Equal(utc(2027, 1, 14, 1, 0)) {
			t.Errorf("pass at %s, want 14 January 01:00 UTC", at)
		}
	})

	t.Run("stops on context cancel without running", func(t *testing.T) {
		r := start(t, "0 2 * * *", utc(2027, 1, 10, 0, 0))
		r.clock.Wait(t)
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return after the cancel")
		}
		r.noPass(t)
	})

	t.Run("does not run when the context is cancelled as the timer fires", func(t *testing.T) {
		d, err := schedule.ParseDaily("0 2 * * *")
		if err != nil {
			t.Fatal(err)
		}
		clock := scheduletest.NewClock(utc(2027, 1, 10, 0, 0))
		ctx, cancel := context.WithCancel(context.Background())
		ran := false
		done := make(chan struct{})
		go func() {
			defer close(done)
			schedule.Run(ctx, d, paris(t), clock, nil, func(context.Context) { ran = true })
		}()
		clock.Wait(t)
		cancel()
		clock.Set(utc(2027, 1, 10, 1, 0))
		<-done
		if ran {
			t.Error("a pass ran after the cancel")
		}
	})
}

// syncBuffer is a bytes.Buffer safe for the scheduler's goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
