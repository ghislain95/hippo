package schema

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompileVersions(t *testing.T) {
	t.Run("a start-up error when the bundle has no version for a template", func(t *testing.T) {
		broken := bytes.Replace(ClassifyItem, []byte(`"title": "Fiche : astuce",
      "type": "object",
      "x-schema-version": 1`), []byte(`"title": "Fiche : astuce",
      "type": "object"`), 1)
		if bytes.Equal(broken, ClassifyItem) {
			t.Fatal("the fixture did not remove the version of fiche_tip: the bundle layout changed")
		}
		_, err := compile(broken)
		if err == nil || !strings.Contains(err.Error(), "fiche_tip") {
			t.Errorf("compile = %v, want an error naming fiche_tip", err)
		}
	})

	t.Run("a bundle that is not JSON does not compile", func(t *testing.T) {
		if _, err := compile([]byte("{")); err == nil {
			t.Error("compile accepted a broken bundle")
		}
	})
}
