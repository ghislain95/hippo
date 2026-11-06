// Package prompt gives the parts of an analysis request that come from the prompt contract of
// docs/prompt/ (ADR-0005, ADR-0006): the fixed part of the system prompt and its version, and
// the classify_item tool with the version of its envelope.
//
// The fixed part is embedded through prompt.gen.json, generated from
// docs/prompt/system-prompt.md by cmd/schemabundle (go:embed cannot reach docs/): never edit it,
// change the prompt and run `make generate`; `make check-generated` fails when it drifts.
package prompt

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ghislain95/hippo/backend/internal/analysis/schema"
)

//go:embed prompt.gen.json
var generated []byte

// Prompt is the part of every analysis request that does not depend on the item nor on the user.
type Prompt struct {
	// Version is the prompt_version of system-prompt.md, recorded with each batch (RG-64).
	Version int
	// Fixed is the fixed part of the system prompt, byte for byte.
	Fixed string
	// EnvelopeVersion is the x-schema-version of classify-item.schema.json, recorded with each
	// batch (RG-64).
	EnvelopeVersion int
	// ToolDescription is the description of the classify_item tool: the root description of
	// its schema.
	ToolDescription string
	// InputSchema is the bundled classify_item schema, as embedded.
	InputSchema json.RawMessage
}

// Load reads the embedded prompt and schema. An error means a broken build: the service does
// not start.
func Load() (Prompt, error) {
	return load(generated, schema.ClassifyItem)
}

func load(promptCopy, bundled []byte) (Prompt, error) {
	var copied struct {
		PromptVersion int    `json:"prompt_version"`
		Fixed         string `json:"fixed"`
	}
	if err := json.Unmarshal(promptCopy, &copied); err != nil {
		return Prompt{}, fmt.Errorf("read the embedded prompt: %w", err)
	}
	if copied.PromptVersion < 1 {
		return Prompt{}, errors.New("read the embedded prompt: no prompt_version")
	}
	if copied.Fixed == "" {
		return Prompt{}, errors.New("read the embedded prompt: no fixed part")
	}
	var root struct {
		Description   string `json:"description"`
		SchemaVersion *int   `json:"x-schema-version"`
	}
	if err := json.Unmarshal(bundled, &root); err != nil {
		return Prompt{}, fmt.Errorf("read the embedded classify_item schema: %w", err)
	}
	if root.SchemaVersion == nil || *root.SchemaVersion < 1 {
		return Prompt{}, errors.New("read the embedded classify_item schema: no x-schema-version at its root")
	}
	if root.Description == "" {
		return Prompt{}, errors.New("read the embedded classify_item schema: no description at its root")
	}
	return Prompt{
		Version:         copied.PromptVersion,
		Fixed:           copied.Fixed,
		EnvelopeVersion: *root.SchemaVersion,
		ToolDescription: root.Description,
		InputSchema:     json.RawMessage(bundled),
	}, nil
}
