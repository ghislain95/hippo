package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ownerName is the name of the single V1 user.
const ownerName = "owner"

// EnsureSingleUser returns the id of the single V1 user, creating it at first start. Its token
// hash is replaced when API_TOKEN changes, so rotating the token only needs a restart.
func (s *Store) EnsureSingleUser(ctx context.Context, tokenHash string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("ensure user: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var id, storedHash string
	err = tx.QueryRowContext(ctx, `SELECT id, api_token_hash FROM users ORDER BY created_at LIMIT 1`).Scan(&id, &storedHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if id, err = s.createUser(ctx, tx, ownerName, tokenHash); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("read user: %w", err)
	case storedHash != tokenHash:
		if _, err := tx.ExecContext(ctx, `UPDATE users SET api_token_hash = ? WHERE id = ?`, tokenHash, id); err != nil {
			return "", fmt.Errorf("rotate user token: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("ensure user: %w", err)
	}
	return id, nil
}

// CreateUser creates a user and returns its id. Only EnsureSingleUser creates a user in
// production (one user in V1); tests use it for a second user (RG-90).
func (s *Store) CreateUser(ctx context.Context, name, tokenHash string) (string, error) {
	return s.createUser(ctx, s.db, name, tokenHash)
}

// execer is what a write needs from a *sql.DB or a *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) createUser(ctx context.Context, db execer, name, tokenHash string) (string, error) {
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, name, api_token_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, name, tokenHash, now.Format(timeLayout)); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}
