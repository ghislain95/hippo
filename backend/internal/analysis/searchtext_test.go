package analysis_test

import (
	"encoding/json"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis"
)

func TestSearchText(t *testing.T) {
	for _, tc := range []struct {
		name, fiche, want string
	}{
		{
			"emits every string in document order, without the template nor a code block's language",
			`{"template":"generic","summary":"Résumé A","sections":[{"heading":"Titre B","body":"Corps C",` +
				`"code":[{"language":"bash","code":"ls -la","caption":null}]}],"key_points":["Point D"]}`,
			"Résumé A\nTitre B\nCorps C\nls -la\nPoint D",
		},
		{
			"keeps a root member named language, numbers and booleans are left out",
			`{"template":"resource","language":"fr","count":3,"free":true,"links":[{"label":"template","url":"https://example.com"}]}`,
			"fr\ntemplate\nhttps://example.com",
		},
		{
			"keeps a nested template member and empty objects and arrays",
			`{"template":"note","a":{},"b":[],"c":{"template":"x","d":[["deep"]]}}`,
			"x\ndeep",
		},
		{"an empty fiche has no text", `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := analysis.SearchText(json.RawMessage(tc.fiche))
			if err != nil || got != tc.want {
				t.Errorf("SearchText = %q, %v; want %q", got, err, tc.want)
			}
		})
	}

	t.Run("a fiche that is not JSON is an error", func(t *testing.T) {
		if _, err := analysis.SearchText(json.RawMessage(`{"a":`)); err == nil {
			t.Error("SearchText accepted a broken fiche")
		}
	})
}
