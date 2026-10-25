package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const openAPIFile = "../../../api/fiches.gen.json"

type componentsDoc struct {
	Components struct {
		Schemas map[string]map[string]any `json:"schemas"`
	} `json:"components"`
}

func loadComponents(t *testing.T) componentsDoc {
	t.Helper()
	raw, err := OpenAPIComponents(schemasDir)
	if err != nil {
		t.Fatalf("OpenAPIComponents: %v", err)
	}
	var doc componentsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse components: %v", err)
	}
	return doc
}

// ficheFiles lists the fiche schema files, the definitions file excepted.
func ficheFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(schemasDir, "fiches", "*.schema.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var fiches []string
	for _, file := range files {
		if !strings.HasPrefix(filepath.Base(file), "_") {
			fiches = append(fiches, file)
		}
	}
	return fiches
}

func TestOpenAPIComponents(t *testing.T) {
	doc := loadComponents(t)
	schemas := doc.Components.Schemas

	t.Run("emits one component per fiche file", func(t *testing.T) {
		for _, file := range ficheFiles(t) {
			name := "Fiche" + pascalCase(strings.TrimSuffix(filepath.Base(file), ".schema.json"))
			if _, ok := schemas[name]; !ok {
				t.Errorf("no component %s for %s", name, filepath.Base(file))
			}
		}
		if _, ok := schemas["FicheTechArticle"]; !ok {
			t.Error("no FicheTechArticle component")
		}
	})

	t.Run("names the common definitions after the fiche namespace", func(t *testing.T) {
		for _, name := range []string{"FicheText", "FicheTextList", "FicheCodeBlock", "FicheSection", "FicheLink"} {
			if _, ok := schemas[name]; !ok {
				t.Errorf("no component %s", name)
			}
		}
	})

	t.Run("drops $id and $schema", func(t *testing.T) {
		raw, err := OpenAPIComponents(schemasDir)
		if err != nil {
			t.Fatalf("OpenAPIComponents: %v", err)
		}
		for _, key := range []string{`"$id"`, `"$schema"`} {
			if strings.Contains(string(raw), key) {
				t.Errorf("components still contain %s", key)
			}
		}
	})

	t.Run("types every template constant as a string", func(t *testing.T) {
		for name, schema := range schemas {
			template, ok := property(schema, "template")
			if !ok {
				continue
			}
			if template["type"] != "string" || template["const"] == nil {
				t.Errorf("%s.template = %v, want a string const", name, template)
			}
		}
	})

	t.Run("points every reference inside the components", func(t *testing.T) {
		var refs []string
		raw, _ := json.Marshal(schemas)
		collectRefs(t, raw, &refs)
		if len(refs) == 0 {
			t.Fatal("no reference: the common definitions were not linked")
		}
		for _, ref := range refs {
			target, found := strings.CutPrefix(ref, "#/components/schemas/")
			if !found {
				t.Errorf("reference %q does not point inside the components", ref)
				continue
			}
			if _, ok := schemas[target]; !ok {
				t.Errorf("reference %q points to a missing component", ref)
			}
		}
	})

	t.Run("discriminates the Fiche union on every template constant", func(t *testing.T) {
		fiche, ok := schemas["Fiche"]
		if !ok {
			t.Fatal("no Fiche component")
		}
		discriminator, _ := fiche["discriminator"].(map[string]any)
		if discriminator["propertyName"] != "template" {
			t.Fatalf("discriminator = %v, want propertyName template", discriminator)
		}
		mapping, _ := discriminator["mapping"].(map[string]any)
		var keys []string
		for key, target := range mapping {
			keys = append(keys, key)
			name := strings.TrimPrefix(target.(string), "#/components/schemas/")
			template, _ := property(schemas[name], "template")
			if template["const"] != key {
				t.Errorf("mapping %q points to %s whose template is %v", key, name, template["const"])
			}
		}
		sort.Strings(keys)
		// The expected set is derived from the fiche files themselves (design D1).
		var want []string
		for _, file := range ficheFiles(t) {
			schema, err := readSchema(file)
			if err != nil {
				t.Fatalf("readSchema: %v", err)
			}
			template, _ := property(schema, "template")
			want = append(want, template["const"].(string))
		}
		sort.Strings(want)
		if strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Errorf("mapping keys = %v, want %v", keys, want)
		}
		if oneOf, _ := fiche["oneOf"].([]any); len(oneOf) != len(want) {
			t.Errorf("Fiche.oneOf has %d entries, want %d", len(oneOf), len(want))
		}
	})

	t.Run("is deterministic", func(t *testing.T) {
		first, err := OpenAPIComponents(schemasDir)
		if err != nil {
			t.Fatalf("OpenAPIComponents: %v", err)
		}
		second, err := OpenAPIComponents(schemasDir)
		if err != nil {
			t.Fatalf("OpenAPIComponents: %v", err)
		}
		if string(first) != string(second) {
			t.Error("two runs on the same sources differ")
		}
	})

	t.Run("matches the committed components", func(t *testing.T) {
		raw, err := OpenAPIComponents(schemasDir)
		if err != nil {
			t.Fatalf("OpenAPIComponents: %v", err)
		}
		committed, err := os.ReadFile(openAPIFile)
		if err != nil {
			t.Fatalf("read committed components: %v", err)
		}
		if string(raw) != string(committed) {
			t.Error("the committed fiche components are out of date: run make generate")
		}
	})
}

func property(schema map[string]any, name string) (map[string]any, bool) {
	properties, _ := schema["properties"].(map[string]any)
	value, ok := properties[name].(map[string]any)
	return value, ok
}

// fixtureSchemas writes a minimal schemas directory: _common.schema.json plus the given fiche
// files (name → content), and returns its path.
func fixtureSchemas(t *testing.T, fiches map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fiches"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	common := `{"$defs": {"text": {"type": "string"}, "textList": {"type": "array", "items": {"$ref": "#/$defs/text"}}}}`
	files := map[string]string{"_common.schema.json": common}
	for name, content := range fiches {
		files[name] = content
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, "fiches", name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestOpenAPIComponentsErrors(t *testing.T) {
	const tip = `{"type": "object", "properties": {"template": {"const": "tip"}, "tip": {"$ref": "_common.schema.json#/$defs/text"}}}`
	cases := []struct {
		name   string
		fiches map[string]string
		want   string
	}{
		{"rejects an empty fiches directory", map[string]string{}, "no fiche schema"},
		{"rejects a fiche without template constant", map[string]string{
			"tip.schema.json": `{"type": "object", "properties": {"tip": {"type": "string"}}}`,
		}, "template.const is missing"},
		{"rejects two fiches with the same template", map[string]string{
			"tip.schema.json":   tip,
			"other.schema.json": tip,
		}, "already used"},
		{"rejects a fiche named like a common definition", map[string]string{
			"text.schema.json": `{"type": "object", "properties": {"template": {"const": "text"}}}`,
		}, "defined twice"},
		{"rejects a reference outside the common definitions", map[string]string{
			"tip.schema.json": `{"type": "object", "properties": {"template": {"const": "tip"}, "x": {"$ref": "other.schema.json#/$defs/text"}}}`,
		}, "points outside"},
		{"rejects a nested reference", map[string]string{
			"tip.schema.json": `{"type": "object", "properties": {"template": {"const": "tip"}, "x": {"$ref": "_common.schema.json#/$defs/text/items"}}}`,
		}, "unsupported $ref"},
		{"rejects two common definitions with the same component name", map[string]string{
			"_common.schema.json": `{"$defs": {"code_block": {"type": "string"}, "codeBlock": {"type": "string"}}}`,
			"tip.schema.json":     tip,
		}, "defined twice"},
		{"rejects a definitions file without $defs", map[string]string{
			"_common.schema.json": `{"title": "nothing"}`,
			"tip.schema.json":     tip,
		}, "has no $defs"},
		{"rejects a local reference in a fiche", map[string]string{
			"tip.schema.json": `{"type": "object", "properties": {"template": {"const": "tip"}, "x": {"$ref": "#/$defs/text"}}}`,
		}, "local $ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenAPIComponents(fixtureSchemas(t, tc.fiches))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("OpenAPIComponents() error = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	t.Run("accepts a valid minimal set", func(t *testing.T) {
		if _, err := OpenAPIComponents(fixtureSchemas(t, map[string]string{"tip.schema.json": tip})); err != nil {
			t.Errorf("OpenAPIComponents: %v", err)
		}
	})
}
