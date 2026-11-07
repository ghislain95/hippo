package schema_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis/schema"
)

// examples are the answers of docs/prompt/schemas/examples, one per fiche template.
const examples = "../../../../docs/prompt/schemas/examples"

// decode reads raw as the validator reads an answer: numbers kept as json.Number.
func decode(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestValidator(t *testing.T) {
	v, err := schema.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(examples, "*.json"))
	if err != nil || len(files) < 8 {
		t.Fatalf("examples = %v, %v; want one per template", files, err)
	}

	t.Run("every example validates", func(t *testing.T) {
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Validate(decode(t, raw)); err != nil {
				t.Errorf("%s: %v", filepath.Base(file), err)
			}
			template := strings.TrimSuffix(filepath.Base(file), ".json")
			if version, ok := v.FicheVersion(template); !ok || version < 1 {
				t.Errorf("FicheVersion(%s) = %d, %v", template, version, ok)
			}
		}
	})

	t.Run("a broken example does not validate, and the error quotes nothing of it", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(examples, "tip.json"))
		if err != nil {
			t.Fatal(err)
		}
		answer := decode(t, raw).(map[string]any)
		delete(answer, "title")
		answer["language"] = "DISTINCTIVE-LANGUAGE"
		err = v.Validate(answer)
		if !errors.Is(err, schema.ErrInvalid) {
			t.Fatalf("Validate = %v, want ErrInvalid", err)
		}
		if strings.Contains(err.Error(), "DISTINCTIVE") || strings.Contains(err.Error(), "title") {
			t.Errorf("error %q quotes the answer", err)
		}
	})

	t.Run("a tag outside the allowed form does not validate", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(examples, "tip.json"))
		if err != nil {
			t.Fatal(err)
		}
		answer := decode(t, raw).(map[string]any)
		answer["tags"] = []any{"c++", "go"}
		if err := v.Validate(answer); !errors.Is(err, schema.ErrInvalid) {
			t.Errorf("Validate = %v, want ErrInvalid", err)
		}
	})

	t.Run("the version of generic is 1", func(t *testing.T) {
		if version, ok := v.FicheVersion("generic"); !ok || version != 1 {
			t.Errorf("FicheVersion(generic) = %d, %v; want 1", version, ok)
		}
		if _, ok := v.FicheVersion("book"); ok {
			t.Error("FicheVersion(book) found a version")
		}
	})
}
