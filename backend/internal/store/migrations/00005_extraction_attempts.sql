-- +goose Up
-- Extraction attempts of links (docs/spec-v1.md, "Pipeline"). Story E2-05; design of the
-- extract-web-page change.

-- started_at is set, and committed, before a link is downloaded; the row is deleted when the
-- item is written as extracted. A row whose started_at is still set at the next start means
-- the previous attempt never finished (the backend crashed or was killed while reading the
-- page): the link falls back without being downloaded again. A stop of the backend or a
-- network outage of the NAS clears started_at and keeps the row; network_retries counts the
-- outages met by this link, which falls back at the third. outcome deferred_pdf keeps a link
-- answered as a PDF queued until its own extraction (E2-08). Not synced to the app, so its
-- writes do not advance change_seq. The composite foreign key ties an attempt to an item of
-- the same user (RG-90).
CREATE TABLE extraction_attempts (
    item_id         TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    started_at      TEXT,                                      -- ISO 8601 UTC; NULL when no download is under way
    network_retries INTEGER NOT NULL DEFAULT 0,
    outcome         TEXT CHECK (outcome IN ('deferred_pdf')),
    FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id)
) STRICT;
