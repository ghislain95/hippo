// Package prompt gives the parts of an analysis request that come from the prompt contract of
// docs/prompt/ (ADR-0005, ADR-0006): the fixed part of the system prompt and its version, and
// the classify_item tool with the version of its envelope.
//
// The fixed part is embedded through prompt.gen.json, generated from
// docs/prompt/system-prompt.md by cmd/schemabundle (go:embed cannot reach docs/): never edit it,
// change the prompt and run `make generate`; `make check-generated` fails when it drifts.
package prompt

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	// InputSchema is the bundled classify_item schema as the tool's input schema: the embedded
	// bundle without its "$schema" keyword nor any "x-*" keyword, at any depth (ToolSchema).
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
	input, err := ToolSchema(bundled)
	if err != nil {
		return Prompt{}, fmt.Errorf("read the embedded classify_item schema: %w", err)
	}
	return Prompt{
		Version:         copied.PromptVersion,
		Fixed:           copied.Fixed,
		EnvelopeVersion: *root.SchemaVersion,
		ToolDescription: root.Description,
		InputSchema:     input,
	}, nil
}

// ToolSchema returns the JSON Schema bundled as the input schema of a tool: the API promises
// only « a JSON Schema object », so the "$schema" keyword (the dialect) and every extension
// keyword "x-*" (x-schema-version, Hippo's own) are left out, at any depth. Everything else
// keeps its meaning: $defs, $ref, title, description, every constraint. Only keywords go: a
// property or a definition named "x-…" stays, and the values of enum, const, default and
// examples are data, copied as they are. Keys come out sorted (encoding/json), numbers as
// written.
func ToolSchema(bundled []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(bundled))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("tool schema: %w", err)
	}
	if _, ok := root.(map[string]any); !ok {
		return nil, errors.New("tool schema: the schema is not an object")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(toolSchema(root)); err != nil {
		return nil, fmt.Errorf("tool schema: %w", err)
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// toolSchema strips a schema (an object) of its "$schema" and "x-*" keywords, and recurses into
// its subschemas.
func toolSchema(node any) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			switch {
			case key == "$schema" || strings.HasPrefix(key, "x-"):
				// left out
			case namedSchemas(key):
				out[key] = schemaMap(value)
			case dataKeyword(key):
				out[key] = value
			default:
				out[key] = toolSchema(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, value := range v {
			out[i] = toolSchema(value)
		}
		return out
	default:
		return node
	}
}

// schemaMap strips each schema of a map of names to schemas, keeping every name.
func schemaMap(node any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return node
	}
	out := make(map[string]any, len(m))
	for name, value := range m {
		out[name] = toolSchema(value)
	}
	return out
}

// namedSchemas tells whether the value of key maps names (never keywords) to schemas.
func namedSchemas(key string) bool {
	switch key {
	case "$defs", "definitions", "properties", "patternProperties", "dependentSchemas":
		return true
	}
	return false
}

// dataKeyword tells whether the value of key is data, not a schema.
func dataKeyword(key string) bool {
	switch key {
	case "enum", "const", "default", "examples":
		return true
	}
	return false
}
