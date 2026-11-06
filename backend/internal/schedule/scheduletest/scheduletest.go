// Package scheduletest gives the tests a clock that schedule.Run waits on, moved by the test
// only: no sleep, no dependency on the real time nor on the zone of the machine
// (docs/dev/testing.md « Horloge »).
package scheduletest

import (
	"sync"
	"testing"
	"time"
)

// waitTimeout bounds the wait of a test for the scheduler: a scheduler that never waits again
// fails the test instead of hanging it.
const waitTimeout = 10 * time.Second

// Clock is a fake schedule.Clock. Its zero value is not usable: call NewClock. It is safe for
// concurrent use.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []timer
	waits  chan time.Time
}

type timer struct {
	deadline time.Time
	ch       chan time.Time
}

// NewClock returns a clock at now.
func NewClock(now time.Time) *Clock {
	return &Clock{now: now, waits: make(chan time.Time, 64)}
}

// Now returns the time of the clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that receives the time once Set moves the clock to now + d or later,
// or once Fire is called; the deadline is handed to Wait.
// A test that lets more than 64 calls go unread fails rather than blocks.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	t := timer{deadline: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	select {
	case c.waits <- t.deadline:
	default:
		panic("scheduletest: 64 waits of the scheduler left unread by the test")
	}
	return t.ch
}

// Wait returns the deadline of the next call to After, failing the test when none comes.
func (c *Clock) Wait(t testing.TB) time.Time {
	t.Helper()
	select {
	case deadline := <-c.waits:
		return deadline
	case <-time.After(waitTimeout):
		t.Fatal("the scheduler never waited on the clock")
		return time.Time{}
	}
}

// Set moves the clock to now, forward or back, and fires the timers whose deadline is reached.
// It tells whether a timer fired.
func (c *Clock) Set(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
	fired := false
	kept := c.timers[:0]
	for _, t := range c.timers {
		if now.Before(t.deadline) {
			kept = append(kept, t)
			continue
		}
		t.ch <- now
		fired = true
	}
	c.timers = kept
	return fired
}

// Fire fires every pending timer without moving the clock, as a timer of the system does when
// the wall clock was moved back while it ran.
func (c *Clock) Fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.timers {
		t.ch <- c.now
	}
	c.timers = nil
}
