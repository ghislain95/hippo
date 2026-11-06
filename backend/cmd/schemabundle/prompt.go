package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// fixedPartHeading is the heading of the section of docs/prompt/system-prompt.md whose single
// ```text fence is the fixed part of the system prompt.
const fixedPartHeading = "## Partie fixe"

// promptVersionPattern finds the prompt version in the header of system-prompt.md, written
// `prompt_version = N`.
var promptVersionPattern = regexp.MustCompile("`prompt_version = ([0-9]+)`")

// promptCopy is the generated copy of the fixed part of the prompt: go:embed cannot reach
// docs/, so the backend embeds this file (design.md D4). Its keys keep this order.
type promptCopy struct {
	PromptVersion int    `json:"prompt_version"`
	Fixed         string `json:"fixed"`
}

// PromptCopy returns the generated copy of the system prompt source (the content of
// system-prompt.md): its prompt version, read from the `prompt_version = N` of its header (the
// text before the first section), and its fixed part, the text of the only fence of its
// « Partie fixe » section, which must be a ```text fence, byte for byte without the fence lines.
func PromptCopy(source []byte) ([]byte, error) {
	text := string(source)
	header, _, _ := strings.Cut(text, "\n## ")
	versions := promptVersionPattern.FindAllStringSubmatch(header, -1)
	if len(versions) != 1 {
		return nil, fmt.Errorf("system prompt: %d `prompt_version = N` in its header, want exactly one", len(versions))
	}
	version, err := strconv.Atoi(versions[0][1])
	if err != nil || version < 1 {
		return nil, fmt.Errorf("system prompt: prompt_version %q is not a positive integer", versions[0][1])
	}
	fixed, err := fixedPart(text)
	if err != nil {
		return nil, fmt.Errorf("system prompt: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keeps <element> and <contenu> readable in the generated file
	enc.SetIndent("", "  ")
	if err := enc.Encode(promptCopy{PromptVersion: version, Fixed: fixed}); err != nil {
		return nil, fmt.Errorf("system prompt: encode the copy: %w", err)
	}
	return buf.Bytes(), nil
}

// fixedPart returns the text of the only fence of the « Partie fixe » section of text, which
// runs to the next heading of the same level.
func fixedPart(text string) (string, error) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if line == fixedPartHeading {
			if start >= 0 {
				return "", fmt.Errorf("two %q sections", fixedPartHeading)
			}
			start = i + 1
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no %q section", fixedPartHeading)
	}

	var (
		fences  [][]string
		opening string
		body    []string
		inFence bool
	)
	for _, line := range lines[start:] {
		if !inFence && strings.HasPrefix(line, "## ") {
			break
		}
		switch {
		case !inFence && strings.HasPrefix(line, "```"):
			inFence, opening, body = true, line, []string{}
		case inFence && line == "```":
			if opening != "```text" {
				return "", fmt.Errorf("the fence of %q opens with %q, want ```text", fixedPartHeading, opening)
			}
			fences, inFence = append(fences, body), false
		case inFence && strings.HasPrefix(line, "```"):
			// The fence was not closed where meant: the next one opens inside it.
			return "", fmt.Errorf("a fence of %q is not closed before %q", fixedPartHeading, line)
		case inFence:
			body = append(body, line)
		}
	}
	if inFence {
		return "", fmt.Errorf("a fence of %q is not closed", fixedPartHeading)
	}
	if len(fences) != 1 {
		return "", fmt.Errorf("%d fences in %q, want exactly one ```text fence", len(fences), fixedPartHeading)
	}
	fixed := strings.Join(fences[0], "\n")
	if strings.TrimSpace(fixed) == "" {
		return "", errors.New("the fixed part is empty")
	}
	return fixed, nil
}
