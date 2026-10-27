-- +goose Up
-- Images of items, stored on disk under DATA_DIR/assets and referenced here (ADR-0004;
-- docs/spec-v1.md, "Modèle de données"). Story E2-04; design of the capture-image change.

-- The composite foreign key of assets needs a unique key on (id, user_id). id alone is
-- already unique: this index adds no constraint, it lets an asset reference its item and its
-- user together.
CREATE UNIQUE INDEX items_id_user ON items (id, user_id);

-- One row per image file. path is relative to DATA_DIR/assets and built only from server
-- identifiers (<user_id>/<asset_id>.<ext>). The composite foreign key ties an asset to an item
-- of the same user (RG-90). Not a synced table: assets travel inside their item, and every
-- write of an item's assets advances the item's change_seq in the same transaction. Not tied
-- to kind = image: a link may get a preview image later (RG-23).
CREATE TABLE assets (
    id         TEXT PRIMARY KEY,                               -- ULID
    item_id    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id),
    path       TEXT NOT NULL,
    mime_type  TEXT NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/heic', 'image/webp')),
    width      INTEGER,                                        -- pixels, JPEG and PNG only
    height     INTEGER,
    size_bytes INTEGER NOT NULL,
    created_at TEXT NOT NULL,                                  -- ISO 8601 UTC
    FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id)
) STRICT;

CREATE INDEX assets_item ON assets (item_id);
