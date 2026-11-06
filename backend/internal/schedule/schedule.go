// Package schedule runs a task once a day at a wall-clock time of a location: the nightly
// analysis pass at BATCH_SCHEDULE in Paris time (RG-30, design.md D1 of submit-nightly-batch).
//
// Only the daily form « M H * * * » is accepted. Each calendar date of the location has exactly
// one run instant: a time that does not exist that day (spring clock change) runs at the end of
// the gap, a time that occurs twice (autumn clock change) runs at its first occurrence. A date
// missed while the service was stopped is not caught up.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Daily is a time of day, read as a wall-clock time of a location.
type Daily struct {
	Hour, Minute int
}

// ParseDaily reads the daily form « M H * * * »: exactly five fields separated by one space,
// M from 0 to 59 and H from 0 to 23 as plain decimal numbers of one or two digits, the other
// three fields « * ». Lists, ranges, steps, names, a day, month or weekday field, and any other
// number of fields are refused.
func ParseDaily(expr string) (Daily, error) {
	fields := strings.Split(expr, " ")
	if len(fields) != 5 {
		return Daily{}, fmt.Errorf("schedule %q: want the daily form \"M H * * *\"", expr)
	}
	if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
		return Daily{}, fmt.Errorf("schedule %q: only a daily schedule is accepted, \"M H * * *\"", expr)
	}
	minute, errM := number(fields[0], 59)
	hour, errH := number(fields[1], 23)
	if err := errors.Join(errM, errH); err != nil {
		return Daily{}, fmt.Errorf("schedule %q: want the minute 0-59 and the hour 0-23: %w", expr, err)
	}
	return Daily{Hour: hour, Minute: minute}, nil
}

// number reads one or two ASCII digits, at most limit.
func number(field string, limit int) (int, error) {
	if len(field) < 1 || len(field) > 2 {
		return 0, fmt.Errorf("%q is not a number from 0 to %d", field, limit)
	}
	for _, c := range []byte(field) {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not a number from 0 to %d", field, limit)
		}
	}
	n, err := strconv.Atoi(field)
	if err != nil || n > limit {
		return 0, fmt.Errorf("%q is not a number from 0 to %d", field, limit)
	}
	return n, nil
}

// String returns the schedule in its daily form, without leading zeros.
func (d Daily) String() string {
	return fmt.Sprintf("%d %d * * *", d.Minute, d.Hour)
}

// probeStep and probeSpan bound the instants RunAt examines around the candidate of time.Date:
// every offset change of the tz database is a multiple of 15 minutes and at most 2 hours
// (Paris: one hour).
const (
	probeStep = 15 * time.Minute
	probeSpan = 3 * time.Hour
)

// RunAt returns the run instant of the calendar date year-month-day in loc:
//   - the earliest instant whose wall clock in loc is that date at H:M (in an overlap, the
//     first occurrence);
//   - when no instant has that wall clock (a gap), the first instant of that date whose wall
//     clock is after H:M: the end of the gap.
//
// It does not rely on time.Date alone, which in a transition « does not guarantee which » of
// the instants it returns (package time, func Date).
func (d Daily) RunAt(year int, month time.Month, day int, loc *time.Location) time.Time {
	candidate := time.Date(year, month, day, d.Hour, d.Minute, 0, 0, loc)
	target := d.Hour*3600 + d.Minute*60
	sameDate := func(t time.Time) bool {
		y, m, dd := t.In(loc).Date()
		return y == year && m == month && dd == day
	}
	clock := func(t time.Time) int {
		h, m, s := t.In(loc).Clock()
		return h*3600 + m*60 + s
	}

	var exact, after time.Time
	for offset := -probeSpan; offset <= probeSpan; offset += probeStep {
		t := candidate.Add(offset)
		if sameDate(t) && clock(t) == target && (exact.IsZero() || t.Before(exact)) {
			exact = t
		}
		// The end of a gap is the start of the zone in effect after it.
		start, _ := t.ZoneBounds()
		if !start.IsZero() && sameDate(start) && clock(start) > target && (after.IsZero() || start.Before(after)) {
			after = start
		}
	}
	switch {
	case !exact.IsZero():
		return exact
	case !after.IsZero():
		return after
	default:
		return candidate
	}
}

// maxDates bounds the dates Next examines: the run of the date after the one of `after` is
// always later, except across an overlap at midnight, which the date after that settles.
const maxDates = 3

// Next returns the first run instant strictly after `after`: the run of the date of `after` in
// loc when it is still to come, otherwise the run of a following date. A run already past is
// never caught up.
func (d Daily) Next(after time.Time, loc *time.Location) time.Time {
	year, month, day := after.In(loc).Date()
	var run time.Time
	for i := range maxDates + 1 {
		// time.Date normalises the day; noon is never inside a transition of the date.
		y, m, dd := time.Date(year, month, day+i, 12, 0, 0, 0, loc).Date()
		run = d.RunAt(y, m, dd, loc)
		if run.After(after) {
			return run
		}
	}
	return run
}

// Clock is the time source of Run: the real clock in production, a fake one in tests.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// LateTolerance is how late after its instant a run still starts. A wake-up later than that (the
// NAS resumed from sleep, whose timers do not count the suspended time, or a wall clock moved
// ahead) is a missed run: it is not caught up at another hour (RG-30).
const LateTolerance = 10 * time.Minute

// Run calls task once per date of loc at the time of d, until ctx is done. It waits for the
// next run instant; a wake-up before it (the wall clock moved back) waits again, a wake-up more
// than LateTolerance after it skips the run, logged at warn with outcome missed_run and the
// planned instant, and waits for the next one after the wake-up. task runs synchronously, with
// ctx: Run returns once it has returned. The run after a task is the first one after both the
// instant just run and the end of the task, so that a clock moved back never runs a date twice.
// logger nil discards the lines.
func Run(ctx context.Context, d Daily, loc *time.Location, clock Clock, logger *slog.Logger, task func(context.Context)) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	next := d.Next(clock.Now(), loc)
	for {
		if wait := next.Sub(clock.Now()); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-clock.After(wait):
			}
			if clock.Now().Before(next) {
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		now := clock.Now()
		if now.Sub(next) > LateTolerance {
			logger.Warn("scheduled run missed", slog.String("outcome", "missed_run"),
				slog.Time("planned", next.UTC()), slog.Time("woke", now.UTC()))
			next = d.Next(now, loc)
			continue
		}
		task(ctx)
		from := next
		if now := clock.Now(); now.After(from) {
			from = now
		}
		next = d.Next(from, loc)
	}
}
