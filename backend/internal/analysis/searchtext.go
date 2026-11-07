package analysis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SearchText flattens a stored fiche to the text of the search (ADR-0007, design D11 of
// apply-batch-results): every string value, in document order, one per line, except the value
// of the root member template (every fiche of a type would match its key) and of any member
// named language inside a nested object (a code block's language). Keys, numbers, booleans and
// null are never emitted; code is kept, so that a command can be found (RG-41).
func SearchText(fiche json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(fiche))
	dec.UseNumber()
	var (
		stack []jsonFrame
		lines []string
	)
	// valueDone marks the value of the innermost object member as read: a key comes next.
	valueDone := func() {
		if n := len(stack); n > 0 && stack[n-1].object {
			stack[n-1].expectKey = true
		}
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if len(stack) > 0 {
				return "", errors.New("flatten fiche: unexpected end")
			}
			break
		}
		if err != nil {
			return "", fmt.Errorf("flatten fiche: %w", err)
		}
		if n := len(stack); n > 0 && stack[n-1].object && stack[n-1].expectKey {
			if d, ok := tok.(json.Delim); ok && d == '}' {
				stack = stack[:n-1]
				valueDone()
				continue
			}
			key, ok := tok.(string)
			if !ok {
				return "", errors.New("flatten fiche: an object key is not a string")
			}
			stack[n-1].key, stack[n-1].expectKey = key, false
			continue
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, jsonFrame{object: true, expectKey: true})
			case '[':
				stack = append(stack, jsonFrame{})
			default: // ']'; '}' is read as a key position above
				stack = stack[:len(stack)-1]
				valueDone()
			}
		case string:
			if !skipString(stack) {
				lines = append(lines, v)
			}
			valueDone()
		default: // numbers, booleans, null
			valueDone()
		}
	}
	return strings.Join(lines, "\n"), nil
}

// jsonFrame is an open object or array of the fiche: for an object, the member whose value
// comes next, or expectKey when a key comes next.
type jsonFrame struct {
	object    bool
	expectKey bool
	key       string
}

// skipString tells whether a string value read at stack is left out of the search text: the
// root member template, or a member language of a nested object.
func skipString(stack []jsonFrame) bool {
	n := len(stack)
	if n == 0 || !stack[n-1].object {
		return false
	}
	objects := 0
	for _, f := range stack {
		if f.object {
			objects++
		}
	}
	key := stack[n-1].key
	return (n == 1 && key == "template") || (objects > 1 && key == "language")
}
