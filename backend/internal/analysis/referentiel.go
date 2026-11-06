package analysis

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// pathSeparator joins the name of a sub-category to its root's in the référentiel.
const pathSeparator = " › "

// oneLine replaces the line breaks of a value written on one line of the prompt by spaces.
var oneLine = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// LoadReferentiel reads the content types and the categories of userID and returns their
// référentiel. It is built once per batch, so that every request of the batch carries the same
// bytes (RG-52, prompt cache).
func LoadReferentiel(ctx context.Context, catalog Catalog, userID string) (string, error) {
	types, err := catalog.ContentTypes(ctx)
	if err != nil {
		return "", fmt.Errorf("build référentiel: %w", err)
	}
	categories, err := catalog.Categories(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("build référentiel: %w", err)
	}
	return Referentiel(types, categories), nil
}

// Referentiel writes the <referentiel> block of the system prompt (docs/prompt/system-prompt.md,
// RG-52) from the types and the categories that are not deleted:
//
//   - <types>: one "key | name" line per type by id, " | proposed" added for a proposed type;
//   - <categories>: one "id | path | status | description" line per category, the roots by id,
//     each followed by its sub-categories by id, the path joining names with " › "; a line
//     without description ends with "|". A sub-category whose root is not listed (deleted) is
//     not listed either;
//   - <tags>: empty until the tags exist (E3-04).
//
// A line break inside a name or a description becomes a space: one entry stays one line. The
// text ends with </referentiel>, without a line break.
func Referentiel(types []store.ContentType, categories []store.Category) string {
	var b strings.Builder
	b.WriteString("<referentiel>\n<types>\n")
	for _, t := range slices.SortedFunc(slices.Values(types), func(x, y store.ContentType) int { return cmp.Compare(x.ID, y.ID) }) {
		b.WriteString(oneLine.Replace(t.Key))
		b.WriteString(" | ")
		b.WriteString(oneLine.Replace(t.Name))
		if t.Status == "proposed" {
			b.WriteString(" | proposed")
		}
		b.WriteByte('\n')
	}
	b.WriteString("</types>\n<categories>\n")
	sorted := slices.SortedFunc(slices.Values(categories), func(x, y store.Category) int { return cmp.Compare(x.ID, y.ID) })
	for _, root := range sorted {
		if root.ParentID != nil {
			continue
		}
		rootName := oneLine.Replace(root.Name)
		writeCategory(&b, root, rootName)
		for _, sub := range sorted {
			if sub.ParentID != nil && *sub.ParentID == root.ID {
				writeCategory(&b, sub, rootName+pathSeparator+oneLine.Replace(sub.Name))
			}
		}
	}
	b.WriteString("</categories>\n<tags>\n</tags>\n</referentiel>")
	return b.String()
}

func writeCategory(b *strings.Builder, c store.Category, path string) {
	b.WriteString(strconv.FormatInt(c.ID, 10))
	b.WriteString(" | ")
	b.WriteString(path)
	b.WriteString(" | ")
	b.WriteString(c.Status)
	b.WriteString(" |")
	if c.Description != nil {
		if description := strings.TrimSpace(oneLine.Replace(*c.Description)); description != "" {
			b.WriteByte(' ')
			b.WriteString(description)
		}
	}
	b.WriteByte('\n')
}
