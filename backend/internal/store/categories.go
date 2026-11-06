package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
		`INSERT INTO categories (user_id, name, description, parent_id, status, created_at, updated_at, change_seq)
		 VALUES (?, ?, ?, ?, 'active', ?, ?, ?) RETURNING id`,
		userID, name, description, parentID, date, date, seq).Scan(&id); err != nil {
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
