package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// NewAsset is the image of a captured item. Its file is already on disk at Path, relative to
// the assets directory, when the capture transaction runs: a row never points to a missing
// file (ADR-0004).
type NewAsset struct {
	ID        string // from NewID, chosen before the file is renamed
	Path      string // <user_id>/<asset_id>.<ext>, built from server identifiers only
	MimeType  string
	Width     *int64 // nil when the format is not decoded (HEIC, WebP)
	Height    *int64
	SizeBytes int64
}

// Asset is a stored image of an item.
type Asset struct {
	ID        string
	ItemID    string
	UserID    string
	Path      string
	MimeType  string
	Width     *int64
	Height    *int64
	SizeBytes int64
	CreatedAt time.Time
}

// NewID returns a new ULID from the store's clock, for a row whose id must be known before its
// transaction: an asset names its file before the file is renamed into place.
func (s *Store) NewID() (string, error) {
	return s.newID(s.now().UTC())
}

// Asset returns the asset id of userID, or ErrNotFound, for an unknown asset as for another
// user's (RG-90).
func (s *Store) Asset(ctx context.Context, userID, id string) (Asset, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM assets WHERE user_id = ? AND id = ?`, userID, id)
	var asset Asset
	if err := scanAsset(row.Scan, &asset); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Asset{}, ErrNotFound
		}
		return Asset{}, err
	}
	return asset, nil
}

// insertAsset stores the asset of the new item itemID inside the capture transaction. It does
// not advance the sequence itself: the item it belongs to was inserted with a new change_seq in
// the same transaction, and assets travel inside their item (ADR-0035).
func insertAsset(ctx context.Context, tx *sql.Tx, userID, itemID string, in NewAsset, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO assets (id, item_id, user_id, path, mime_type, width, height, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ID, itemID, userID, in.Path, in.MimeType, in.Width, in.Height, in.SizeBytes, now.Format(timeLayout)); err != nil {
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

const assetColumns = `id, item_id, user_id, path, mime_type, width, height, size_bytes, created_at`

// assetsOfItem returns the assets of the item itemID of userID, oldest first; none for another
// user's item. Every item read back carries them (withAssets).
func assetsOfItem(ctx context.Context, db querier, userID, itemID string) ([]Asset, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+assetColumns+` FROM assets WHERE user_id = ? AND item_id = ? ORDER BY id`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	assets := []Asset{}
	for rows.Next() {
		var asset Asset
		if err := scanAsset(rows.Scan, &asset); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	return assets, nil
}

// scanAsset reads a row of assetColumns through scan (a *sql.Row or *sql.Rows Scan). It returns
// sql.ErrNoRows unwrapped, for Asset to tell a missing row.
func scanAsset(scan func(dest ...any) error, asset *Asset) error {
	var (
		width, height sql.Null[int64]
		createdAt     string
	)
	err := scan(&asset.ID, &asset.ItemID, &asset.UserID, &asset.Path, &asset.MimeType, &width, &height,
		&asset.SizeBytes, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		return fmt.Errorf("read asset: %w", err)
	}
	asset.Width, asset.Height = ptr(width), ptr(height)
	if asset.CreatedAt, err = parseTime(createdAt); err != nil {
		return fmt.Errorf("read asset %s: created_at: %w", asset.ID, err)
	}
	return nil
}
