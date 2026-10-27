# Proposal

## Why

Since E2-01, capturing the same link twice creates two items, so the library fills with copies and a link shared again with a new note scatters the intent across items. This change implements backlog story **E2-02 — Dédoublonner les liens**: a link is stored once per user, recognised by its normalised URL.

## What Changes

- Every link capture computes the **normalised URL** of RG-05: scheme and host in lower case, fragment removed, tracking parameters removed (`utm_*`, `fbclid`, `gclid`, `igsh`, `si`, `ref`), trailing slash removed, remaining parameters sorted. The original URL is kept unchanged for opening (RG-05).
- A link capture whose normalised URL matches an item of the same user creates nothing and answers `200` with the existing item (RG-04):
  - its capture note, if any, is appended to the existing note after a blank line, prefixed by the day's date in Paris time (`2026-10-27 — <note>`); an item without a note takes the new note as is, undated; a note identical to one already present is not appended again (RG-04, precised on 2026-10-27);
  - a duplicate capture marked private makes the existing item private; an unmarked one never makes a private item public (RG-15, new);
  - nothing else of the existing item changes (`url`, `source`, title, status).
- Every `client_id` received is remembered with the item it resolved to, including the `client_id` of a capture merged into an existing item, so that replaying a duplicate capture never appends its note twice nor makes the item private again (RG-08 over RG-04). The replay check stays first.
- **Data**: one SQL migration creates the table of capture identifiers (the single reference for replays), copies the existing `items.client_id` values into it, drops that column and its index, and makes `(user_id, normalized_url)` unique. Links captured before this change keep a null `normalized_url` and are never found as duplicates: no real database exists yet (E6), so no backfill is written. A duplicate capture that changes the item (note or privacy) updates `updated_at` and advances the modification sequence; one that changes nothing writes nothing to `items` (only its capture identifier is recorded, a table not synchronised) (ADR-0035).
- **Contract**: the description of the `200` of `createItem` follows the precised RG-04 and RG-15 (documentation only, no schema change).

## Non-goals

- Normalisation beyond RG-05: default ports, `www.`, `http` versus `https`, user info, percent-encoding, internal `//` and path case are kept as sent; internationalised hosts are lower-cased, not converted to punycode.
- Filling `normalized_url` for links captured before this change (no real data yet).
- Note and image captures (E2-03, E2-04), which are never duplicates (RG-04).
- Deleting items, and what a deletion mark does to `normalized_url` and the recorded `client_id` values (the deletion story).
- Any change to the app or the share extension.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `item-capture`: duplicate links answer `200` with the existing item, the capture note is appended dated, privacy only tightens, and a replay covers every recorded `client_id`. The scenario "Captures without client_id are never replays" changes: the same URL twice is now a duplicate.

## Impact

- **Story**: E2-02.
- **Business rules**: RG-04 (precised on 2026-10-27: note format, no repeated note), RG-05, RG-08, RG-12, RG-14, RG-15 (new), RG-90. The precision of RG-04 and RG-15 were decided with Ghislain on 2026-10-27 and are written in `docs/metier.md` by this change.
- **ADRs**: ADR-0004 (SQLite), ADR-0022 (user_id everywhere), ADR-0032 (goose SQL migrations), ADR-0033 (tests on a real SQLite), ADR-0035 (modification sequence).
- **AI cost**: none.
- **Private items**: a duplicate capture can only make an item private, never public, so no item leaves the private set through a capture (RG-12).
- **Persisted data**: new SQL migration; new table of capture identifiers, existing `client_id` values moved into it, `items.client_id` dropped; no real database exists yet (E6 not done).
- **API contract**: description of the `200` of `createItem` only; no schema, no new response.
- **Prompt and fiche schemas**: none.
- **Code**: `backend/internal/capture` (normalisation, note merge, Paris time zone loaded with `time/tzdata`), `backend/internal/store` (migration, lookup, merge, capture identifiers), `backend/cmd/hippo` (the service refuses to start if the Paris time zone cannot be loaded).
- **Dependencies**: none new (`time/tzdata` is the standard library).
- **User QA: no** (nothing visible in the app yet).

## Open questions

None left: the note format, repeated notes and privacy of a duplicate were decided with Ghislain on 2026-10-27.

Known limit of RG-05, reported to Ghislain: removing `ref` and the fragment merges pages that differ only by them (`?ref=<branch>` on a code forge, `#!` routes); the first capture's URL is the one kept.
