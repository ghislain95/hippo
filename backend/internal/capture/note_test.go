package capture_test

import (
	"testing"
	"time"
	_ "time/tzdata" // the Paris zone without the machine's time zone database

	"github.com/ghislain95/hippo/backend/internal/capture"
)

func TestMergeNote(t *testing.T) {
	const day = "2026-10-27"
	cases := []struct {
		name        string
		existing    *string
		note        string
		want        *string
		wantChanged bool
	}{
		{
			name: "no existing note: the note is taken undated", existing: nil, note: "to test",
			want: ptr("to test"), wantChanged: true,
		},
		{
			name: "first note kept undated, further note dated", existing: ptr("for the trip"), note: "to test",
			want: ptr("for the trip\n\n2026-10-27 — to test"), wantChanged: true,
		},
		{
			name: "dated append after a dated entry", existing: ptr("for the trip\n\n2026-10-20 — to test"), note: "again",
			want: ptr("for the trip\n\n2026-10-20 — to test\n\n2026-10-27 — again"), wantChanged: true,
		},
		{
			name: "identical to the first note", existing: ptr("for the trip\n\n2026-10-20 — to test"), note: "for the trip",
			want: ptr("for the trip\n\n2026-10-20 — to test"), wantChanged: false,
		},
		{
			name: "identical to a dated entry of another day", existing: ptr("for the trip\n\n2026-10-20 — to test\n\n2026-10-21 — other"), note: "to test",
			want: ptr("for the trip\n\n2026-10-20 — to test\n\n2026-10-21 — other"), wantChanged: false,
		},
		{
			name: "identical to the last dated entry", existing: ptr("for the trip\n\n2026-10-20 — to test\n\n2026-10-21 — other"), note: "other",
			want: ptr("for the trip\n\n2026-10-20 — to test\n\n2026-10-21 — other"), wantChanged: false,
		},
		{
			name: "note contained in another one is appended", existing: ptr("buy milk and bread"), note: "buy milk",
			want: ptr("buy milk and bread\n\n2026-10-27 — buy milk"), wantChanged: true,
		},
		{
			name: "multi-paragraph first note is one entry", existing: ptr("line one\n\nline two"), note: "line two",
			want: ptr("line one\n\nline two\n\n2026-10-27 — line two"), wantChanged: true,
		},
		{
			name: "multi-paragraph note equal to the first note", existing: ptr("line one\n\nline two"), note: "line one\n\nline two",
			want: ptr("line one\n\nline two"), wantChanged: false,
		},
		{
			name: "a date without a blank line is not a separator", existing: ptr("for the trip\n2026-10-20 — to test"), note: "to test",
			want: ptr("for the trip\n2026-10-20 — to test\n\n2026-10-27 — to test"), wantChanged: true,
		},
		{
			name: "comparison is byte for byte", existing: ptr("To test"), note: "to test",
			want: ptr("To test\n\n2026-10-27 — to test"), wantChanged: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := capture.MergeNote(tc.existing, tc.note, day)
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
			if got == nil || *got != *tc.want {
				t.Errorf("MergeNote = %v, want %q", deref(got), *tc.want)
			}
		})
	}
}

func TestDayInParis(t *testing.T) {
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatalf("LoadParis: %v", err)
	}
	cases := []struct {
		at, want string
	}{
		{"2026-10-27T22:30:00Z", "2026-10-27"}, // 23:30 in Paris
		{"2026-10-27T23:30:00Z", "2026-10-28"}, // 00:30 the next day in Paris
		{"2026-10-24T22:30:00Z", "2026-10-25"}, // summer time, the night before the change
		{"2026-10-25T23:30:00Z", "2026-10-26"}, // winter time, the night after the change
		{"2026-07-01T21:59:59Z", "2026-07-01"},
		{"2026-07-01T22:00:00Z", "2026-07-02"},
	}
	for _, tc := range cases {
		t.Run(tc.at, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := capture.DayIn(at, paris); got != tc.want {
				t.Errorf("DayIn(%s) = %s, want %s", tc.at, got, tc.want)
			}
		})
	}
}

func deref(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
