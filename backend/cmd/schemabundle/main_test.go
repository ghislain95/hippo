package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	schemasDir  = "../../../docs/prompt/schemas"
	examplesDir = "../../../docs/prompt/schemas/examples"
	bundleFile  = "../../internal/analysis/schema/classify-item.bundled.json"
)

func TestBundle(t *testing.T) {
	bundled, err := Bundle(schemasDir, rootSchema)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	t.Run("leaves no external reference", func(t *testing.T) {
		var refs []string
		collectRefs(t, bundled, &refs)
		if len(refs) == 0 {
			t.Fatal("no $ref at all: the fiche schemas were not linked")
		}
		for _, ref := range refs {
			if !strings.HasPrefix(ref, "#/$defs/") {
				t.Errorf("external or nested $ref left: %q", ref)
			}
		}
	})

	t.Run("defines every fiche type", func(t *testing.T) {
		want := []string{"generic", "note", "place", "recipe", "resource", "tech_article", "tip", "tutorial"}
		got, err := ficheTemplates(bundled)
		if err != nil {
			t.Fatalf("ficheTemplates: %v", err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("fiche templates = %v, want %v", got, want)
		}
	})

	t.Run("is deterministic", func(t *testing.T) {
		again, err := Bundle(schemasDir, rootSchema)
		if err != nil {
			t.Fatalf("Bundle: %v", err)
		}
		if !bytes.Equal(bundled, again) {
			t.Error("two bundles of the same sources differ")
		}
	})

	t.Run("matches the committed bundle", func(t *testing.T) {
		committed, err := os.ReadFile(bundleFile)
		if err != nil {
			t.Fatalf("read committed bundle: %v", err)
		}
		if !bytes.Equal(bundled, committed) {
			t.Error("the committed bundle is out of date: run make generate")
		}
	})
}

func TestValidateExamples(t *testing.T) {
	bundled, err := Bundle(schemasDir, rootSchema)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	t.Run("accepts the examples of every fiche type", func(t *testing.T) {
		count, err := ValidateExamples(bundled, examplesDir)
		if err != nil {
			t.Fatalf("ValidateExamples: %v", err)
		}
		if count < 8 {
			t.Errorf("validated %d examples, want at least 8", count)
		}
	})

	t.Run("rejects an example that breaks its fiche schema", func(t *testing.T) {
		dir := copyExamples(t)
		breakExample(t, filepath.Join(dir, "recipe.json"), func(answer map[string]any) {
			delete(answer["fiche"].(map[string]any), "ingredients")
		})
		if _, err := ValidateExamples(bundled, dir); err == nil || !strings.Contains(err.Error(), "recipe.json") {
			t.Errorf("ValidateExamples() error = %v, want recipe.json rejected", err)
		}
	})

	t.Run("rejects an example with an invalid tag", func(t *testing.T) {
		dir := copyExamples(t)
		breakExample(t, filepath.Join(dir, "tip.json"), func(answer map[string]any) {
			answer["tags"] = []any{"Vinaigre Blanc", "menage"}
		})
		if _, err := ValidateExamples(bundled, dir); err == nil {
			t.Error("ValidateExamples() accepted a tag with capitals and a space")
		}
	})

	t.Run("requires an example for every fiche type", func(t *testing.T) {
		dir := copyExamples(t)
		if err := os.Remove(filepath.Join(dir, "place.json")); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if _, err := ValidateExamples(bundled, dir); err == nil || !strings.Contains(err.Error(), `"place"`) {
			t.Errorf("ValidateExamples() error = %v, want the missing place example reported", err)
		}
	})
}

func TestRunCheck(t *testing.T) {
	t.Run("fails when the bundle is stale", func(t *testing.T) {
		stale := filepath.Join(t.TempDir(), "bundle.json")
		if err := os.WriteFile(stale, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		err := run([]string{"-schemas", schemasDir, "-examples", examplesDir, "-out", stale, "-check"}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "out of date") {
			t.Errorf("run -check error = %v, want out of date", err)
		}
	})
}

func collectRefs(t *testing.T, bundled []byte, refs *[]string) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(bundled, &doc); err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if ref, ok := child.(string); ok && key == "$ref" {
					*refs = append(*refs, ref)
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(doc)
}

func copyExamples(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files, err := filepath.Glob(filepath.Join(examplesDir, "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(file)), raw, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func breakExample(t *testing.T, file string, mutate func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var answer map[string]any
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("parse: %v", err)
	}
	mutate(answer)
	raw, err = json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
