package analysis_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// referentielOf builds the batch of userID, with one waiting note, and returns the référentiel
// its request carries.
func (e *env) referentielOf(t *testing.T, userID string) string {
	t.Helper()
	id := e.note(t, userID, "Une idée")
	_, parsed := e.build(t, userID)
	return parsed.of(t, id).referentiel(t)
}

// refBlock returns the lines between <name> and </name> of the référentiel ref.
func refBlock(t *testing.T, ref, name string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(ref, "\n<"+name+">\n")
	if !ok {
		t.Fatalf("no <%s> block in the référentiel:\n%s", name, ref)
	}
	inner, _, ok := strings.Cut(rest, "</"+name+">\n")
	if !ok {
		t.Fatalf("<%s> block not closed", name)
	}
	if inner == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(inner, "\n"), "\n")
}

// categoryLine matches a line of the <categories> block: id | path | status |[ description].
var categoryLine = regexp.MustCompile(`^([0-9]+) \| ([^|]+) \| (active|proposed) \|( [^|]+)?$`)

// indexOf returns the index of the first line that equals want, or -1.
func indexOf(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	return -1
}

// categoryID returns the id of the category name of userID.
func (e *env) categoryID(t *testing.T, userID, name string) int64 {
	t.Helper()
	categories, err := e.st.Categories(context.Background(), userID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	for _, c := range categories {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("no category %q", name)
	return 0
}

func TestReferentiel(t *testing.T) {
	ctx := context.Background()

	t.Run("The types are listed for the AI", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		got := refBlock(t, e.referentielOf(t, userID), "types")
		want := []string{
			"tech_article | Article technique", "tutorial | Tuto", "tip | Astuce", "recipe | Recette",
			"place | Lieu", "resource | Ressource", "note | Note perso",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("<types> =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("Two users see the same types", func(t *testing.T) {
		e := newEnv(t)
		first, second := e.user(t, "first"), e.user(t, "second")
		a := refBlock(t, e.referentielOf(t, first), "types")
		b := refBlock(t, e.referentielOf(t, second), "types")
		if len(a) != 7 || strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("types differ:\n%v\n%v", a, b)
		}
	})

	t.Run("A new user gets the starting tree", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		lines := refBlock(t, e.referentielOf(t, userID), "categories")
		roots, subs := 0, 0
		for _, line := range lines {
			m := categoryLine.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("line %q does not read id | path | status | description", line)
			}
			if m[3] != "active" {
				t.Errorf("line %q is not active", line)
			}
			if strings.Contains(m[2], " › ") {
				subs++
				if m[4] != "" {
					t.Errorf("sub-category %q has a description", line)
				}
				continue
			}
			roots++
			if m[4] == "" {
				t.Errorf("root %q has no description", line)
			}
		}
		if len(lines) != 24 || roots != 9 || subs != 15 {
			t.Errorf("%d lines, %d roots, %d sub-categories; want 24, 9, 15", len(lines), roots, subs)
		}
	})

	t.Run("Deleting every category does not bring them back", func(t *testing.T) {
		e := newEnv(t)
		userID, err := e.st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		categories, err := e.st.Categories(ctx, userID)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		// Sub-categories first, as a user would empty a category before deleting it.
		for i := len(categories) - 1; i >= 0; i-- {
			storetest.DeleteCategory(t, e.path, userID, categories[i].ID)
		}
		if _, err := e.st.EnsureSingleUser(ctx, "hash"); err != nil { // the backend starts again
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		ref := e.referentielOf(t, userID)
		if lines := refBlock(t, ref, "categories"); len(lines) != 0 {
			t.Errorf("<categories> lists %d lines, want none", len(lines))
		}
		if !strings.Contains(ref, "\n<categories>\n</categories>\n") {
			t.Errorf("the empty <categories> block is not written as such:\n%s", ref)
		}
	})

	t.Run("A user never sees another user's categories", func(t *testing.T) {
		e := newEnv(t)
		first, second := e.user(t, "first"), e.user(t, "second")
		storetest.AddCategory(t, e.path, second, storetest.NewCategory{
			Name: "Jardin secret", Description: ptr("Ce que garde le second utilisateur."), Status: "active",
		})
		ref := e.referentielOf(t, first)
		if strings.Contains(ref, "Jardin secret") {
			t.Error("the first user's référentiel names the second user's category")
		}
		mine, err := e.st.Categories(ctx, first)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		owned := map[string]bool{}
		for _, c := range mine {
			owned[strconv.FormatInt(c.ID, 10)] = true
		}
		for _, line := range refBlock(t, ref, "categories") {
			if id, _, _ := strings.Cut(line, " | "); !owned[id] {
				t.Errorf("line %q lists a category that is not the first user's", line)
			}
		}
		if !strings.Contains(e.referentielOf(t, second), " | Jardin secret | active | ") {
			t.Error("the second user's référentiel lacks their own category")
		}
	})

	t.Run("A sub-category follows its root with its path", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		lines := refBlock(t, e.referentielOf(t, userID), "categories")
		dev := indexOf(lines, fmt.Sprintf("%d | Dev | active | Développement logiciel : langages, frameworks, bases de données, conception, tests.",
			e.categoryID(t, userID, "Dev")))
		java := indexOf(lines, fmt.Sprintf("%d | Dev › Java & Spring | active |", e.categoryID(t, userID, "Java & Spring")))
		ia := indexOf(lines, fmt.Sprintf("%d | IA | active | Intelligence artificielle, en particulier appliquée au développement.",
			e.categoryID(t, userID, "IA")))
		if dev < 0 || java < 0 || ia < 0 || dev >= java || java >= ia {
			t.Errorf("lines of Dev, Java & Spring, IA at %d, %d, %d; want all present, in this order:\n%s",
				dev, java, ia, strings.Join(lines, "\n"))
		}
	})

	t.Run("A root category carries its description", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		want := fmt.Sprintf("%d | Cuisine | active | Recettes, techniques et astuces de cuisine.", e.categoryID(t, userID, "Cuisine"))
		if indexOf(refBlock(t, e.referentielOf(t, userID), "categories"), want) < 0 {
			t.Errorf("no line %q", want)
		}
	})

	t.Run("Proposed categories are listed with their status", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		infra := e.categoryID(t, userID, "Infra & DevOps")
		id := storetest.AddCategory(t, e.path, userID, storetest.NewCategory{
			Name: "Kubernetes", Description: ptr("Orchestration de conteneurs avec Kubernetes."), ParentID: &infra,
			Status: "proposed", ProposalReason: ptr("Plusieurs articles sur Kubernetes."),
		})
		lines := refBlock(t, e.referentielOf(t, userID), "categories")
		got := indexOf(lines, fmt.Sprintf("%d | Infra & DevOps › Kubernetes | proposed | Orchestration de conteneurs avec Kubernetes.", id))
		last := indexOf(lines, fmt.Sprintf("%d | Infra & DevOps › Auto-hébergement | active |", e.categoryID(t, userID, "Auto-hébergement")))
		if got < 0 || last < 0 || got != last+1 {
			t.Errorf("Kubernetes at line %d, last starting sub-category of Infra & DevOps at %d; want right after:\n%s",
				got, last, strings.Join(lines, "\n"))
		}
	})

	t.Run("A deleted category is not listed", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		storetest.DeleteCategory(t, e.path, userID, e.categoryID(t, userID, "Plantes"))
		if ref := e.referentielOf(t, userID); strings.Contains(ref, "Plantes") {
			t.Errorf("the référentiel names a deleted category:\n%s", ref)
		}
	})

	t.Run("The tags block is empty", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		ref := e.referentielOf(t, userID)
		if !strings.HasSuffix(ref, "\n</categories>\n<tags>\n</tags>\n</referentiel>") || !strings.HasPrefix(ref, "<referentiel>\n<types>\n") {
			t.Errorf("référentiel does not end with an empty <tags> block then </referentiel>:\n%s", ref)
		}
	})
}

func TestReferentielFormat(t *testing.T) {
	types := []store.ContentType{
		{ID: 1, Key: "tech_article", Name: "Article technique", Status: "active"},
		{ID: 8, Key: "podcast", Name: "Podcast\naudio", Status: "proposed"},
	}
	parent := int64(3)
	gone := int64(99)
	categories := []store.Category{
		{ID: 3, Name: "Dev", Description: ptr("Sur\r\ndeux lignes"), Status: "active"},
		{ID: 4, Name: "Java\nSpring", ParentID: &parent, Status: "active"},
		{ID: 5, Name: "Orpheline", ParentID: &gone, Status: "active"},
		{ID: 6, Name: "Vide", Description: ptr(""), Status: "proposed"},
	}
	got := analysis.Referentiel(types, categories)
	want := "<referentiel>\n<types>\ntech_article | Article technique\npodcast | Podcast audio | proposed\n</types>\n" +
		"<categories>\n3 | Dev | active | Sur deux lignes\n4 | Dev › Java Spring | active |\n6 | Vide | proposed |\n</categories>\n" +
		"<tags>\n</tags>\n</referentiel>"

	t.Run("a proposed type ends with its status, and a line break never splits a line", func(t *testing.T) {
		if got != want {
			t.Errorf("Referentiel =\n%s\nwant\n%s", got, want)
		}
	})
}
