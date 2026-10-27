# Design

## Context

See `proposal.md` for the motivation. Today `POST /api/items` is the `CreateItem` stub of `backend/internal/httpapi/stubs.go`, the database holds only `users` (`00001_init.sql`), and every route already passes through `requireAuth`, which puts the user id in the request context (`internal/auth`). Write transactions start with `BEGIN IMMEDIATE`: `_txlock=immediate` in `internal/store/store.go`, honoured for every non read-only transaction (`modernc.org/sqlite@v1.61.0/tx.go:23`), so SQLite serialises writers.

The generated server decodes JSON bodies with `encoding/json` defaults (`oapi-codegen/runtime@v1.8.0/bindjson.go:36-56`): unknown members are ignored, names match case-insensitively, data after the first value is ignored, and `pattern` and `enum` (the `Ulid` and `CaptureSource` schemas, `type Ulid = string` in `api.gen.go`) are not checked. The adversarial pass showed what that costs: `{"isPrivate": true}` would store a public item (RG-12), `"client_id": ""` would turn every later capture into a replay (RG-08).

## Goals / Non-Goals

**Goals:**
- A first domain package and a first business table that the next capture stories (E2-02 to E2-04) extend without reshaping.
- The modification sequence of ADR-0035, enforced from the first synced row.
- Strict JSON bodies on implemented operations, without a new dependency.

**Non-Goals:**
- Reading items through the API, sync, search: later stories add their queries.
- Validating every request against the full contract (a generic OpenAPI validator such as `kin-openapi`'s `openapi3filter` would be a new direct dependency and a transverse choice for an ADR; not needed for one route).
- Deletion of items: its story will null `client_id` and `normalized_url` on the deletion mark (ADR-0035 marks carry no content), so that a replay or a new capture never returns a mark.

## Decisions

### Packages and contract touched

| Area | Change |
| --- | --- |
| `api/openapi.yaml` | `source` (optional `$ref: CaptureSource`, default `share`) on `CaptureRequest` and on `CaptureImageRequest` (so E2-04 does not reopen the contract); `createItem` 413 also covers a JSON body over 1 MiB (`about:blank`); the introduction states the strict-body and 1 MiB rules. No new operation, no new problem type. |
| `backend/internal/capture` (new) | Domain of the capture: cleaning (RG-09), validation (RG-02, RG-07, `client_id`, `source`), provisional title (RG-26), call to the store. No `net/http`, no generated code (`docs/dev/conventions-backend.md`, "Dépendances entre couches"); a new `depguard` rule of `backend/.golangci.yml` (`domains-know-no-transport`) refuses `net/http` there, the existing one already refuses `httpapi`. |
| `backend/internal/store` | `00002_items.sql`; `items.go` (insert-or-replay, read by id); `sequence.go` (the only writer of `sync_sequence`); `CreateUser`, shared with `EnsureSingleUser`. |
| `backend/internal/httpapi` | `CreateItem` handler; strict JSON body and size-limit middleware; mapping of `capture` errors to problems and of a stored item to `Item`; `CreateItem` stub removed. |
| `backend/cmd/hippo` | Wiring of the capture service into `httpapi.Options`. |

Prompt and fiche schemas: unchanged, so no eval set run.

### Migration `00002_items.sql` (new file)

A migration is required; it is a new file and `00001_init.sql` is untouched (`docs/dev/conventions-backend.md`, "Migrations"). SQLite 3.54.1 is embedded (`modernc.org/sqlite@v1.61.0/lib/sqlite.go:4564`): `STRICT` (3.37) and `RETURNING` (3.35) are available.

- `sync_sequence (id INTEGER PRIMARY KEY CHECK (id = 1), value INTEGER NOT NULL)`, seeded with `(1, 0)` by the migration.
- `items`, `STRICT`, with the columns of `docs/spec-v1.md` "Modèle de données" **except** `type_id`, `category_id` and `batch_id`, which reference tables that do not exist yet. Their stories add them with `ALTER TABLE items ADD COLUMN … REFERENCES …`, which SQLite allows for a column whose default is NULL (https://www.sqlite.org/lang_altertable.html, "ALTER TABLE ADD COLUMN"; unverified on a real migration until then).
- `user_id TEXT NOT NULL REFERENCES users(id)`; `change_seq INTEGER NOT NULL` **without default**, so an insert that forgets the sequence fails.
- `CHECK` constraints on the 0/1 flags, on `read_progress` between 0 and 1, and on the closed enumerations `kind`, `status`, `extraction_mode`, `read_status`, `freshness`. **No `CHECK` on `source`**: V2 adds `rss`, and changing a `CHECK` means rebuilding the table; the domain validates it. Changing one of the other enumerations later costs a table rebuild, accepted: they are part of the V1 model.
- Defaults: `is_private 0`, `content_truncated 0`, `incomplete_retried 0`, `is_pinned 0`, `read_status 'to_read'`, `read_progress 0`, `attempts 0`, `user_edited '[]'`, `search_text ''`. Absent values are stored as NULL, never `''` (`client_id`, `normalized_url`, `user_note`).
- Indexes: `UNIQUE (user_id, client_id) WHERE client_id IS NOT NULL` (RG-08) and `(user_id, change_seq)` for `GET /api/sync`. The unique index on `(user_id, normalized_url)` is **left to E2-02**: that story fills `normalized_url` for the links captured in between (in Go, before creating the index), and decides what to do with two identical links already captured. An index is created without rebuilding the table.

### The modification sequence (ADR-0035)

`store/sequence.go` holds the only function that advances the sequence: `UPDATE sync_sequence SET value = value + 1 WHERE id = 1 RETURNING value`, called inside the caller's transaction. Every write of a synced row in the store goes through it; `change_seq NOT NULL` without default catches a forgotten insert, and a store test checks that each write raises the sequence. Alternatives: triggers (hidden from the reader of the Go code; `recursive_triggers` off by default makes them workable, but four tables would each need two), or `MAX(change_seq) + 1` per table (not database-wide, and a deleted maximum would be reused).

### Insert or replay in one transaction (RG-08)

`store` exposes one function that, in a single transaction: when a `client_id` is given, reads the item of this user with this `client_id` and returns it with `created = false`; otherwise advances the sequence, inserts the row, reads it back and returns it with `created = true`. Writers being serialised, two concurrent replays cannot both insert; the unique index is the backstop, and a unique violation on it is answered by reading the existing row (replay), never by a `500`. The backstop is reached in a store test through an unexported variant that skips the lookup (`captureItem(…, replayFirst bool)`), since serialised writers never reach it otherwise. The answer is always built from the row read back, so a `201` and a later `200` serialise the same dates (millisecond `timeLayout`).

The replay runs before any future deduplication by URL (E2-02), so that a replay never appends a note (RG-08 over RG-04).

### Strict JSON bodies and the 1 MiB limit

A middleware passed to the generated router (`StdHTTPServerOptions.Middlewares`, run inside the mux so `r.Pattern` is the operation's pattern, e.g. `POST /api/items`):

- wraps every `application/json` body in `http.MaxBytesReader` (1 MiB);
- for the patterns of a small registry (`POST /api/items` → `CaptureRequest` in this change; each story adds its operations), reads the body, decodes it with `DisallowUnknownFields` and checks that nothing follows the value; a failure answers `400 about:blank`; the body is then restored for the generated decoder.

An oversized body answers `413 about:blank`, whether the middleware or the generated decoder hits the limit (`errors.As(err, *http.MaxBytesError)` in the request error handler). Multipart bodies are not limited here: E2-04 owns the 20 MB image rule. Duplicate keys keep the last value and member names still match case-insensitively (`"URL"` sets `url`): harmless, left as `encoding/json` does it.

### Validation order

1. `401` (`requireAuth`), then `413` / `400 about:blank` (middleware and generated decoder).
2. Neither JSON nor multipart (both bodies nil) → `415`; multipart → `501` (E2-04).
3. `client_id` present and not a ULID in its canonical form (`oklog/ulid/v2` `ParseStrict`, already a dependency, and the parsed id printed back equal to the value: `ParseStrict` accepts lower case, the contract pattern does not; `""` included) → `400 about:blank`; `source` present and not `share`/`manual` → `400 about:blank`.
4. Trim `url` and `user_note` (`strings.TrimSpace`); a blank `user_note` becomes absent (RG-09). `note` is not trimmed here (E2-03).
5. `url` and `note` present, or `note` and `user_note` present → `ambiguous-capture` (RG-07).
6. Neither `url` nor `note` → `empty-capture` (RG-02). A JSON `null` body decodes to an empty request and lands here.
7. `note` alone → `501 not-implemented` (E2-03).
8. `url` not valid → `invalid-url` (RG-02): `net/url.Parse` succeeds, scheme `http` or `https` (net/url lower-cases it), `Hostname()` not empty (`https://:443` and the opaque `https:example.com` fail). User info (`user:pass@`) is accepted: RG-02 does not exclude it. The stored `url` is the trimmed string, not `u.String()`.
9. Replay (`200`) or insert (`201`).

Ambiguity comes before the URL check so that the answer names the real problem. The `*url.Error` of `net/url` prints the URL: the domain never wraps it, it returns a bare `invalid-url` error, so the URL cannot reach a log through `ResponseErrorHandlerFunc`.

### Provisional title (RG-26)

`strings.ToLower(u.Hostname())`: no port, IPv6 without brackets, `www.` kept, an internationalised host kept as sent (converting would need `golang.org/x/net/idna`, a new dependency). Extraction replaces it (E2-05).

### Mapping to the `Item` schema

`httpapi` maps the stored item field by field. Columns without a value map to `null`; `tag_ids`, `assets` and `user_edited` to empty arrays, never `nil`; `fiche` is omitted; `content_type` and `category_id` are `null` (no column yet). An integration test validates the `201` body against the `Item` schema of `api/openapi.bundled.gen.json` with `santhosh-tekuri/jsonschema/v6` (already a direct dependency, `cmd/schemabundle/validate.go`).

### user_id, private items and budget

- Every query of `items.go` has `user_id = ?` in its `WHERE` clause, and the insert takes the user id from the authenticated context (`auth.UserID`), never from the body (RG-90; `docs/dev/conventions-backend.md`, "Stockage SQLite"). A two-user integration test proves that a `client_id` is scoped to its user.
- Private items: `is_private` is stored, and the strict body guarantees a misspelt flag is refused rather than silently ignored. No path of this change reaches the AI; the RG-12 test belongs to the first story that sends anything (E3).
- Budget: no AI call, so no budget check on these paths.

### Second user in tests

`items.user_id` references `users(id)`, so the two-user test needs a real second user. `store.CreateUser` inserts one (used by `EnsureSingleUser` and by tests); a test authenticator maps two test tokens to two user ids. No production path creates a second user.

"Nothing is stored" is checked by `storetest.ItemCount`, which counts a user's items through a read-only connection to the test database (`storetest.OpenWithPath`): no API reads items yet, and the store gains no production method for tests.

### Clock and identifiers

Dates and the ULID timestamp both come from the store's injected clock (`Store.now`): `ulid.New(ulid.Timestamp(now), entropy)` (its error returned, no panic) with a monotonic entropy source guarded by a mutex (`oklog/ulid/v2@v2.1.2`, `ulid.go` `Monotonic`).

### Response time

E2-01 asks for an answer in under 200 ms. The capture runs no network call and one short transaction; no timing assertion is written (it would be flaky in CI).

## Risks / Trade-offs

- [The strict-body registry must grow with each implemented operation] → the "every operation answers 501" test lists implemented operations; a story that implements one without registering its body leaves the old lenient decoding, caught by its own unknown-member scenario (api-conventions delta).
- [Case-insensitive member names and duplicate keys stay accepted] → no field of the contract differs from another by case only.
- [`normalized_url` empty for links captured before E2-02] → no real data before the app ships (E5); E2-02 fills it before creating its index.
- [Columns added later by `ALTER TABLE`] → their order differs from `docs/spec-v1.md`; nothing depends on it.
- [The 1 MiB limit and the strict decoding key on `Content-Type: application/json`, while the generated decoder of a JSON-only operation decodes any body] → harmless today (`POST /api/items` answers `415` without a JSON or multipart type); the first story that implements a JSON-only operation applies both to every non-multipart body and adds a "body without Content-Type" scenario.
- [The replay safety net treats any unique violation as a `client_id` replay] → only one unique index exists today; E2-02, which adds the `(user_id, normalized_url)` index, must tell the two apart, or a duplicate link carrying a new `client_id` would answer `500`.
- [`httpapi` reads `store.Item` through `capture.Result`] → accepted for now: the domain adds no behaviour to the item yet; the layer diagram of `docs/dev/conventions-backend.md` gets the read-only arrow if a later domain keeps the pattern.

## Migration Plan

The new migration runs at start-up like the first one (ADR-0032). No rollback migration: rollback is the backup (ADR-0020). No existing data.
