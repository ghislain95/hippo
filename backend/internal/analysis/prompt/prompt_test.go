package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis/schema"
)

func TestLoad(t *testing.T) {
	t.Run("gives the prompt version 3 and the envelope version 1", func(t *testing.T) {
		p, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if p.Version != 3 || p.EnvelopeVersion != 1 {
			t.Errorf("versions = prompt %d, envelope %d; want 3 and 1", p.Version, p.EnvelopeVersion)
		}
	})

	t.Run("gives the fixed part of the prompt", func(t *testing.T) {
		p, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !strings.HasPrefix(p.Fixed, "Tu ranges les contenus du deuxième cerveau") || !strings.HasSuffix(p.Fixed, "false.") {
			t.Errorf("fixed part starts %.40q", p.Fixed)
		}
	})

	t.Run("gives the tool's description and the bundled schema", func(t *testing.T) {
		p, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !strings.HasPrefix(p.ToolDescription, "Range un élément") {
			t.Errorf("tool description = %q", p.ToolDescription)
		}
		if string(p.InputSchema) != string(schema.ClassifyItem) {
			t.Error("the input schema is not the embedded bundled schema")
		}
	})

	t.Run("fails without the envelope version", func(t *testing.T) {
		var doc map[string]any
		if err := json.Unmarshal(schema.ClassifyItem, &doc); err != nil {
			t.Fatal(err)
		}
		delete(doc, "x-schema-version")
		withoutVersion, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := load(generated, withoutVersion); err == nil || !strings.Contains(err.Error(), "x-schema-version") {
			t.Errorf("load() error = %v, want x-schema-version missing", err)
		}
	})

	broken := []struct {
		name, copy string
	}{
		{"fails without the prompt version", `{"fixed":"Tu ranges"}`},
		{"fails without the fixed part", `{"prompt_version":3,"fixed":""}`},
		{"fails on a copy that is not JSON", `prompt`},
	}
	for _, tc := range broken {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := load([]byte(tc.copy), schema.ClassifyItem); err == nil {
				t.Error("load() accepted a broken copy of the prompt")
			}
		})
	}
}
