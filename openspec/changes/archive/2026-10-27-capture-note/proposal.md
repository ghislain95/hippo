# Proposal

## Why

A thought, a tip heard in a meeting or a list typed on the phone has no URL, so Hippo cannot keep it yet: a JSON capture holding a `note` answers `501`. This change implements backlog story **E2-03 — Capturer une note**.

## What Changes

- `POST /api/items` with a JSON body holding a `note` creates a `note` item owned by the authenticated user, status `queued`, read status `to_read`, and answers `201` (RG-01, RG-90). The text is stored as the item's `user_note`, as the glossary and `docs/spec-v1.md` define for a note item.
- The text is trimmed of surrounding whitespace, inner whitespace kept; a text empty after trimming answers `400 empty-note` (RG-02, RG-09 extended on 2026-10-27).
- A note with a capture note answers `400 ambiguous-capture`, already in place since E2-01 (RG-07).
- Until analysis, the title of a note is its first non-empty line, cut at 80 characters and followed by `…` when longer (RG-26 extended on 2026-10-27).
- `is_private`, `source` and the replay by `client_id` (recorded in `capture_ids`) apply as for a link (RG-08, RG-10). A note is never a duplicate (RG-04): the same text twice makes two items.
- No business limit on the length of a note: the 1 MiB limit of JSON bodies applies (decided with Ghislain on 2026-10-27).

## Non-goals

- Image capture (E2-04): multipart keeps answering `501`.
- Extraction and analysis of a note (E2-05, E3).
- Any change to the app.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `item-capture`: a note capture is created instead of answering `501` (the requirement "Note and image captures are not available yet" is replaced by "Image captures are not available yet"); the provisional title covers notes; the "blank capture note" scenario now expects a created note.

## Impact

- **Story**: E2-03.
- **Business rules**: RG-01, RG-02, RG-04, RG-07, RG-08, RG-09 (extended), RG-10, RG-26 (extended), RG-90. Extensions decided with Ghislain on 2026-10-27 and written in `docs/metier.md` by this change.
- **ADRs**: ADR-0022 (user_id everywhere), ADR-0033 (tests on a real SQLite).
- **AI cost**: none.
- **Private items**: `is_private` stored; no path reaches the AI.
- **Persisted data**: none new (no migration): a note uses the `items` columns created by E2-01 (`kind = note`, `user_note`, `normalized_url` null).
- **API contract**: none; `createItem` already declares the note body and `empty-note`. The description of the `501` path is checked.
- **Prompt and fiche schemas**: none.
- **Code**: `backend/internal/capture`, `backend/internal/store` (insert without normalised URL), `backend/internal/httpapi` tests.
- **Dependencies**: none.
- **User QA: no**.

## Open questions

None left: title, trimming and length were decided with Ghislain on 2026-10-27.
