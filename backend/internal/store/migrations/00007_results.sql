-- +goose Up
-- E3-02 to E3-05 (change apply-batch-results): the results of a batch, the proposals and the
-- tags (docs/spec-v1.md, "Modèle de données"; design D9).
ALTER TABLE items ADD COLUMN envelope_version INTEGER;          -- RG-64, from the batch row

-- categories.name_key is the normalised name (RG-54), written by Go only: lower() of SQLite
-- folds ASCII only and removes no accent. store.Open fills it for the existing rows. The
-- COALESCE makes two roots of the same name collide (NULLs are distinct in a unique index);
-- a deleted category never forbids a new one of the same name.
ALTER TABLE categories ADD COLUMN name_key TEXT;                -- RG-54, written by Go only
CREATE UNIQUE INDEX categories_user_parent_name
    ON categories (user_id, COALESCE(parent_id, 0), name_key)
    WHERE deleted_at IS NULL AND name_key IS NOT NULL;

-- A batch whose results Anthropic no longer serves is ended with results_unavailable_at
-- (decision 1) rather than a new status: changing the CHECK of status means rebuilding the
-- table, which items.batch_id references.
ALTER TABLE batches ADD COLUMN results_unavailable_at TEXT;     -- decision 1
ALTER TABLE batches ADD COLUMN results_lines_read INTEGER;      -- most complete cut read

-- Tags of each user (RG-58, RG-90), synced (ADR-0035). name holds the normalised form only.
CREATE TABLE tags (
    id         INTEGER PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    change_seq INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX tags_id_user ON tags (id, user_id);
CREATE UNIQUE INDEX tags_user_name ON tags (user_id, name) WHERE deleted_at IS NULL;
CREATE INDEX tags_user_change_seq ON tags (user_id, change_seq);

-- The tags of an item. Both composite foreign keys tie the item and the tag to the same user
-- (RG-90). Not synced on its own: the tags travel inside their item (tag_ids).
CREATE TABLE item_tags (
    item_id TEXT    NOT NULL,
    tag_id  INTEGER NOT NULL,
    user_id TEXT    NOT NULL,
    PRIMARY KEY (item_id, tag_id),
    FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id),
    FOREIGN KEY (tag_id, user_id) REFERENCES tags (id, user_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX item_tags_tag ON item_tags (tag_id);
