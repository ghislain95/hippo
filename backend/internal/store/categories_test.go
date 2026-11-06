package store_test

import (
	"context"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// startingCategory is a category of the starting tree of docs/metier.md section 6: a root with
// its description, or a sub-category (parent set) without one.
type startingCategory struct {
	name, parent, description string
}

// startingTree is docs/metier.md section 6, depth-first: each root followed by its
// sub-categories, in the order of the table.
var startingTree = []startingCategory{
	{name: "Dev", description: "Développement logiciel : langages, frameworks, bases de données, conception, tests."},
	{name: "Java & Spring", parent: "Dev"},
	{name: "Bases de données", parent: "Dev"},
	{name: "Front & mobile", parent: "Dev"},
	{name: "Architecture & pratiques", parent: "Dev"},
	{name: "IA", description: "Intelligence artificielle, en particulier appliquée au développement."},
	{name: "Outils & agents", parent: "IA"},
	{name: "Modèles", parent: "IA"},
	{name: "Usages au quotidien", parent: "IA"},
	{name: "Infra & DevOps", description: "Infrastructure, déploiement, exploitation, réseau, NAS."},
	{name: "Conteneurs & orchestration", parent: "Infra & DevOps"},
	{name: "CI/CD", parent: "Infra & DevOps"},
	{name: "Cloud", parent: "Infra & DevOps"},
	{name: "Auto-hébergement", parent: "Infra & DevOps"},
	{name: "Actu tech", description: "Actualités et annonces du monde de la tech qui ne relèvent pas d'un domaine précis."},
	{name: "Maison & quotidien", description: "Entretien de la maison, astuces de la vie de tous les jours."},
	{name: "Ménage", parent: "Maison & quotidien"},
	{name: "Bricolage", parent: "Maison & quotidien"},
	{name: "Astuces pratiques", parent: "Maison & quotidien"},
	{name: "Cuisine", description: "Recettes, techniques et astuces de cuisine."},
	{name: "Plantes", description: "Entretien des plantes d'intérieur et d'extérieur."},
	{name: "Jeux vidéo", description: "Jeux, matériel, émulation."},
	{name: "Voyages & sorties", description: "Lieux à visiter, restaurants, sorties, voyages."},
	{name: "Lille", parent: "Voyages & sorties"},
}

// assertStartingTree checks that the categories of userID are exactly the starting tree, once,
// with ids in depth-first order.
func assertStartingTree(t *testing.T, st *store.Store, userID string) []store.CategoryRowForTest {
	t.Helper()
	rows := store.CategoryRowsForTest(t, st, userID)
	if len(rows) != len(startingTree) {
		t.Fatalf("categories = %d, want %d", len(rows), len(startingTree))
	}
	idByName := map[string]int64{}
	for i, row := range rows {
		want := startingTree[i]
		idByName[row.Name] = row.ID
		if row.Name != want.name || row.UserID != userID || row.Status != "active" || row.Deleted ||
			row.Color != nil || row.ProposalReason != nil {
			t.Errorf("category %d = %+v, want %q of the user, active, without colour nor reason", i, row, want.name)
		}
		switch want.parent {
		case "":
			if row.ParentID != nil || row.Description == nil || *row.Description != want.description {
				t.Errorf("root %q: parent %v, description %v; want no parent and %q", row.Name, row.ParentID, row.Description, want.description)
			}
		default:
			if row.ParentID == nil || *row.ParentID != idByName[want.parent] || row.Description != nil {
				t.Errorf("sub-category %q: parent %v, description %v; want parent %q (%d) and no description",
					row.Name, row.ParentID, row.Description, want.parent, idByName[want.parent])
			}
		}
	}
	return rows
}

func TestCategorySeeding(t *testing.T) {
	ctx := context.Background()

	t.Run("A user created before the categories gets them once", func(t *testing.T) {
		st, userID, _, _ := openVersion5(t)

		for range 2 {
			id, err := st.EnsureSingleUser(ctx, "hash")
			if err != nil {
				t.Fatalf("EnsureSingleUser: %v", err)
			}
			if id != userID {
				t.Fatalf("EnsureSingleUser = %q, want the existing user %q", id, userID)
			}
		}
		assertStartingTree(t, st, userID)
		if store.CategoriesSeededAtForTest(t, st, userID) == nil {
			t.Error("categories_seeded_at is NULL after the catch-up")
		}
	})

	t.Run("The starting tree has two levels", func(t *testing.T) {
		st := storetest.Open(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		rows := assertStartingTree(t, st, userID)
		roots := map[int64]bool{}
		for _, row := range rows {
			if row.ParentID == nil {
				roots[row.ID] = true
			}
		}
		for _, row := range rows {
			if row.ParentID != nil && !roots[*row.ParentID] {
				t.Errorf("sub-category %q has parent %d, which is not a root", row.Name, *row.ParentID)
			}
		}
	})

	t.Run("CreateUser seeds the starting tree, one change_seq per category, and sets the marker", func(t *testing.T) {
		st, clock := openWithClock(t)
		before := store.SequenceForTest(t, st)

		userID := newUser(t, st, "first")

		rows := assertStartingTree(t, st, userID)
		if got := store.SequenceForTest(t, st); got != before+int64(len(startingTree)) {
			t.Errorf("sequence = %d, want %d (one value per category)", got, before+int64(len(startingTree)))
		}
		date := clock.Now().UTC().Format(store.TimeLayout)
		seen := map[int64]bool{}
		for _, row := range rows {
			if row.ChangeSeq <= before || seen[row.ChangeSeq] {
				t.Errorf("category %q change_seq = %d, want a distinct value above %d", row.Name, row.ChangeSeq, before)
			}
			seen[row.ChangeSeq] = true
			if row.CreatedAt != date || row.UpdatedAt != date {
				t.Errorf("category %q dates = %s, %s; want %s from the store clock", row.Name, row.CreatedAt, row.UpdatedAt, date)
			}
		}
		if marker := store.CategoriesSeededAtForTest(t, st, userID); marker == nil || *marker != date {
			t.Errorf("categories_seeded_at = %v, want %s", marker, date)
		}
	})

	t.Run("each user gets their own starting tree", func(t *testing.T) {
		st := storetest.Open(t)
		first := newUser(t, st, "first")
		second := newUser(t, st, "second")

		firstRows := assertStartingTree(t, st, first)
		secondRows := assertStartingTree(t, st, second)
		firstIDs := map[int64]bool{}
		for _, row := range firstRows {
			firstIDs[row.ID] = true
		}
		for _, row := range secondRows {
			if firstIDs[row.ID] || (row.ParentID != nil && firstIDs[*row.ParentID]) {
				t.Errorf("second user's category %q (id %d, parent %v) shares an id with the first user's", row.Name, row.ID, row.ParentID)
			}
		}
	})

	t.Run("a token rotation and the catch-up happen in the same start", func(t *testing.T) {
		st, userID, _, _ := openVersion5(t)

		id, err := st.EnsureSingleUser(ctx, "new-hash")
		if err != nil || id != userID {
			t.Fatalf("EnsureSingleUser = %q, %v; want %q", id, err, userID)
		}
		if got := store.TokenHashForTest(t, st, userID); got != "new-hash" {
			t.Errorf("token hash = %q, want new-hash", got)
		}
		assertStartingTree(t, st, userID)
	})

	t.Run("a seeded user is not seeded again at the next start", func(t *testing.T) {
		st := storetest.Open(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		sequence := store.SequenceForTest(t, st)
		if _, err := st.EnsureSingleUser(ctx, "hash"); err != nil {
			t.Fatalf("EnsureSingleUser again: %v", err)
		}
		assertStartingTree(t, st, userID)
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence = %d after a second start, want %d unchanged", got, sequence)
		}
	})
}

func TestContentTypes(t *testing.T) {
	ctx := context.Background()

	t.Run("lists the seven starting types in the order of metier.md, for every user alike", func(t *testing.T) {
		st := storetest.Open(t)
		newUser(t, st, "first")
		newUser(t, st, "second")

		types, err := st.ContentTypes(ctx)
		if err != nil {
			t.Fatalf("ContentTypes: %v", err)
		}
		want := []string{"tech_article", "tutorial", "tip", "recipe", "place", "resource", "note"}
		if len(types) != len(want) {
			t.Fatalf("ContentTypes = %d types, want %d", len(types), len(want))
		}
		for i, ct := range types {
			if ct.ID != int64(i+1) || ct.Key != want[i] || ct.Status != "active" || !ct.HasTemplate || ct.ChangeSeq == 0 {
				t.Errorf("type %d = %+v, want id %d %s active with a template and a change_seq", i, ct, i+1, want[i])
			}
		}
		if types[0].Name != "Article technique" || types[6].Name != "Note perso" {
			t.Errorf("names = %q … %q, want Article technique … Note perso", types[0].Name, types[6].Name)
		}
	})
}

func TestCategories(t *testing.T) {
	ctx := context.Background()

	t.Run("lists the starting tree of a new user by id", func(t *testing.T) {
		st := storetest.Open(t)
		userID := newUser(t, st, "first")

		categories, err := st.Categories(ctx, userID)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		rows := store.CategoryRowsForTest(t, st, userID)
		if len(categories) != len(rows) {
			t.Fatalf("Categories = %d, want %d", len(categories), len(rows))
		}
		for i, c := range categories {
			row := rows[i]
			if c.ID != row.ID || c.Name != row.Name || c.Status != row.Status || c.ChangeSeq != row.ChangeSeq ||
				!equalPtr(c.Description, row.Description) || !equalPtr(c.ParentID, row.ParentID) {
				t.Errorf("category %d = %+v, want %+v", i, c, row)
			}
		}
	})

	t.Run("lists only the user's categories", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		first := newUser(t, st, "first")
		second := newUser(t, st, "second")
		secret := storetest.AddCategory(t, path, second, storetest.NewCategory{Name: "Jardin secret", Status: "active"})

		categories, err := st.Categories(ctx, first)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		own := map[int64]bool{}
		for _, row := range store.CategoryRowsForTest(t, st, first) {
			own[row.ID] = true
		}
		if len(categories) != len(startingTree) {
			t.Errorf("first user's categories = %d, want %d", len(categories), len(startingTree))
		}
		for _, c := range categories {
			if !own[c.ID] || c.ID == secret || c.Name == "Jardin secret" {
				t.Errorf("first user's list holds %+v, which is not theirs", c)
			}
		}
		others, err := st.Categories(ctx, second)
		if err != nil || len(others) != len(startingTree)+1 || others[len(others)-1].ID != secret {
			t.Errorf("second user's categories = %d (%v), want %d ending with Jardin secret", len(others), err, len(startingTree)+1)
		}
	})

	t.Run("leaves deleted categories out and lists proposed ones with their status", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID := newUser(t, st, "first")
		idByName := map[string]int64{}
		for _, row := range store.CategoryRowsForTest(t, st, userID) {
			idByName[row.Name] = row.ID
		}
		storetest.DeleteCategory(t, path, userID, idByName["Plantes"])
		kubernetes := storetest.AddCategory(t, path, userID, storetest.NewCategory{
			Name: "Kubernetes", Description: ptr("Orchestration de conteneurs."), ParentID: ptr(idByName["Infra & DevOps"]),
			Status: "proposed", ProposalReason: ptr("Plusieurs éléments sur Kubernetes."),
		})

		categories, err := st.Categories(ctx, userID)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		if len(categories) != len(startingTree) {
			t.Fatalf("Categories = %d, want %d (one deleted, one proposed)", len(categories), len(startingTree))
		}
		for _, c := range categories {
			if c.Name == "Plantes" {
				t.Error("the deleted category Plantes is listed")
			}
		}
		last := categories[len(categories)-1]
		if last.ID != kubernetes || last.Status != "proposed" || last.ParentID == nil || *last.ParentID != idByName["Infra & DevOps"] {
			t.Errorf("last category = %+v, want Kubernetes, proposed, under Infra & DevOps", last)
		}
	})

	t.Run("a user whose categories were all deleted does not get them back at the next start", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		rows := store.CategoryRowsForTest(t, st, userID)
		for i := len(rows) - 1; i >= 0; i-- {
			storetest.DeleteCategory(t, path, userID, rows[i].ID)
		}

		if _, err := st.EnsureSingleUser(ctx, "hash"); err != nil {
			t.Fatalf("EnsureSingleUser again: %v", err)
		}
		categories, err := st.Categories(ctx, userID)
		if err != nil || len(categories) != 0 {
			t.Errorf("Categories after a restart = %+v (%v), want none", categories, err)
		}
	})
}
