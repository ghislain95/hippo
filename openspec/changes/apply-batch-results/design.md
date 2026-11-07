# Design

## Context

See proposal.md for the motivation and every decision taken with Ghislain (2026-11-06 and, after the adversarial pass, 2026-11-07). This design settles how they are built; the specs say what is observable.

Current state that shapes the approach (E3-01, archive `2026-11-06-submit-nightly-batch`):

- One `analysis.Pass` per process (`backend/internal/analysis/pass.go:103-106`), whose mutex serialises `Run` (the nightly pass) and `Reconcile` (start-up). `cmd/hippo/analysis.go` starts `Reconcile` and `schedule.Run` in two goroutines under one cancellable context and waits for them before closing the store.
- `runUser` reconciles the user's `pending` batches, then checks `HasBatchWithoutCost` (`backend/internal/store/batches.go:55-66`: `pending`, `in_progress`, or `ended` with `cost_usd IS NULL` block), then the month spend, then sends.
- `batches` (migration `00006_analysis.sql`) has a `CHECK` on `status IN ('pending','in_progress','ended','failed')`, the four token columns, `cost_usd`, `ended_at`; `items` has `type_id`, `category_id`, `batch_id`, and since E2-01 every analysis column except an envelope version. `content_types.key` is `UNIQUE` without `user_id` (`00006_analysis.sql:15`). `categories` has no normalised-name column.
- `analysis.Analyzer` has `CreateBatch` and `ListBatches` (`backend/internal/analysis/analyzer.go`); `internal/analysis/anthropic` is the only importer of the SDK (`anthropic-sdk-go/v2` v2.7.0, depguard `anthropic-only-in-analysis`); `analysistest` fakes it.
- The référentiel builder writes an empty `<tags>` block (`backend/internal/analysis/referentiel.go`).
- `httpapi.itemFromStore` (`backend/internal/httpapi/items.go:173-197`) hard-codes `content_type`, `category_id` null and `tag_ids` `[]`, without `fiche`; it serves `createItem` (duplicate, replay) and `updateItem`.
- `santhosh-tekuri/jsonschema/v6` v6.0.3 is already a direct dependency (`cmd/schemabundle/validate.go`); `golang.org/x/text` v0.43.0 is indirect (`backend/go.mod:68`) and becomes direct (decision 7).

## Goals / Non-Goals

**Goals:**

- A batch accepted by Anthropic always ends, in Hippo, as `ended` with a cost, whatever the service answers (results, archive, not found), so that the guard never blocks for good.
- Every `submitted` item of a batch ends `analyzed`, `extracted` or `failed`; none stays `submitted` once its batch is ended.
- Bounded memory: one result line at a time, at most 4 MiB.
- Idempotent at every step: a stop at any point loses nothing and applies nothing twice.

**Non-Goals:**

- The items' full-text index `items_fts` (E4 search, ADR-0024): this change writes `search_text`, the index comes with its triggers in E4.
- The tag and category routes (`/api/tags`, `/api/categories`, E4) and `GET /api/sync` (E4): this change only fills the `Item` the two implemented operations already answer.
- A per-user content type (decision 4).
- Reading the results of a batch failed by an ambiguous reconciliation (proposal, Non-goals).

## Vocabulary

Terms this change introduces or makes precise; `doc-keeper` adds them to `docs/glossary.md` in the task that first uses them.

| Term | Code | Meaning |
| --- | --- | --- |
| **Suivi du batch** | `follow-up`, `Pass.Follow` | Asking Anthropic the state of a user's batch in progress and, once it ended, reading and applying its results. At start-up, every 15 minutes, and before the guards of the nightly pass. |
| **Résultat** | `result`, `analysis.Result` | One line of a batch's results file: the outcome of one request (`succeeded`, `errored`, `expired`, `canceled`), with its `custom_id`. |
| **Ligne illisible** | `unreadable line` | A line of the results file that is not a JSON result or exceeds 4 MiB: skipped and counted; its item has no result. |
| **Résultats indisponibles** | `batches.results_unavailable_at` | Anthropic no longer serves the results of the batch (not found, archived): the batch is ended with the tokens already read. |
| **Nom normalisé** | `categories.name_key` | The name of a category lower-cased, without accent, inner spaces collapsed, edges trimmed (RG-54, decision 6). |
| **Code d'échec** | `last_error`, `AnalysisError` | Why the last attempt failed: `invalid_response`, `request_errored` or `too_long`. Never a message. |
| **Texte de recherche** | `search_text` | The fiche flattened to text for the search (ADR-0007), rule in D11. |

## Packages, contracts and artefacts touched

| Where | Change |
| --- | --- |
| `api/openapi.yaml` | new schema `AnalysisError` (enum `invalid_response`, `request_errored`, `too_long`); `Item.last_error` becomes `anyOf [AnalysisError, null]`; `make generate` (Go server, TS client) |
| `internal/store` | migration `00007_results.sql` (D9); `name_key` backfill at open; new `results.go` (batches to follow, item outcomes, batch closing), `tags.go` (référentiel tags, tag reuse), `categories.go` (proposals, `name_key` on every insert, seeding included), `content_types.go` (proposed types); `items.go` reads type key, category, tag ids, fiche JSON |
| `internal/store/storetest` | helpers for states no store function writes yet: a field listed in `user_edited`, a deleted tag, a tag attached to an item, a batch's `envelope_version` |
| `internal/naming` (new, leaf) | `Tag(s)` and `CategoryKey(name)`, the two normalisations (D7); imports `golang.org/x/text/unicode/norm` only |
| `internal/analysis` | `Analyzer` extended (D2); `follow.go` (follow-up, D1); `results.go` (outcome of each line, validation, D5–D6); `searchtext.go` (D11); `cost.go` (D12); référentiel `<tags>` (D10) |
| `internal/analysis/schema` | the compiled validator of the bundled schema and the fiche schema versions by template, built once at start-up |
| `internal/analysis/anthropic` | `GetBatch`, `Results` and the JSONL reader (D2, D3) |
| `internal/analysis/analysistest` | fake batch states and scripted results files (lines, cut downloads, not found, archived) |
| `internal/config` | `ANALYSIS_INPUT_PRICE_USD_PER_MTOK`, `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK` (D12) |
| `internal/httpapi` | `itemFromStore` fills `content_type`, `category_id`, `tag_ids`, `fiche`, `last_error` (D13) |
| `cmd/hippo` | follow-up after the start-up reconciliation and every 15 minutes (D1); start-up log of the model and prices |
| `backend/.golangci.yml` | depguard `text-normalisation-only-in-naming`: `golang.org/x/text` only in `internal/naming` |
| `infra/.env.example` | the two price variables |
| app | none (generated TS types only) |

- **API contract**: yes, `AnalysisError` and `Item.last_error` (above). No operation added or removed. The two implemented operations returning an `Item` now fill the members the contract already declares.
- **Migration**: yes, one new file `00007_results.sql`; no existing migration is edited.
- **Prompt and fiche schemas**: unchanged (no prompt text, no envelope, no fiche schema change, no version change). The validation order of `docs/prompt/README.md` changes (tags normalised before the schema check, decision 2) through `prompt-engineer`; no `/eval-prompt` run is needed since nothing sent to the model changes, except the `<tags>` block now filled exactly as `system-prompt.md` already describes it.

## Decisions

### D1. Follow-up inside the pass

- `Pass.Follow(ctx)` follows every user's batches; `Pass.followUser(ctx, userID)` does one user. Both hold the pass mutex, like `Run` and `Reconcile`: a follow-up never runs while a nightly send or a reconciliation is in flight, and vice versa.
- **Order in `runUser`**: reconcile `pending` batches → `followUser` → guard 2 (`HasBatchWithoutCost`) → guard 3 → selection and send. A batch ending at 01:56 is applied before the 02:00 guard (spec « A batch that ended before the pass is applied before the guards », RG-30).
- **Start-up and ticks**: `cmd/hippo` runs, in the background goroutine that already reconciles, `Reconcile` then `Follow`, then `analysis.RunFollowUp(ctx, pass, clock, FollowInterval, logger)`: a loop on `schedule.Clock.After(15 minutes)` calling `pass.Follow`. A tick that fires while the pass is busy waits on the mutex; ticks are not queued (the next wait starts when `Follow` returns). The fake clock of `schedule/scheduletest` drives the tests; no sleep.
- **No key**: no `Analyzer` → `Follow` returns at once, nothing logged per tick (the nightly pass already logs `no_api_key`).
- **What is followed**: `store.BatchesToFollow(ctx, userID)` returns the user's batches with `status = 'in_progress'` (they all have an `anthropic_batch_id`), oldest first, plus any `ended` batch with `cost_usd IS NULL` (no writer produces it any more, but `storetest` and an older binary could). Nothing to follow → no call (spec « Nothing to follow makes no call »).
- **One batch**: `Analyzer.GetBatch` (bounded to 1 minute, SDK retries included): `in_progress` or `canceling` → nothing; `ended` → read and apply (D3–D8) within a 10-minute bound; not found or archived → closed as results unavailable (D8). Any other error → `poll_failed` (status and error type only), nothing written, retried at the next occasion.
- Rejected: a separate goroutine with its own lock (the 02:00 pass could see an ended batch mid-application and skip the night, adversarial pass I1); following inside `schedule.Run` only (a batch would wait a whole day).

### D2. `Analyzer` extended

```go
// internal/analysis
type Analyzer interface {
    CreateBatch(ctx context.Context, body io.ReadSeeker, size int64) (BatchRef, error)
    ListBatches(ctx context.Context, since time.Time) ([]BatchInfo, error)
    // GetBatch returns the processing state of a batch, ErrResultsUnavailable when the service
    // does not know it (404) or has archived it.
    GetBatch(ctx context.Context, anthropicID string) (BatchState, error)
    // Results opens the results file of an ended batch, ErrResultsUnavailable on a 404.
    Results(ctx context.Context, anthropicID string) (ResultReader, error)
}

type BatchState struct {
    Processing string    // "in_progress", "canceling", "ended"
    EndedAt    time.Time // zero until ended
}

type ResultReader interface {
    Next() (Result, error) // io.EOF at the end; ErrUnreadableLine for a skipped line (keep reading)
    Close() error
}

type Result struct {
    CustomID   string
    Kind       string     // "succeeded", "errored", "canceled", "expired"
    StopReason string     // succeeded only
    ToolCalls  []ToolCall // the tool_use blocks, in order; text blocks are not kept
    Usage      Usage      // succeeded only
    ErrorType  string     // errored only: the API error type, never its message
}
type ToolCall struct{ Name string; Input json.RawMessage }
type Usage struct{ Input, Output, CacheWrite5m, CacheWrite1h, CacheRead int64 }

var ErrResultsUnavailable = errors.New("batch results no longer served")
var ErrUnreadableLine = errors.New("unreadable result line")
```

- The analysis package never sees an SDK type: `Result` is Hippo's, built by the client from the line.
- **`GetBatch`**: `MessageBatchService.Get` (`SDK/messagebatch.go:86`), at v2.7.0 of `github.com/anthropics/anthropic-sdk-go/v2` (`backend/go.mod`; `SDK/` = the module root in the module cache). `processing_status` is one of `in_progress`, `canceling`, `ended` (`SDK/messagebatch.go:336-338`); `archived_at` is « the time at which the Message Batch was archived and its results became unavailable » (`SDK/messagebatch.go:270-273`): non-zero → `ErrResultsUnavailable`. A `*anthropic.Error` with `StatusCode` 404 (`SDK/internal/apierror/apierror.go:24`) → `ErrResultsUnavailable`; any other error is reduced to `*QueryError{Status, Type}` or the context's error, never wrapped (its `Error()` prints the URL and the raw answer, `apierror.go:72-86`, as E3-01 D9 found). The SDK's default retries apply (a read).
- **`Results`**: `Client.Get(ctx, "v1/messages/batches/<id>/results", nil, &raw, option.WithHeader("Accept", "application/x-jsonl"))` (`SDK/client.go:299`), the same path and header as `ResultsStreaming` (`SDK/messagebatch.go:224-241`), but with a `**http.Response` destination: the SDK hands the response over unread (`SDK/internal/requestconfig/requestconfig.go:541`, `:663`) and still turns a status ≥ 400 into an `*Error` (`:537-539`). Hippo reads the body itself (D3) instead of `jsonl.Stream`, which stops at the first line it cannot decode (`SDK/packages/jsonl/jsonl.go:37-52`).
- Rejected: `ResultsStreaming` (one bad line blocks the batch, adversarial pass B1); the SDK's `MessageBatchIndividualResponse` as the line type (a decode error of any nested union fails the line; Hippo needs six fields).

### D3. Results reader (`internal/analysis/anthropic`)

- `bufio.Reader` of 64 KiB over the body; a line is read by `ReadSlice` pieces appended to a buffer up to `MaxResultLine` = 4 MiB (4 194 304 bytes). Beyond it, the rest of the line is discarded up to `\n` and `Next` returns `ErrUnreadableLine`. An output of 8 000 tokens is about 32 KB, at most a few hundred KB with JSON escapes: 4 MiB is far above any valid line and bounds memory (one line and its decoded form at a time).
- The line is decoded with `encoding/json` into a private struct holding only: `custom_id`; `result.type`; `result.message.stop_reason`, `result.message.content[]` (`type`, `name`, `input` as `json.RawMessage`), `result.message.usage` (`input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation.ephemeral_5m_input_tokens`, `cache_creation.ephemeral_1h_input_tokens`); `result.error.error.type`. Field names from the SDK at v2.7.0: `SDK/messagebatch.go:393-404` (line), `:464-470` (result union), `:357-359` (errored), `SDK/message.go:8976` (content), `:9012` (stop reason), `:9037` (usage), `:16427-16439` and `:3937-3941` (usage and its cache breakdown), `:15933-15939` (`tool_use` input as raw JSON), `SDK/shared/shared.go:1048-1053`, `:1158-1161` (error type).
- Not JSON, no `custom_id`, or a `result.type` outside the four → `ErrUnreadableLine`. A `cache_creation` breakdown absent while `cache_creation_input_tokens` > 0 counts the total as 5-minute writes (the request sets the default 5-minute `cache_control`, E3-01 D4).
- A transport error or a context end mid-body → `Next` returns it (not `ErrUnreadableLine`): the read is cut (D8).
- Tests against an `httptest.Server`: headers, a 404, a line of 4 MiB + 1 then a valid line, a non-JSON line, a cut body, an unknown result type, a missing cache breakdown.

### D4. Outcome of each line

The analysis package reads the lines and, for each, decides an outcome, then calls one store transaction. Tokens of every `succeeded` line are summed in memory whatever its outcome (spec « Every succeeded line counts in the tokens »).

| Line | Outcome | Item afterwards |
| --- | --- | --- |
| `succeeded`, batch envelope version ≠ embedded | `requeue` | `extracted`, batch NULL, attempts and `last_error` unchanged |
| `succeeded`, `stop_reason = max_tokens`, not yet retried | `cut` | `extracted`, batch NULL, `incomplete_retried = 1`, attempts unchanged |
| `succeeded`, `max_tokens`, already retried | `too_long` | `failed`, `last_error = too_long`, attempts unchanged |
| `succeeded`, any other stop reason, or validation fails (D5) | `invalid` | attempts + 1, `last_error = invalid_response`; `extracted` with batch NULL, or `failed` when attempts reaches 3 |
| `succeeded`, valid | `analyzed` (D6) | `analyzed`, counters reset |
| `errored` | `errored` | attempts + 1, `last_error = request_errored`; `extracted` or `failed` at 3 |
| `expired`, `canceled` | `requeue` | as the first row |
| unreadable line, or no line for the item | settled by the batch closing (D8) | as the first row |

- Every outcome is written by `store.SettleItem(ctx, userID, batchID, itemID, outcome)` (a valid response: `store.WriteAnalysis`, D6), in one `BEGIN IMMEDIATE` transaction that re-reads the item `WHERE user_id = ? AND id = ?` and writes only when `status = 'submitted' AND batch_id = ?` and `deleted_at IS NULL`; otherwise it writes nothing and answers `not_submitted` (counted as `unknown`) or `deleted` (counted as `deleted`, RG-79). Each write advances `updated_at` and `change_seq` (`nextChangeSeq`). This check is the idempotence key: a line applied before a stop, or repeated, finds the item no longer `submitted` in that batch.
- The attempt threshold is `attempts + 1 >= 3` → `failed` (RG-35). Attempts are never reset by a `requeue`.
- A `failed` item keeps `batch_id` (traceability of the last attempt); an `extracted` one has `batch_id` NULL, as `FailBatch` does in E3-01.

### D5. Validation (`internal/analysis`, steps 1–5 outside the transaction)

In this order, the first failure giving `invalid` with a reason class for the logs (spec « A response is validated in a fixed order and rejected whole »):

1. `stop_reason == "tool_use"` (`stop_reason`).
2. Exactly one tool call, named `classify_item`, whatever the text blocks (`tool_calls`).
3. The input is a JSON object (decoded with `UseNumber`); if its `tags` member is an array of strings, each is normalised by `naming.Tag` (D7), empty results dropped, duplicates dropped keeping the first; fewer than 2 left → `too_few_tags`. Any other shape of `tags` is left for the schema.
4. The object, tags replaced, is validated against the bundled schema compiled once at start-up (`jsonschema.NewCompiler`, `DefaultDraft(Draft2020)`, `AddResource`, `Compile`: `santhosh-tekuri/jsonschema/v6@v6.0.3/compiler.go:43,121,178`; `Schema.Validate`, `validator.go:15`; `pattern` is Go `regexp`, `compiler.go:331`), as `cmd/schemabundle` does (`schema`). The validator's error text is never logged nor stored: it quotes instance values.
5. The typed fields are decoded (`type`, `category`, `tags`, `title`, `fiche` kept as `json.RawMessage` and compacted, `language`, `freshness`, `content_ok`, `content_issue`); the fiche schema version is read from the bundle at `$defs.fiche_<template>.x-schema-version`, mapped once at start-up; the search text is computed (D11).

Steps that need the database run inside the write transaction (D6): the template against the type, the type and the category.

- The envelope version check (D4, first row) runs before step 1, once per batch: `batches.envelope_version` against the embedded one.
- `docs/prompt/README.md` « Validation d'une réponse » is rewritten in that order by `prompt-engineer` (task 4.9).

### D6. Writing a valid response (`store.WriteAnalysis`, one transaction)

Inside the `BEGIN IMMEDIATE` transaction of D4, after the `submitted` check, in this order; a failure of a check is `invalid` (attempt counted in the same transaction, nothing else written):

1. **Type** (RG-57, decision 3). `{"key": k}`: a non-deleted `content_types` row with key `k`, else `unknown_type`. `{"proposed": {key, name, reason}}`: a non-deleted row with that key is reused as it is (decision 3, no field changed, RG-56); a row with that key marked deleted cannot exist before E4 merges types: the store refuses with `unknown_type` (see Open Questions); otherwise a row is inserted `proposed`, `has_template = 0`, `proposal_reason`, `updated_at`, its own `change_seq`. Shared by all users (D15).
2. **Template** (README step « template »). Existing type chosen by key: `has_template = 1` → `fiche.template == key`; `has_template = 0` → `generic`. Proposed type, new or reused: `generic` (decision 3). Else `template`.
3. **Category**. `{"id": n}`: a non-deleted category of this user (`WHERE id = ? AND user_id = ? AND deleted_at IS NULL`), else `unknown_category`. `{"proposed": {...}}`: parent corrected first (RG-55): `parent_id` null → root; a non-deleted category of this user that is a root → kept; a non-deleted sub-category of this user → its root if that root is not deleted, else root; anything else (unknown, deleted, another user's) → root. Then the match (RG-54, decision 6): `name_key = naming.CategoryKey(name)` among the user's non-deleted categories with the same `COALESCE(parent_id, 0)`; a match is reused unchanged (RG-56); otherwise inserted `proposed` with `name`, `name_key`, `description`, `proposal_reason`, `color` NULL (inherited at display, RG-59), `created_at`, `updated_at`, its own `change_seq`. The depth check (RG-51) holds by construction: a parent is always a root.
4. **Tags** (RG-58). For each normalised name: the user's non-deleted tag with that `name`, else a new row with its own `change_seq`.
5. **Corrections by hand** (RG-61). `user_edited` read in the transaction: `content_type` listed → steps 1 and 2 still validate, but nothing is created and `type_id` is kept; `category` listed → step 3 validates an id, creates no proposal and keeps `category_id`; `tags` listed → step 4 creates nothing and the item's tags are kept. Validation is never skipped: a response invalid in a corrected field is still invalid (RG-35).
6. **Item**: `status = 'analyzed'`, `type_id`, `category_id`, `title`, `fiche_json`, `search_text`, `language`, `freshness`, `content_ok`, `content_issue`, `model`, `prompt_version`, `envelope_version` from the batch row (proposal, « The batch row is the reference »), `schema_version`, `analyzed_at`, `updated_at`, `attempts = 0`, `last_error = NULL`, `incomplete_retried = 0`, `batch_id` kept, a new `change_seq`; `item_tags` of the item replaced by the new set (`user_id` in every row).

Proposals of two items of one batch meet in the database: the transactions are sequential, the second finds the first's row (specs « Two items of a batch proposing the same category get one », « … the same type get one », « A new tag of two items of a batch is created once »). The unique indexes of D9 make a duplicate impossible even if two writers raced.

### D7. Normalisations (`internal/naming`, decisions 2 and 6)

- `CategoryKey(name)`: `norm.NFD`, drop `unicode.Mn`, `strings.ToLower`, collapse each run of `unicode.IsSpace` into one space, trim. « Ménage » → `menage`; « Java  &  Spring » → `java & spring`; « Java et Spring » → `java et spring`.
- `Tag(s)`: `norm.NFD`, drop `unicode.Mn`, `strings.ToLower`; each run of spaces, `_` and `-` → one `-`; `-` trimmed at both ends; cut to 40 runes; trailing `-` trimmed again. Any other character is kept, so that the schema's `pattern` rejects it (`c++` → invalid response, spec « A tag still outside the allowed form makes the response invalid »).
- `norm.NFD` and `Form.String` from `golang.org/x/text@v0.43.0/unicode/norm/normalize.go:39,59`; `go get golang.org/x/text@v0.43.0` makes the indirect requirement direct (decision 7), no new module. `unicode.Mn` is the standard library's.
- The collapse of separator runs and the edge trimming make RG-58's « espaces et `_` remplacés par des tirets » precise; the wording is in « Decisions to confirm with Ghislain ».
- Table tests on both functions, including `ÉCOLE`, `œuvre` (no decomposition: kept, then refused by the pattern), `Ǆ`, a 45-rune tag, `--a__b  c--`.

### D8. Reading a batch and closing it

1. `Results(anthropicID)`; `ErrResultsUnavailable` → close as unavailable (step 4).
2. For each line: `ErrUnreadableLine` → count, continue; a result → D4; `io.EOF` → step 3; any other error (cut body, context) → **cut read**: write the token sums read so far with the line count into the batch row when this read went further than the recorded one (`results_lines_read`, one `UPDATE … WHERE results_lines_read IS NULL OR results_lines_read < ?`), log `read_failed`, stop; the next occasion reads again from the start.
3. **Close** (`store.CloseBatch`), one `BEGIN IMMEDIATE` transaction filtered by `user_id`, `WHERE status IN ('in_progress','ended') AND cost_usd IS NULL`: every item still `submitted` with this `batch_id` and not deleted goes back to `extracted`, `batch_id` NULL, attempts unchanged, new `updated_at` and `change_seq` (spec « An item without result goes back without attempt »); the token sums of this full read and `cost_usd` (D12) are written; `status = 'ended'`, `ended_at` = Anthropic's `ended_at` (or now).
4. **Close as unavailable**: the same transaction, with the tokens already recorded by the most complete cut read (zeros if none, decision 1), their cost, and `results_unavailable_at` = now. Logged `results_unavailable`.

- A batch closed by another run in the meantime (zero rows updated) is logged `not_in_progress` and nothing else changes.
- Idempotence: a full read after a cut read re-applies nothing (D4 key) and writes the full sums, never the partial ones; the cost is written once (`cost_usd IS NULL`).

### D9. Migration `00007_results.sql`

```sql
-- +goose Up
-- E3-02 to E3-05 (change apply-batch-results).
ALTER TABLE items ADD COLUMN envelope_version INTEGER;          -- RG-64, from the batch row

ALTER TABLE categories ADD COLUMN name_key TEXT;                -- RG-54, written by Go only
CREATE UNIQUE INDEX categories_user_parent_name
    ON categories (user_id, COALESCE(parent_id, 0), name_key)
    WHERE deleted_at IS NULL AND name_key IS NOT NULL;

ALTER TABLE batches ADD COLUMN results_unavailable_at TEXT;     -- decision 1
ALTER TABLE batches ADD COLUMN results_lines_read INTEGER;      -- most complete cut read

CREATE TABLE tags (
    id         INTEGER PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    change_seq INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX tags_id_user ON tags (id, user_id);
CREATE UNIQUE INDEX tags_user_name ON tags (user_id, name) WHERE deleted_at IS NULL;
CREATE INDEX tags_user_change_seq ON tags (user_id, change_seq);

CREATE TABLE item_tags (
    item_id TEXT    NOT NULL,
    tag_id  INTEGER NOT NULL,
    user_id TEXT    NOT NULL,
    PRIMARY KEY (item_id, tag_id),
    FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id),
    FOREIGN KEY (tag_id, user_id) REFERENCES tags (id, user_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX item_tags_tag ON item_tags (tag_id);
```

- `items (id, user_id)` already has the unique index `items_id_user` (`00004_assets.sql:8`); `tags_id_user` gives the second composite key: an item only carries tags of its own user (RG-90), enforced by SQLite.
- `COALESCE(parent_id, 0)`: SQLite treats NULLs as distinct in a unique index, so two roots with the same name would pass without it; an index on an expression and a partial index are both supported (https://sqlite.org/expridx.html, https://sqlite.org/partialindex.html; SQLite as embedded by `modernc.org/sqlite v1.61.0`). `deleted_at IS NULL`: a deleted category or a merged tag (RG-72, RG-74) never forbids a new one of the same name.
- **No new batch status**: changing the `CHECK` of `batches.status` means rebuilding the table, which `items.batch_id` references, inside goose's transaction where `PRAGMA foreign_keys` cannot change; an unavailable batch is `ended` with `results_unavailable_at`. Rejected: a `closed` status (rebuild); a separate table of closings (one nullable column suffices).
- **`name_key` backfill**: `lower()` of SQLite folds ASCII only and removes no accent, and a migration is SQL only (ADR-0032). `store.Open`, after the migrations, fills `name_key` with `naming.CategoryKey(name)` for every row where it is NULL, in one transaction, without advancing `change_seq` (`name_key` is not synced); count logged. A collision (two existing categories of one user with the same key under the same parent) stops the start-up with an error naming the two ids: impossible in V1, where only the seeding wrote categories. From then on, every insert of a category (seeding included) writes `name_key`.
- `tags.name` holds the normalised form only; the 40-character `CHECK` is a guard behind `naming.Tag`.
- Migration test: a version-6 base with a user, seeded categories, items and a batch, migrated: the new columns are NULL, the tables are empty, the unique indexes refuse a duplicate root name once `name_key` is filled, accept it when one is deleted, and the backfill fills the 24 seeded rows.

### D10. Tags of the référentiel

`store.ReferentielTags(ctx, userID, 300)`:

```sql
SELECT t.name FROM tags t
  JOIN item_tags it ON it.tag_id = t.id AND it.user_id = t.user_id
  JOIN items i ON i.id = it.item_id AND i.user_id = it.user_id
 WHERE t.user_id = ? AND t.deleted_at IS NULL AND i.deleted_at IS NULL AND i.is_private = 0
 GROUP BY t.id ORDER BY COUNT(*) DESC, t.name ASC LIMIT 300
```

`Referentiel` writes `<tags>\n` + names joined by `, ` + `\n</tags>` when there are any, and `<tags>\n</tags>` otherwise, so the existing format stays (`system-prompt.md`, « séparés par des virgules »). Built once per batch with the rest of the référentiel: every request of a batch stays byte-identical (prompt cache). Names are ASCII by construction, so the name order is the byte order.

### D11. Search text (ADR-0007)

The stored fiche is walked with a `json.Decoder` token stream in document order; every string value is emitted, one per line, except the value of the root member `template` and of any member named `language` inside a nested object (a code block's language). Numbers, booleans and null are skipped; keys are never emitted. Code is kept (a command must be findable, RG-41). Rejected: a per-template field list (eight lists to keep in step with the schemas); including `template` (every fiche of a type would match its key).

### D12. Cost and prices

- `config.Prices{InputPerMTok, OutputPerMTok float64}` from `ANALYSIS_INPUT_PRICE_USD_PER_MTOK` and `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK`, defaults 1 and 5 (Haiku 4.5 standard prices, checked on 2026-11-06 at https://platform.claude.com/docs/en/about-claude/pricing according to the proposal); parsed like `MONTHLY_BUDGET_USD`, refusing anything not finite and > 0 with the variable's name. A price of 0 would make every batch free and the budget unreachable.
- `analysis.BatchCost(u Usage, p Prices) float64` = `0.5 × ((Input + 1.25 × CacheWrite5m + 2 × CacheWrite1h + 0.1 × CacheRead) × p.InputPerMTok + Output × p.OutputPerMTok) / 1e6`. Batch discount and cache ratios are constants in code (decided with Ghislain on 2026-11-06); that they stack is stated on the same pricing page (unverified by this design at writing; task 1.3 re-reads the page and records the date).
- `batches.cache_creation_input_tokens` stores the sum of both cache-write durations; the cost is computed from the split before it is summed.
- Start-up log: `analysis prices` with `model`, `input_usd_per_mtok`, `output_usd_per_mtok`.
- `errored`, `expired` and `canceled` lines carry no usage and add nothing; that they are not billed is unverified (Anthropic's batch-processing guide, https://platform.claude.com/docs/en/build-with-claude/batch-processing).

### D13. Contract and items served

- `api/openapi.yaml`: `AnalysisError: {type: string, enum: [invalid_response, request_errored, too_long], description: …}`; `Item.last_error: anyOf [$ref AnalysisError, {type: "null"}]`, description updated. `make generate`; the app's generated types change, no app code reads them yet.
- `store.Item` gains `TypeKey *string`, `CategoryID *int64`, `TagIDs []int64` (never nil, non-deleted tags, ascending id), `FicheJSON json.RawMessage`, `EnvelopeVersion *int64`; `itemByID` and its siblings read them (a `LEFT JOIN content_types`, a second query on `item_tags` like `withAssets`).
- `itemFromStore` maps them; the fiche is decoded into the generated `Fiche` union (`api/fiches.gen.json`, as `httpapi/fiche_test.go` does); a stored fiche that does not decode is an internal error (`500`, cause logged by class), since every stored fiche was validated against the same schemas. `last_error` maps to `AnalysisError`; any other stored value is an internal error too (none exists: extraction never writes it).
- Tests: the item-editing and item-capture scenarios of the deltas; the existing contract tests (`operations_test.go`) stay green.

### D14. Logs

- One line per followed batch (`analysis follow-up`): `user_id`, `batch_id`, `anthropic_batch_id`, `outcome` (`in_progress`, `ended`, `results_unavailable`, `poll_failed`, `read_failed`, `not_in_progress`), counts `analyzed`, `invalid`, `cut`, `too_long`, `errored`, `requeued`, `failed`, `deleted`, `unknown`, `unreadable_lines`, `without_result`, the four token sums, `cost_usd`, `duration_ms`; for `poll_failed` and `read_failed`, `status` and `error_type` only.
- One line per item at debug level: `item_id`, `outcome`, `reason` (the class of D5–D6) or `error_type` for `errored`.
- Never: a response, a fiche, a title, a tag or category name, a validator message, an API error message (docs/dev/conventions-backend.md « Logs »). Test: spec « Result logs carry no content », JSON logs captured with distinctive values.

### D15. Isolation, private items, budget

- **RG-90**: every new store function takes `userID` and filters every statement by it; a line's `custom_id` is applied only to an item `WHERE user_id = ? AND batch_id = ? AND status = 'submitted'` (spec « A result for another user's item is ignored »); proposals and tags are created for the item's user; `item_tags` carries `user_id` with both composite keys (D9). `content_types` stays the only table without `user_id` (ADR-0022).
- **V1 limit, shared proposed types (decision 4)**: a type proposed from one user's item, with its key, name and reason, is visible to every user and can be accepted or merged by any of them (RG-72, E4). Accepted for V1 (one user); a new ADR amending ADR-0022 is required before a second user is created. Written in `docs/spec-v1.md` with the `content_types` table.
- **RG-12**: the follow-up builds no request: `Follow` uses `GetBatch` and `Results` only (test: a follow-up never calls `CreateBatch`, fake counts). The référentiel lists only tags carried by a non-private, non-deleted item (D10, decision 5); categories proposed from an item made private after the send stay in the tree, as any category of the user (RG-52).
- **RG-14**: `SetPrivate` never touches `status`; a `submitted` item checked Privé is settled like any other (spec « An item made private after its batch left gets its result »).
- **RG-81**: the follow-up sends nothing; the cost it writes is what guard 3 sums. Guard order unchanged (spec « Sends are guarded in a fixed order », modified only to say the follow-up runs first).

### D16. Configuration and infrastructure

- `infra/.env.example`: the two price variables, commented (« standard prices of `ANALYSIS_MODEL` per million tokens; update them with the model »).
- No change to the compose files or the image.

## Risks / Trade-offs

- [An `errored` request counts an attempt even during an Anthropic outage (decision of 2026-11-06)] → three nights of outage fail the item; « Relancer » (E3-07) brings it back; `error_type` is logged per item to tell an outage from a bad request.
- [A batch failed by an ambiguous reconciliation may have been processed and billed] → never read, its cost never counted: the budget can be under-counted by one night; logged `possible_double_send` since E3-01; at most one per such row.
- [A batch whose results are no longer served] → cost of the tokens already read, often 0: the budget can be under-counted by one night (decision 1).
- [A fiche schema changed without an envelope version change while a batch is in flight] → the response is validated against the new fiche schema and may be rejected with an attempt; the deployment note in `spec-v1.md` says to deploy a schema change when no batch is in progress (the follow-up log says it).
- [A tag the model writes with a character outside `[a-z0-9-]` after normalisation (`c++`, `c#`, `cœur`)] → the whole response is rejected with an attempt (decision 2 keeps the schema); the prompt already asks for that form; watched in the first nights' logs (`reason=schema`).
- [The key revoked or wrong (401)] → `poll_failed` every 15 minutes, the batch blocks until the key is fixed; visible in logs only until E3-08's Réglages alert.
- [A line above 4 MiB] → treated as unreadable, its item back to `extracted` without attempt, and sent again the next night: a loop if it repeats; implausible with an 8 000-token cap.
- [Two composite foreign keys on `item_tags`] → a tag of another user cannot be attached even by a bug; cost: one more unique index.

## Migration Plan

1. Deploy: migration 00007 runs at start-up; `store.Open` fills `name_key` for the seeded categories (24 rows per user, logged); the follow-up finds nothing to follow (no key on the NAS yet).
2. With Ghislain's agreement, the paid end-to-end test below, before the key goes on the NAS.
3. Ghislain puts `ANTHROPIC_API_KEY` (and, if Haiku's prices changed, the two price variables) in `infra/.env` and restarts; the first night sends, the follow-up applies within 15 minutes of the batch's end.
4. Rollback: restore the backup taken before the update (ADR-0020); no down migration. An older binary ignores the new columns and tables, but would leave `ended` batches as E3-01 knows them.

## Paid end-to-end test (proposed, not run)

The results reader is tested only against recorded fixtures. Before the key goes on the NAS, an optional manual run, **only with Ghislain's agreement** (cost under 0.01 $):

- a throwaway `DATA_DIR` on the Mac, `make dev` with a dedicated test key and `BATCH_SCHEDULE` set to the next minute;
- three fictitious notes captured through the API (invented texts: a recipe, a shell tip, a meetup), no personal content;
- wait for the follow-up to log `ended`; check the items are `analyzed` with plausible fields, the batch's tokens and cost, and compare the cost with the Anthropic console;
- delete the directory. Nothing is committed; the observed result lines, stripped of their text, may become a fixture.

## Decisions to confirm with Ghislain

All approved on the substance (proposal, decisions 1–8); the exact French wording for `docs/metier.md` is below. **Wording and the points marked (new) approved by Ghislain on 2026-11-07**, as was the open question's interim answer (`unknown_type` until E4).

1. **RG-13**: « Un élément privé n'a pas de fiche générée et apparaît dans **À trier** jusqu'à ce que l'utilisateur lui donne une catégorie. Exception : un élément coché Privé après le départ de son batch garde le résultat de ce batch, fiche et classement compris (RG-14). *(Précisée le 7 novembre 2026 avec Ghislain.)* »
2. **RG-35**: « Une réponse de l'IA qui ne respecte pas le schéma attendu est rejetée entièrement : rien n'est écrit, l'élément compte une tentative. Une requête que le service signale en erreur compte aussi une tentative. Une requête que le service n'a pas traitée (expirée, annulée), un élément resté sans résultat lisible, ou dont le batch n'est plus disponible, revient dans la file sans compter de tentative. Après 3 tentatives, il passe en `failed` et apparaît dans À trier et dans Réglages avec un bouton « Relancer ». *(Précisée le 7 novembre 2026 avec Ghislain.)* » **(new)**: « un élément resté sans résultat lisible » (unreadable line or missing line) extends decision 1 to a single line; and an item of a batch sent with another envelope version also comes back without attempt (proposal, « The batch row is the reference »), which this wording covers only as technical detail in `spec-v1.md`.
3. **RG-52** (decision 5): « … la liste des types et les tags existants portés par au moins un élément non privé et non supprimé. … »
4. **RG-54** (decision 6): « Avant de créer une proposition, le backend corrige son parent (RG-55), puis vérifie qu'aucune catégorie (active ou proposée, non supprimée) n'a déjà le même nom normalisé sous ce parent ; si c'est le cas, il réutilise l'existante. Le nom normalisé est le nom en minuscules, sans accent, les espaces intérieurs réduits à un seul, ceux des bords retirés : « Ménage » et « menage » sont le même nom, « Java & Spring » et « Java et Spring » non. *(Précisée le 7 novembre 2026 avec Ghislain.)* »
5. **RG-57** (decision 3): add « Un type proposé dont la clé existe déjà est rangé sous ce type, avec sa fiche générique, comme après une fusion (RG-72). »
6. **RG-58** (decision 2): « **Tags** : 2 à 5 par élément, réutilisés en priorité. Avant de vérifier la réponse, le backend normalise tout tag reçu (minuscules, sans accent, espaces et `_` remplacés par des tirets, 40 caractères maximum) et ignore les doublons. S'il reste moins de 2 tags distincts, la réponse est rejetée (RG-35). *(Précisée le 7 novembre 2026 avec Ghislain.)* » **(new)**: a run of spaces, `_` or `-` gives a single dash and no dash is left at either end; a tag that still holds another character than unaccented lower-case letters, digits and dashes (`c++`, `cœur`) makes the response invalid, as the schema already says.
7. **RG-80** (decision 1): add « Un batch dont les résultats ne sont plus disponibles enregistre les tokens déjà lus, zéro sinon. »

## Open Questions

- A proposed type whose key is held only by a deleted (merged) type: impossible until E4 lets a type be merged (RG-72); the store answers `unknown_type` (invalid response) meanwhile. E4's merge change decides whether such a key maps to the merge target; no spec or task here depends on it.
