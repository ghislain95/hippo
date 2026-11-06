-- +goose Up
-- Content types, categories and batches of the nightly analysis (docs/spec-v1.md, "Modèle de
-- données"). Story E3-01, design of the submit-nightly-batch change (D13).
--
-- Exception to "only nextChangeSeq writes sync_sequence" (internal/store/sequence.go): a
-- migration is SQL only (ADR-0032), so the seven seeded content types advance the sequence here,
-- one value per row, before each insert. Every later write of a synced row goes through
-- nextChangeSeq.

-- Content types are shared by every user (RG-57, ADR-0022) and synced (ADR-0035). ids 1 to 7
-- are the starting types of docs/metier.md section 7, in its order, which is the order the AI
-- reads them in.
CREATE TABLE content_types (
    id              INTEGER PRIMARY KEY,
    key             TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('active', 'proposed')),
    has_template    INTEGER NOT NULL CHECK (has_template IN (0, 1)),
    proposal_reason TEXT,
    updated_at      TEXT NOT NULL,                             -- ISO 8601 UTC
    deleted_at      TEXT,
    change_seq      INTEGER NOT NULL                           -- no default: a forgotten sequence fails
) STRICT;

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (1, 'tech_article', 'Article technique', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (2, 'tutorial', 'Tuto', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (3, 'tip', 'Astuce', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (4, 'recipe', 'Recette', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (5, 'place', 'Lieu', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (6, 'resource', 'Ressource', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

UPDATE sync_sequence SET value = value + 1 WHERE id = 1;
INSERT INTO content_types (id, key, name, status, has_template, updated_at, change_seq)
VALUES (7, 'note', 'Note perso', 'active', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
        (SELECT value FROM sync_sequence WHERE id = 1));

-- Categories of each user (RG-50 to RG-59, RG-90), synced (ADR-0035). The starting tree of
-- docs/metier.md section 6 is seeded per user by the store, in Go, when the user is created
-- (users.categories_seeded_at below). Two levels (RG-51): the composite foreign key keeps a
-- parent within the same user, the store keeps the depth. No index on the normalised name yet
-- (E3-03).
CREATE TABLE categories (
    id              INTEGER PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id),
    name            TEXT NOT NULL,
    description     TEXT,                                      -- given to the AI (RG-52)
    parent_id       INTEGER,                                   -- NULL for a root category
    status          TEXT NOT NULL CHECK (status IN ('active', 'proposed')),
    proposal_reason TEXT,
    color           TEXT,
    created_at      TEXT NOT NULL,                             -- ISO 8601 UTC
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT,
    change_seq      INTEGER NOT NULL,                          -- no default: a forgotten sequence fails
    CHECK (parent_id IS NULL OR parent_id <> id),
    FOREIGN KEY (parent_id, user_id) REFERENCES categories (id, user_id)
) STRICT;

-- id alone is unique: this index lets a sub-category reference its parent and its user together.
CREATE UNIQUE INDEX categories_id_user ON categories (id, user_id);

-- Changes of a user in sequence order, for GET /api/sync (ADR-0035).
CREATE INDEX categories_user_change_seq ON categories (user_id, change_seq);

-- Message Batches batches of each user (RG-30 to RG-32, RG-81). pending: items submitted, send
-- not confirmed; in_progress: accepted by Anthropic; ended: results available (E3-02); failed:
-- refused, or never arrived. Not synced to the app.
CREATE TABLE batches (
    id                          TEXT PRIMARY KEY,               -- ULID (ADR-0035)
    user_id                     TEXT NOT NULL REFERENCES users (id),
    anthropic_batch_id          TEXT UNIQUE,                    -- NULL while pending
    status                      TEXT NOT NULL CHECK (status IN ('pending', 'in_progress', 'ended', 'failed')),
    model                       TEXT NOT NULL,
    prompt_version              INTEGER NOT NULL,
    envelope_version            INTEGER NOT NULL,
    item_count                  INTEGER NOT NULL CHECK (item_count BETWEEN 1 AND 200),
    input_tokens                INTEGER,
    output_tokens               INTEGER,
    cache_creation_input_tokens INTEGER,
    cache_read_input_tokens     INTEGER,
    cost_usd                    REAL,
    created_at                  TEXT NOT NULL,                  -- ISO 8601 UTC
    sent_at                     TEXT,                           -- accepted by Anthropic
    ended_at                    TEXT
) STRICT;

-- Spend of a user's month (RG-81).
CREATE INDEX batches_user_created ON batches (user_id, created_at);

-- The columns 00002_items.sql left for these tables. A column added with REFERENCES is allowed
-- with foreign keys on because its default is NULL: existing items keep NULL.
ALTER TABLE items ADD COLUMN type_id INTEGER REFERENCES content_types (id);
ALTER TABLE items ADD COLUMN category_id INTEGER REFERENCES categories (id);
ALTER TABLE items ADD COLUMN batch_id TEXT REFERENCES batches (id);

-- The selection of the nightly batch: a user's extracted items, oldest first.
CREATE INDEX items_user_status ON items (user_id, status, id);

-- The items of a batch, when its results come back (E3-02).
CREATE INDEX items_batch ON items (batch_id) WHERE batch_id IS NOT NULL;

-- Set when the starting categories were seeded for the user, so that they are seeded once:
-- a user who later deletes every category does not get them back.
ALTER TABLE users ADD COLUMN categories_seeded_at TEXT;        -- ISO 8601 UTC
