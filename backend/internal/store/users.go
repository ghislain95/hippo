package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ownerName is the name of the single V1 user.
const ownerName = "owner"

// EnsureSingleUser returns the id of the single V1 user, creating it at first start with its
// starting categories. Its token hash is replaced when API_TOKEN changes, so rotating the token
// only needs a restart. A user created before the categories existed gets the starting tree
// here, once (its categories_seeded_at is NULL until then).
func (s *Store) EnsureSingleUser(ctx context.Context, tokenHash string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("ensure user: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var (
		id, storedHash string
		seededAt       sql.Null[string]
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, api_token_hash, categories_seeded_at FROM users ORDER BY created_at LIMIT 1`).Scan(&id, &storedHash, &seededAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if id, err = s.createUser(ctx, tx, ownerName, tokenHash); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("read user: %w", err)
	default:
		if storedHash != tokenHash {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET api_token_hash = ? WHERE id = ?`, tokenHash, id); err != nil {
				return "", fmt.Errorf("rotate user token: %w", err)
			}
		}
		if !seededAt.Valid {
			if err := seedCategories(ctx, tx, id, s.now().UTC()); err != nil {
				return "", err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("ensure user: %w", err)
	}
	return id, nil
}

// CreateUser creates a user with its starting categories, in one transaction, and returns its
// id. Only EnsureSingleUser creates a user in production (one user in V1); tests use it for a
// second user (RG-90).
func (s *Store) CreateUser(ctx context.Context, name, tokenHash string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	id, err := s.createUser(ctx, tx, name, tokenHash)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// createUser inserts a user and seeds its starting categories inside tx.
func (s *Store) createUser(ctx context.Context, tx *sql.Tx, name, tokenHash string) (string, error) {
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, name, api_token_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, name, tokenHash, now.Format(TimeLayout)); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	if err := seedCategories(ctx, tx, id, now); err != nil {
		return "", err
	}
	return id, nil
}
