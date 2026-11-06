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
		want, err := ToolSchema(schema.ClassifyItem)
		if err != nil {
			t.Fatal(err)
		}
		if string(p.InputSchema) != string(want) {
			t.Error("the input schema is not the embedded bundled schema as a tool schema")
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

func TestToolSchema(t *testing.T) {
	t.Run("leaves out $schema and every x- keyword, at any depth", func(t *testing.T) {
		got, err := ToolSchema(schema.ClassifyItem)
		if err != nil {
			t.Fatalf("ToolSchema: %v", err)
		}
		var keywords []string
		walkKeys(t, got, func(key string) {
			if key == "$schema" || strings.HasPrefix(key, "x-") {
				keywords = append(keywords, key)
			}
		})
		if len(keywords) > 0 {
			t.Errorf("the tool schema keeps %v", keywords)
		}
		for _, kept := range []string{`"$defs"`, `"$ref"`, `"description"`, `"additionalProperties"`, `"required"`} {
			if !strings.Contains(string(got), kept) {
				t.Errorf("the tool schema lost %s", kept)
			}
		}
	})

	t.Run("keeps everything else with its meaning", func(t *testing.T) {
		got, err := ToolSchema(schema.ClassifyItem)
		if err != nil {
			t.Fatalf("ToolSchema: %v", err)
		}
		var want any
		if err := json.Unmarshal(schema.ClassifyItem, &want); err != nil {
			t.Fatal(err)
		}
		stripReference(want)
		var parsed any
		if err := json.Unmarshal(got, &parsed); err != nil {
			t.Fatalf("the tool schema is not JSON: %v", err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(parsed)
		if string(a) != string(b) {
			t.Error("the tool schema differs from the bundle beyond $schema and x- keywords")
		}
	})

	t.Run("keeps a property named x- and the data of enum, const, default and examples", func(t *testing.T) {
		in := `{"$schema":"https://json-schema.org/draft/2020-12/schema","x-v":1,"type":"object",` +
			`"properties":{"x-name":{"type":"string","x-hint":2,"enum":[{"x-data":1}]}},` +
			`"$defs":{"x-def":{"const":{"x-c":1},"default":{"x-d":1},"examples":[{"x-e":1}],"title":"T"}},` +
			`"anyOf":[{"x-a":1,"minimum":1.50}],"items":{"x-i":true,"type":"integer"}}`
		got, err := ToolSchema([]byte(in))
		if err != nil {
			t.Fatalf("ToolSchema: %v", err)
		}
		want := `{"$defs":{"x-def":{"const":{"x-c":1},"default":{"x-d":1},"examples":[{"x-e":1}],"title":"T"}},` +
			`"anyOf":[{"minimum":1.50}],"items":{"type":"integer"},` +
			`"properties":{"x-name":{"enum":[{"x-data":1}],"type":"string"}},"type":"object"}`
		if string(got) != want {
			t.Errorf("ToolSchema =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("refuses a schema that is not an object", func(t *testing.T) {
		for _, in := range []string{`[]`, `true`, `not json`} {
			if _, err := ToolSchema([]byte(in)); err == nil {
				t.Errorf("ToolSchema(%s) accepted it", in)
			}
		}
	})
}

// walkKeys calls visit with every object key of the JSON raw.
func walkKeys(t *testing.T, raw []byte, visit func(string)) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				visit(key)
				walk(value)
			}
		case []any:
			for _, value := range v {
				walk(value)
			}
		}
	}
	walk(doc)
}

// stripReference removes every $schema and x- key of doc, in place: the bundle has neither as
// a property name nor in its data, so a blind walk is the reference.
func stripReference(node any) {
	switch v := node.(type) {
	case map[string]any:
		for key, value := range v {
			if key == "$schema" || strings.HasPrefix(key, "x-") {
				delete(v, key)
				continue
			}
			stripReference(value)
		}
	case []any:
		for _, value := range v {
			stripReference(value)
		}
	}
}
