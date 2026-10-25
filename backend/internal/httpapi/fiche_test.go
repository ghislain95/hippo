package httpapi_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/httpapi"
)

const ficheExamples = "../../../docs/prompt/schemas/examples"

// TestFicheExamplesDecode checks that the generated Fiche union accepts every fiche example
// (docs/prompt/schemas/examples) and that its discriminator returns the type of its template.
func TestFicheExamplesDecode(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(ficheExamples, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fiche example in %s (%v)", ficheExamples, err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var answer struct {
				Fiche httpapi.Fiche `json:"fiche"`
			}
			if err := json.Unmarshal(raw, &answer); err != nil {
				t.Fatalf("decode into the generated Fiche: %v", err)
			}
			template, err := answer.Fiche.Discriminator()
			if err != nil {
				t.Fatalf("Discriminator: %v", err)
			}
			value, err := answer.Fiche.ValueByDiscriminator()
			if err != nil {
				t.Fatalf("ValueByDiscriminator(%q): %v", template, err)
			}
			want := "httpapi.Fiche" + pascal(template)
			if got := fmt.Sprintf("%T", value); got != want {
				t.Errorf("template %q decodes as %s, want %s", template, got, want)
			}
		})
	}
}

func pascal(key string) string {
	parts := strings.Split(key, "_")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}
