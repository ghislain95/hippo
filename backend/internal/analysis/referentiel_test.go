package analysis_test

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
}

// carry attaches the tag name of userID, created when new, to n new public notes of userID.
func (e *env) carry(t *testing.T, userID, name string, n int, tags map[string]int64) []string {
	t.Helper()
	id, ok := tags[name]
	if !ok {
		id = storetest.AddTag(t, e.path, userID, name)
		tags[name] = id
	}
	var items []string
	for i := range n {
		item := e.note(t, userID, fmt.Sprintf("Note %s %d", name, i))
		storetest.AttachTag(t, e.path, userID, item, id)
		items = append(items, item)
	}
	return items
}

func TestReferentielTags(t *testing.T) {
	ctx := context.Background()

	t.Run("The tags block is empty", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		ref := e.referentielOf(t, userID)
		if !strings.HasSuffix(ref, "\n</categories>\n<tags>\n</tags>\n</referentiel>") || !strings.HasPrefix(ref, "<referentiel>\n<types>\n") {
			t.Errorf("a user without any tag has a non-empty <tags> block:\n%s", ref)
		}
	})

	t.Run("Tags are listed most used first, then by name", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		tags := map[string]int64{}
		// Created in another order than the expected one, so that neither the id nor the
		// creation order gives it.
		e.carry(t, userID, "docker", 1, tags)
		e.carry(t, userID, "go", 3, tags)
		e.carry(t, userID, "ansible", 1, tags)
		got := refBlock(t, e.referentielOf(t, userID), "tags")
		if len(got) != 1 || got[0] != "go, ansible, docker" {
			t.Errorf("<tags> = %q, want the single line go, ansible, docker", got)
		}
	})

	t.Run("At most 300 tags are listed", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		carriers := []string{e.note(t, userID, "Premier"), e.note(t, userID, "Second")}
		for i := range 300 {
			tag := storetest.AddTag(t, e.path, userID, fmt.Sprintf("tag-%03d", i))
			for _, item := range carriers {
				storetest.AttachTag(t, e.path, userID, item, tag)
			}
		}
		storetest.AttachTag(t, e.path, userID, carriers[0], storetest.AddTag(t, e.path, userID, "zzz-rare"))
		got := refBlock(t, e.referentielOf(t, userID), "tags")
		if len(got) != 1 {
			t.Fatalf("<tags> holds %d lines, want 1", len(got))
		}
		names := strings.Split(got[0], ", ")
		if len(names) != 300 || slices.Contains(names, "zzz-rare") {
			t.Errorf("<tags> lists %d names (zzz-rare: %v), want 300 without zzz-rare", len(names), slices.Contains(names, "zzz-rare"))
		}
	})

	t.Run("A tag carried only by private items is not listed", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		tags := map[string]int64{}
		private := e.carry(t, userID, "sante", 1, tags)
		storetest.SetAnalysisState(t, e.path, userID, private[0], "analyzed", ptr(time.Now()), 0)
		if _, err := e.st.SetPrivate(ctx, userID, private[0], true); err != nil {
			t.Fatalf("SetPrivate: %v", err)
		}
		e.carry(t, userID, "go", 1, tags)
		got := refBlock(t, e.referentielOf(t, userID), "tags")
		if len(got) != 1 || got[0] != "go" {
			t.Errorf("<tags> = %q, want go and not sante", got)
		}
	})

	t.Run("A tag carried only by deleted items is not listed", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		tags := map[string]int64{}
		deleted := e.carry(t, userID, "ancien", 1, tags)
		storetest.DeleteItem(t, e.path, userID, deleted[0])
		e.carry(t, userID, "go", 1, tags)
		if got := refBlock(t, e.referentielOf(t, userID), "tags"); len(got) != 1 || strings.Contains(got[0], "ancien") {
			t.Errorf("<tags> = %q, want go only", got)
		}
	})

	t.Run("A deleted tag is not listed", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		tags := map[string]int64{}
		e.carry(t, userID, "golang", 1, tags)
		storetest.DeleteTag(t, e.path, userID, tags["golang"])
		if got := refBlock(t, e.referentielOf(t, userID), "tags"); len(got) != 0 {
			t.Errorf("<tags> = %q, want an empty block", got)
		}
	})

	t.Run("Another user's tags are never listed", func(t *testing.T) {
		e := newEnv(t)
		first, second := e.user(t, "first"), e.user(t, "second")
		e.carry(t, second, "jardin-secret", 1, map[string]int64{})
		e.carry(t, first, "go", 1, map[string]int64{})
		if ref := e.referentielOf(t, first); strings.Contains(ref, "jardin-secret") {
			t.Errorf("the first user's référentiel names the second user's tag:\n%s", ref)
		}
		if got := refBlock(t, e.referentielOf(t, second), "tags"); len(got) != 1 || got[0] != "jardin-secret" {
			t.Errorf("the second user's <tags> = %q, want their own tag", got)
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
	got := analysis.Referentiel(types, categories, nil)
	want := "<referentiel>\n<types>\ntech_article | Article technique\npodcast | Podcast audio | proposed\n</types>\n" +
		"<categories>\n3 | Dev | active | Sur deux lignes\n4 | Dev › Java Spring | active |\n6 | Vide | proposed |\n</categories>\n" +
		"<tags>\n</tags>\n</referentiel>"

	t.Run("a proposed type ends with its status, and a line break never splits a line", func(t *testing.T) {
		if got != want {
			t.Errorf("Referentiel =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("the tags are one line joined by commas, in the order given", func(t *testing.T) {
		got := analysis.Referentiel(nil, nil, []string{"go", "ansible", "docker"})
		want := "<referentiel>\n<types>\n</types>\n<categories>\n</categories>\n<tags>\ngo, ansible, docker\n</tags>\n</referentiel>"
		if got != want {
			t.Errorf("Referentiel =\n%s\nwant\n%s", got, want)
		}
	})
}
