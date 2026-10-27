# Proposal

## Why

Hippo cannot store anything yet: `POST /api/items` answers `501` and the database only holds the user. Capturing a link is the first step of every scenario of the V1 and the first business code of the backend; it implements backlog story **E2-01 — Capturer un lien**.

## What Changes

- `POST /api/items` with a JSON body holding a `url` creates a `link` item, status `queued`, read status `to_read`, owned by the authenticated user, and answers `201` with the full item (RG-01, RG-90). No extraction and no analysis runs during the capture (RG-01).
- The request body is validated before anything is stored. A JSON body of an implemented operation is strict: an undeclared member (a misspelt `is_private` would otherwise store a public item, RG-12), data after the value, a malformed `client_id` or an unknown `source` answer `400 about:blank`; a JSON body over 1 MiB answers `413`.
  - surrounding whitespace is trimmed from `url` and `user_note`, and a blank `user_note` counts as absent (RG-09, new);
  - neither `url` nor `note` → `400 empty-capture` (RG-02);
  - `url` and `note` together, or `note` and `user_note` together → `400 ambiguous-capture` (RG-07);
  - a `url` that is not an absolute `http` or `https` URL with a non-empty host → `400 invalid-url` (RG-02).
- `user_note` is stored as the capture note of the link (RG-03); `is_private` is stored, `false` by default (RG-10).
- A capture carrying a `client_id` already used by the same user creates nothing and answers `200` with the existing item in its current state (RG-08), including under concurrent replays. The same `client_id` from another user is independent (RG-90).
- Until extraction runs, the title of a link is the host name of its URL (RG-26, new).
- **Contract**: `CaptureRequest` and `CaptureImageRequest` gain an optional `source` (`share` or `manual`, default `share`), so the item records where it was captured (glossary "Source de capture"); the strict-body and 1 MiB rules are stated in the contract.
- **Data**: first business migration: the `items` table with the columns of `docs/spec-v1.md` except those referencing tables not created yet (`type_id`, `category_id`, `batch_id`, added by their stories), the per-user uniqueness of `client_id`, and the database-wide modification sequence `change_seq` that orders the offline sync (ADR-0035). The uniqueness of `normalized_url` comes with E2-02.
- A note capture (JSON `note`) and an image capture (multipart) keep answering `501 not-implemented` until E2-03 and E2-04; any other body format answers `415`.
- The answer comes without any network call, well within the 200 ms of E2-01.

## Non-goals

- Deduplication by normalised URL and appending a capture note to an existing item (RG-04, RG-05): story E2-02. `normalized_url` stays empty until then.
- Note capture (E2-03) and image capture (E2-04), beyond the shared validation above.
- Extraction of the title, site and content (E2-05 and following), and any analysis.
- Reading items back (`GET /api/items`, `GET /api/items/{item_id}`), full-text search (`items_fts`) and sync (`GET /api/sync`).
- Any change in the app or the share extension.

## Capabilities

### New Capabilities

- `item-capture`: capturing an item through `POST /api/items`, its validation, idempotent replay and the state of a freshly captured item. Later capture stories (E2-02 to E2-04) extend it.

### Modified Capabilities

- `api-conventions`: implemented operations leave the "every operation answers 501" sweep; a JSON body of an implemented operation with an undeclared member or trailing data answers `400`; a JSON body over 1 MiB answers `413`.

## Impact

- **Story**: E2-01.
- **Business rules**: RG-01, RG-02, RG-03, RG-07, RG-08, RG-09 (new), RG-10, RG-26 (new), RG-90. RG-09 and RG-26 were decided with Ghislain on 2026-10-27 and added to `docs/metier.md` by this change.
- **ADRs**: ADR-0003 (Go backend), ADR-0004 (SQLite), ADR-0016 (bearer token), ADR-0017 (contract first), ADR-0019 (NAS resources, body limit), ADR-0022 (user_id everywhere), ADR-0032 (goose migrations), ADR-0033 (tests on a real SQLite), ADR-0035 (identifiers and sync sequence).
- **AI cost**: none, no call to the AI.
- **Private items**: stored with `is_private`; nothing is sent to the AI on any path of this change.
- **Persisted data**: new `items` table and modification sequence, through a new migration; no existing data is touched.
- **API contract**: `source` added to both capture bodies (optional, backward compatible); strict bodies and the 1 MiB limit documented; `createItem` implemented for JSON link captures.
- **Prompt and fiche schemas**: none.
- **Code**: `backend/internal/capture` (new domain package), `backend/internal/store`, `backend/internal/httpapi`, `cmd/hippo` wiring.
- **Dependencies**: none new.
- **User QA: no** (nothing visible in the app yet).

## Open questions

None left: the capture source, the provisional title and the whitespace handling were decided with Ghislain on 2026-10-27.
