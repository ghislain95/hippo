package capture

import (
	"fmt"
	"regexp"
	"time"
)

// ParisZone is the time zone of the dates RG-04 writes in a note.
const ParisZone = "Europe/Paris"

// noteSeparator starts each dated entry of a note: a blank line, a date, a space, an em dash
// and a space (RG-04). The text before the first separator is the first note, undated.
var noteSeparator = regexp.MustCompile(`\n\n[0-9]{4}-[0-9]{2}-[0-9]{2} — `)

// MergeNote returns the note of an item after a duplicate capture with the capture note note,
// received on day (YYYY-MM-DD in Paris time), and whether it changed (RG-04): an item without
// note takes it undated; otherwise it is appended after a blank line as "day — note", unless it
// is byte for byte equal to the first note or to the text of a dated entry, whatever its date.
func MergeNote(existing *string, note, day string) (*string, bool) {
	if existing == nil {
		return &note, true
	}
	for _, entry := range noteEntries(*existing) {
		if entry == note {
			return existing, false
		}
	}
	merged := *existing + "\n\n" + day + " — " + note
	return &merged, true
}

// noteEntries splits a note into its first note and the texts of its dated entries.
func noteEntries(note string) []string {
	separators := noteSeparator.FindAllStringIndex(note, -1)
	entries := make([]string, 0, len(separators)+1)
	start := 0
	for _, separator := range separators {
		entries = append(entries, note[start:separator[0]])
		start = separator[1]
	}
	return append(entries, note[start:])
}

// LoadParis loads the Paris time zone. The binary embeds the time zone database (time/tzdata
// in cmd/hippo), so this does not depend on the files of the host or the image; the TZ variable
// is not used, since Go falls back to UTC in silence on an unknown TZ.
func LoadParis() (*time.Location, error) {
	loc, err := time.LoadLocation(ParisZone)
	if err != nil {
		return nil, fmt.Errorf("load time zone %s: %w", ParisZone, err)
	}
	return loc, nil
}

// dayIn is the calendar day of now in loc, as YYYY-MM-DD.
func dayIn(now time.Time, loc *time.Location) string {
	return now.In(loc).Format(time.DateOnly)
}
