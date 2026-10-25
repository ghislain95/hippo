-- +goose Up
-- The single V1 user, created at first start (docs/spec-v1.md, "Modèle de données").
-- Only the SHA-256 of the API token is stored (ADR-0016).
CREATE TABLE users (
    id             TEXT PRIMARY KEY,           -- ULID
    name           TEXT NOT NULL,
    api_token_hash TEXT NOT NULL UNIQUE,
    created_at     TEXT NOT NULL               -- ISO 8601 UTC
) STRICT;
