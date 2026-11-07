package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ghislain95/hippo/backend/internal/naming"
)

// startingRoot is a root category of the starting tree, with the description given to the AI
// (RG-52) and its sub-categories, which have none.
type startingRoot struct {
	name          string
	description   string
	subCategories []string
}

// startingTree mirrors the table of docs/metier.md section 6, in its order. Every user gets it
// once (seedCategories). Two levels only (RG-51): this static tree is the only writer of
// categories in E3-01.
var startingTree = []startingRoot{
	{
		"Dev", "Développement logiciel : langages, frameworks, bases de données, conception, tests.",
		[]string{"Java & Spring", "Bases de données", "Front & mobile", "Architecture & pratiques"},
	},
	{
		"IA", "Intelligence artificielle, en particulier appliquée au développement.",
		[]string{"Outils & agents", "Modèles", "Usages au quotidien"},
	},
	{
		"Infra & DevOps", "Infrastructure, déploiement, exploitation, réseau, NAS.",
		[]string{"Conteneurs & orchestration", "CI/CD", "Cloud", "Auto-hébergement"},
	},
	{"Actu tech", "Actualités et annonces du monde de la tech qui ne relèvent pas d'un domaine précis.", nil},
	{
		"Maison & quotidien", "Entretien de la maison, astuces de la vie de tous les jours.",
		[]string{"Ménage", "Bricolage", "Astuces pratiques"},
	},
	{"Cuisine", "Recettes, techniques et astuces de cuisine.", nil},
	{"Plantes", "Entretien des plantes d'intérieur et d'extérieur.", nil},
	{"Jeux vidéo", "Jeux, matériel, émulation.", nil},
	{"Voyages & sorties", "Lieux à visiter, restaurants, sorties, voyages.", []string{"Lille"}},
}

// seedCategories inserts the starting tree for userID inside tx, depth-first so that ids read
// naturally (each root, then its sub-categories), each row with its own change_seq, and sets
// the user's categories_seeded_at, so that the tree is seeded once: a user who later deletes
// every category does not get it back.
func seedCategories(ctx context.Context, tx *sql.Tx, userID string, now time.Time) error {
	date := now.Format(TimeLayout)
	for _, root := range startingTree {
		rootID, err := insertStartingCategory(ctx, tx, userID, root.name, &root.description, nil, date)
		if err != nil {
			return err
		}
		for _, name := range root.subCategories {
			if _, err := insertStartingCategory(ctx, tx, userID, name, nil, &rootID, date); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET categories_seeded_at = ? WHERE id = ?`, date, userID); err != nil {
		return fmt.Errorf("seed categories: %w", err)
	}
	return nil
}

func insertStartingCategory(ctx context.Context, tx *sql.Tx, userID, name string, description *string, parentID *int64, date string) (int64, error) {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO categories (user_id, name, name_key, description, parent_id, status, created_at, updated_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?) RETURNING id`,
		userID, name, naming.CategoryKey(name), description, parentID, date, date, seq).Scan(&id); err != nil {
		return 0, fmt.Errorf("seed category: %w", err)
	}
	return id, nil
}

// Category is a category of a user (RG-50, RG-51): a root when ParentID is nil.
type Category struct {
	ID          int64
	Name        string
	Description *string // given to the AI (RG-52); nil for the starting sub-categories
	ParentID    *int64
	Status      string // active or proposed
	ChangeSeq   int64
}

// Categories returns the categories of userID that are not deleted, by id (creation order),
// active and proposed alike (RG-52). The référentiel orders them as a tree.
func (s *Store) Categories(ctx context.Context, userID string) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, parent_id, status, change_seq FROM categories
		 WHERE user_id = ? AND deleted_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	categories := []Category{}
	for rows.Next() {
		var (
			c           Category
			description sql.Null[string]
			parentID    sql.Null[int64]
		)
		if err := rows.Scan(&c.ID, &c.Name, &description, &parentID, &c.Status, &c.ChangeSeq); err != nil {
			return nil, fmt.Errorf("list categories: %w", err)
		}
		c.Description, c.ParentID = ptr(description), ptr(parentID)
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}

// fillCategoryKeys writes the normalised name of every category that has none (design D9):
// migration 00007 adds the column, and a migration is SQL only, whose lower() folds ASCII
// only. One transaction, user by user, each statement filtered by user_id (RG-90), deleted
// categories included, without change_seq: name_key is not synced. It returns the number of
// rows filled. Two non-deleted categories of a user with the same key under the same parent
// stop the start-up with an error naming both ids, never their names.
func fillCategoryKeys(ctx context.Context, db *sql.DB) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("fill category keys: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	users, err := queryIDs(ctx, tx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("fill category keys: list users: %w", err)
	}
	filled := 0
	for _, userID := range users {
		n, err := fillUserCategoryKeys(ctx, tx, userID)
		if err != nil {
			return 0, err
		}
		filled += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("fill category keys: %w", err)
	}
	return filled, nil
}

// fillUserCategoryKeys fills the normalised names of the categories of userID inside tx.
func fillUserCategoryKeys(ctx context.Context, tx *sql.Tx, userID string) (int, error) {
	missing, err := categoriesWithoutKey(ctx, tx, userID)
	if err != nil {
		return 0, fmt.Errorf("fill category keys: %w", err)
	}
	for _, r := range missing {
		key := naming.CategoryKey(r.name)
		_, err := tx.ExecContext(ctx, `UPDATE categories SET name_key = ? WHERE user_id = ? AND id = ?`, key, r.userID, r.id)
		var violation *uniqueViolation
		if errors.As(asUniqueViolation("categories", err), &violation) {
			var other int64
			if lookupErr := tx.QueryRowContext(ctx,
				`SELECT id FROM categories
				 WHERE user_id = ? AND COALESCE(parent_id, 0) = ? AND name_key = ? AND deleted_at IS NULL AND id <> ?`,
				r.userID, r.parentID, key, r.id).Scan(&other); lookupErr != nil {
				return 0, fmt.Errorf("fill category keys: category %d collides: %w", r.id, lookupErr)
			}
			return 0, fmt.Errorf("fill category keys: categories %d and %d have the same normalised name under the same parent", other, r.id)
		}
		if err != nil {
			return 0, fmt.Errorf("fill category keys: category %d: %w", r.id, err)
		}
	}
	return len(missing), nil
}

// categoryWithoutKey is a category whose normalised name is not filled yet.
type categoryWithoutKey struct {
	id       int64
	userID   string
	parentID int64 // 0 for a root, as in the index categories_user_parent_name
	name     string
}

// categoriesWithoutKey reads the categories of userID without name_key inside tx.
func categoriesWithoutKey(ctx context.Context, tx *sql.Tx, userID string) ([]categoryWithoutKey, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, user_id, COALESCE(parent_id, 0), name FROM categories WHERE user_id = ? AND name_key IS NULL ORDER BY id`,
		userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	var missing []categoryWithoutKey
	for rows.Next() {
		var c categoryWithoutKey
		if err := rows.Scan(&c.id, &c.userID, &c.parentID, &c.name); err != nil {
			return nil, err
		}
		missing = append(missing, c)
	}
	return missing, rows.Err()
}

// resolveProposedCategory files a proposed category of userID inside tx and returns its id
// (design D6 step 3). Its parent is corrected first (RG-55): none, unknown, deleted or another
// user's → a root; a sub-category → its root when that root is not deleted, else a root. Then
// a non-deleted category of the user under the same parent with the same normalised name is
// reused unchanged (RG-54, RG-56); otherwise one is inserted proposed, without a colour of its
// own (RG-59), with its own change_seq. The depth (RG-51) holds: a parent is always a root.
func resolveProposedCategory(ctx context.Context, tx *sql.Tx, userID string, p ProposedCategory, date string) (int64, error) {
	parentID, err := correctedParent(ctx, tx, userID, p.ParentID)
	if err != nil {
		return 0, err
	}
	key := naming.CategoryKey(p.Name)
	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM categories
		 WHERE user_id = ? AND COALESCE(parent_id, 0) = ? AND name_key = ? AND deleted_at IS NULL`,
		userID, parentID.V, key).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("match proposed category: %w", err)
	}
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO categories (user_id, name, name_key, description, parent_id, status, proposal_reason, created_at,
		   updated_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, 'proposed', ?, ?, ?, ?) RETURNING id`,
		userID, p.Name, key, p.Description, parentID, p.Reason, date, date, seq).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert proposed category: %w", err)
	}
	return id, nil
}

// correctedParent returns the parent a proposed category of userID goes under (RG-55): NULL
// for a root. Only a non-deleted category of the user counts.
func correctedParent(ctx context.Context, tx *sql.Tx, userID string, proposed *int64) (sql.Null[int64], error) {
	root := sql.Null[int64]{}
	if proposed == nil {
		return root, nil
	}
	var grandParent sql.Null[int64]
	err := tx.QueryRowContext(ctx,
		`SELECT parent_id FROM categories WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, *proposed, userID).Scan(&grandParent)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return root, nil
	case err != nil:
		return root, fmt.Errorf("read proposed parent: %w", err)
	case !grandParent.Valid:
		return sql.Null[int64]{V: *proposed, Valid: true}, nil
	}
	var alive bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM categories WHERE id = ? AND user_id = ? AND deleted_at IS NULL)`,
		grandParent.V, userID).Scan(&alive); err != nil {
		return root, fmt.Errorf("read proposed parent: %w", err)
	}
	if !alive {
		return root, nil
	}
	return grandParent, nil
}
