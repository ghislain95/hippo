-- +goose Up
-- Capture identifiers and link deduplication (docs/spec-v1.md, "Modèle de données"). Story
-- E2-02; design of the dedupe-links change.

-- Every client_id received, with the item the capture resolved to: created, merged into
-- (RG-04) or replayed (RG-08). The single reference for replays. Not synced to the app, so its
-- writes do not advance change_seq. The same client_id of another user is independent (RG-90).
CREATE TABLE capture_ids (
    user_id    TEXT NOT NULL REFERENCES users (id),
    client_id  TEXT NOT NULL,                                  -- chosen by the device (RG-08)
    item_id    TEXT NOT NULL REFERENCES items (id),
    created_at TEXT NOT NULL,                                  -- ISO 8601 UTC
    PRIMARY KEY (user_id, client_id)
) STRICT;

-- Captures made before this migration keep being recognised as replays.
INSERT INTO capture_ids (user_id, client_id, item_id, created_at)
SELECT user_id, client_id, id, created_at FROM items WHERE client_id IS NOT NULL;

-- capture_ids replaces items.client_id; a column used by an index cannot be dropped.
DROP INDEX items_user_client_id;
ALTER TABLE items DROP COLUMN client_id;

-- A link is stored once per user, recognised by its normalised URL (RG-04, RG-05). Links
-- captured before E2-02 have a NULL normalized_url and stay outside deduplication.
CREATE UNIQUE INDEX items_user_normalized_url ON items (user_id, normalized_url) WHERE normalized_url IS NOT NULL;
