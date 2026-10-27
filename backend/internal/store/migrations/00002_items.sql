-- +goose Up
-- Items and the database-wide modification sequence (docs/spec-v1.md, "Modèle de données";
-- ADR-0035). Story E2-01; design of the capture-link change.

-- The modification sequence: one row, advanced only by internal/store/sequence.go inside the
-- transaction of each write of a synced row.
CREATE TABLE sync_sequence (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    value INTEGER NOT NULL
) STRICT;

INSERT INTO sync_sequence (id, value) VALUES (1, 0);

-- type_id, category_id and batch_id reference tables that do not exist yet: the stories that
-- create content_types, categories and batches add them with ALTER TABLE ... ADD COLUMN.
-- No CHECK on source: V2 adds rss, and changing a CHECK means rebuilding the table.
CREATE TABLE items (
    id                 TEXT PRIMARY KEY,                          -- ULID
    user_id            TEXT NOT NULL REFERENCES users (id),
    kind               TEXT NOT NULL CHECK (kind IN ('link', 'image', 'note')),
    source             TEXT NOT NULL,
    url                TEXT,                                      -- as captured
    normalized_url     TEXT,                                      -- RG-05, filled from E2-02
    site_name          TEXT,
    user_note          TEXT,                                      -- capture note (RG-03)
    client_id          TEXT,                                      -- chosen by the device (RG-08)
    is_private         INTEGER NOT NULL DEFAULT 0 CHECK (is_private IN (0, 1)),
    status             TEXT NOT NULL CHECK (status IN ('queued', 'extracted', 'submitted', 'analyzed', 'failed')),
    extraction_mode    TEXT CHECK (extraction_mode IN ('full', 'fallback', 'none')),
    content_text       TEXT,
    content_truncated  INTEGER NOT NULL DEFAULT 0 CHECK (content_truncated IN (0, 1)),
    reading_minutes    INTEGER,
    title              TEXT NOT NULL,
    fiche_json         TEXT,
    search_text        TEXT NOT NULL DEFAULT '',
    content_ok         INTEGER CHECK (content_ok IN (0, 1)),
    content_issue      TEXT,
    incomplete_retried INTEGER NOT NULL DEFAULT 0 CHECK (incomplete_retried IN (0, 1)),
    language           TEXT,
    freshness          TEXT CHECK (freshness IN ('dated', 'timeless')),
    model              TEXT,
    prompt_version     INTEGER,
    schema_version     INTEGER,
    user_edited        TEXT NOT NULL DEFAULT '[]',                -- JSON array (RG-60)
    annotations        TEXT,
    is_pinned          INTEGER NOT NULL DEFAULT 0 CHECK (is_pinned IN (0, 1)),
    read_status        TEXT NOT NULL DEFAULT 'to_read' CHECK (read_status IN ('to_read', 'in_progress', 'read')),
    read_progress      REAL NOT NULL DEFAULT 0 CHECK (read_progress BETWEEN 0 AND 1),
    attempts           INTEGER NOT NULL DEFAULT 0,
    last_error         TEXT,
    created_at         TEXT NOT NULL,                             -- ISO 8601 UTC
    updated_at         TEXT NOT NULL,
    extracted_at       TEXT,
    analyzed_at        TEXT,
    read_at            TEXT,
    deleted_at         TEXT,
    change_seq         INTEGER NOT NULL                           -- no default: a forgotten sequence fails
) STRICT;

-- A replayed capture returns the existing item (RG-08); the same client_id of another user is
-- independent (RG-90).
CREATE UNIQUE INDEX items_user_client_id ON items (user_id, client_id) WHERE client_id IS NOT NULL;

-- Changes of a user in sequence order, for GET /api/sync (ADR-0035).
CREATE INDEX items_user_change_seq ON items (user_id, change_seq);
