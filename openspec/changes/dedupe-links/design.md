# Design

## Context

See `proposal.md` for the motivation. E2-01 left: `items.normalized_url` always null and without index; `items.client_id` with its partial unique index `items_user_client_id` (`00002_items.sql`); `store.CaptureItem` doing replay-or-insert in one `BEGIN IMMEDIATE` transaction, with a safety net that reads any unique violation as a `client_id` replay (`isUniqueViolation`, `items.go`); `capture.Service.Capture` validating and calling the store. `capture` imports `store` (`capture.go`); `store` knows no domain (`docs/dev/conventions-backend.md`, "Dépendances entre couches").

## Goals / Non-Goals

**Goals:**
- One reference for replays: every `client_id` received, whatever the outcome.
- Deduplication that cannot lose a note nor a privacy mark, even through the safety net.

**Non-Goals:**
- Backfilling `normalized_url` of links captured before this change. ADR-0032 allows SQL migrations only, and RG-05 cannot be written in SQL; no real database exists yet (E6). Those links stay outside deduplication. A future change of RG-05 that needs to renormalise stored links will need a decision on data migrations first.
- What the nightly batch does with an item made private while `submitted` (RG-15 on an item already sent): a question for Ghislain in the batch story (E3); its selection must re-check `is_private` in the sending transaction.

## Vocabulary

- **Capture identifier** (glossary "Identifiant de capture", `client_id`): stored in the new table `capture_ids`, one row per `(user_id, client_id)`, pointing to the item the capture resolved to (created, merged into, or replayed).

## Decisions

### Packages and contract touched

| Area | Change |
| --- | --- |
| `api/openapi.yaml` | Description of the `200` of `createItem`: first note undated, identical note ignored, privacy only tightens (RG-04, RG-15). No schema change; `make generate` refreshes the bundle. |
| `backend/internal/capture` | `normalize.go` (RG-05, pure), `note.go` (RG-04 note merge, pure), Paris time zone; `Capture` passes the normalised URL and the merge inputs to the store. |
| `backend/internal/store` | `00003_capture_ids.sql`; `items.go` reworked into replay → duplicate → insert; `capture_ids` queries; a reusable `setPrivate` primitive for RG-14 later. |
| `backend/internal/httpapi` | No logic change; tests updated (scenarios of the delta). |
| `backend/cmd/hippo` | Loads the Paris time zone at start-up and refuses to start if it fails. |

Prompt and fiche schemas: unchanged, so no eval set run.

### Migration `00003_capture_ids.sql` (new file, SQL only)

A migration is required; it is a new file (`docs/dev/conventions-backend.md`, "Migrations"), pure SQL (ADR-0032):

1. `CREATE TABLE capture_ids (user_id TEXT NOT NULL REFERENCES users(id), client_id TEXT NOT NULL, item_id TEXT NOT NULL REFERENCES items(id), created_at TEXT NOT NULL, PRIMARY KEY (user_id, client_id)) STRICT`.
2. `INSERT INTO capture_ids SELECT user_id, client_id, id, created_at FROM items WHERE client_id IS NOT NULL`, so the replays of captures made before the migration keep working.
3. `DROP INDEX items_user_client_id`, then `ALTER TABLE items DROP COLUMN client_id` (SQLite 3.35+, embedded 3.54.1: `modernc.org/sqlite@v1.61.0/lib/sqlite.go:4564`; a column can be dropped once no index uses it, https://www.sqlite.org/lang_altertable.html "ALTER TABLE DROP COLUMN"). One source of truth for replays.
4. `CREATE UNIQUE INDEX items_user_normalized_url ON items (user_id, normalized_url) WHERE normalized_url IS NOT NULL` (RG-04). Existing rows have a null `normalized_url`, so the index builds whatever the data.

`capture_ids` is not synchronised to the app (it is not in `Item`), so its writes do not advance `change_seq`.

### Normalisation (RG-05), on the string

`capture.Normalize(trimmedURL) string`, pure, called after the URL is validated by `net/url.Parse` (E2-01). It works on the raw string, because `url.URL.String`, `ParseQuery` and `Values.Encode` re-encode and drop pairs (Go 1.27.3 `net/url`):

1. Cut at the first `#` (fragment removed), then at the first `?`.
2. In `scheme://authority`, lower-case the scheme and the host only; user info and port are kept as sent. An internationalised host is lower-cased with `strings.ToLower`, never converted (punycode would need `golang.org/x/net/idna`).
3. Path: remove one trailing `/`, the root included (`https://example.com/` → `https://example.com`, `/a//` → `/a/`); internal `//` and `%2F` kept.
4. Query: split on raw `&`; drop empty pairs; drop pairs whose name (the part before `=`, raw) lower-cased is `fbclid`, `gclid`, `igsh`, `si`, `ref` or starts with `utm_`; sort the remaining pairs by raw name with a stable sort (values of a same name keep their order); join with `&`; no `?` when nothing is left.

The unit tests cover every rule and the scenarios of the delta.

### Note merge (RG-04)

`capture.MergeNote(existing *string, note string, day string) (merged *string, changed bool)`, pure:

- `existing` null → `note`, changed.
- Otherwise split `existing` into entries: entry 0 is the text before the first separator `\n\nYYYY-MM-DD — ` (a blank line, a date, a space, U+2014, a space); each following entry is the text after its separator. If `note` is byte for byte equal to an entry's text → unchanged. Else → `existing + "\n\n" + day + " — " + note`, changed.

The day is computed by `capture` from the store's clock in `Europe/Paris`: the server's reception day (an offline capture sent the next day is dated the next day, RG-06). The location is loaded once with `time.LoadLocation("Europe/Paris")`, with `import _ "time/tzdata"` in `cmd/hippo` so the distroless image needs no system time zone database; `main` refuses to start if it fails. The `TZ` variable (ADR-0019) is not used for this: RG-04 says Paris, and Go silently falls back to UTC on an unknown `TZ` (`$GOROOT/src/time/zoneinfo_unix.go:59-68`).

The merge runs inside the store transaction, so it reads the note it extends: `store.CaptureItem` takes the merge as a function argument (`func(existing *string) (*string, bool)`) built by `capture` from `MergeNote` and the day. The rule stays in the domain, the atomicity in the store.

### Capture transaction

One `BEGIN IMMEDIATE` transaction (`modernc.org/sqlite@v1.61.0/tx.go:23`):

1. **Replay**: `client_id` given and found in `capture_ids` for this user → return that item, `created = false`, nothing written.
2. **Duplicate**: `normalized_url` found for this user → compute the new note (merge function) and the new privacy (`existing OR requested`). If either changed: update `user_note`, `is_private`, `updated_at = now`, `change_seq = next` (through `sequence.go`). Insert the `capture_ids` row if a `client_id` was given. Return the item read back, `created = false`.
3. **Insert**: as in E2-01, with `normalized_url`, then the `capture_ids` row; `created = true`.

The duplicate lookup and the unique index use the same predicate (`user_id`, `normalized_url`, not null); the deletion story must keep them aligned if it adds a `deleted_at` filter.

### Safety net

Writers are serialised, so a unique violation is practically unreachable; it stays minimal. It is told apart by the statement that failed, not by the SQLite message: a violation on the `capture_ids` insert → read the item by `client_id` (replay); a violation on the `items` insert (only `items_user_normalized_url` can raise one now) → run the whole transaction once more, which takes the duplicate path and merges the note and privacy. The store test that reaches it uses the unexported variant of E2-01 (`captureItem(…, replayFirst)`), extended to skip the duplicate lookup.

### Privacy (RG-15)

`setPrivate` sets `is_private` and leaves the status and the fiche alone (RG-14: the fiche is kept, re-analysis is prevented by the flag). It is the primitive the fiche's switch (RG-14) will reuse. A duplicate never clears the flag.

### user_id, private items and budget

- Every query on `items` and `capture_ids` filters by `user_id = ?` (RG-90); the replay joins `capture_ids` and `items` on both `item_id` and `user_id`. Integration tests cover two users with the same URL and the same `client_id`.
- Private items: a capture can only set the flag, never clear it; no path reaches the AI (RG-12).
- Budget: no AI call.

### Logs

The normalised URL and the notes are never logged; errors of `net/url` are never wrapped (E2-01 rule). The log scenario covers the duplicate path with a note.

## Risks / Trade-offs

- [Links captured before this change are never deduplicated] → only test data exists (no deployment before E6).
- [RG-05 merges pages that differ only by `ref` or the fragment] → the first capture's URL is kept; reported to Ghislain as a limit of the rule.
- [A note grows with every new note of a duplicate, and is returned in every `Item`] → acceptable in V1.
- [Adding a note does not re-analyse the item] → RG-03 makes the note an analysis hint, but no rule asks for a re-analysis; the search story (RG-95) must re-index `user_note` on update.
- [Concurrent captures of one URL with different `client_id`] → serialised writers make the second one take the duplicate path; the integration test sends them at once.

## Migration Plan

`00003_capture_ids.sql` runs at start-up (ADR-0032). The copy of `client_id` values runs before the column is dropped, in the same migration (goose runs a SQL file in a transaction by default). Rollback is the backup (ADR-0020).
