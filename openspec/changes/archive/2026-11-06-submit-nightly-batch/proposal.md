# Proposal

Backlog story: **E3-01 — Build and send the nightly batch** (opens epic E3, analysis).

Rules: RG-12, RG-14, RG-30, RG-31, RG-32, RG-37, RG-81, RG-90, RG-92 · ADRs: ADR-0005, ADR-0006, ADR-0019, ADR-0021, ADR-0022, ADR-0023, ADR-0035.

## Why

Every captured item now stops at `extracted`: nothing sends it to the AI, so no item gets a fiche, a type, a category or tags. This change starts the analysis pipeline. Each night it sends the waiting items to Anthropic in a single Message Batches batch. The results are read back by E3-02.

## What Changes

- **Nightly schedule, at Paris time.**
  - `BATCH_SCHEDULE` accepts only a daily expression, `M H * * *`. It is read in Europe/Paris through `LoadParis`, like the other dates of the domain (RG-04, RG-26); the `TZ` variable is not used.
  - The pass runs once per Paris date, so a daylight-saving change never skips or doubles a night (RG-30).
  - An invalid expression stops the start-up. `infra/.env.example`, which claims that the schedules follow `TZ`, is corrected.
- **Selection.**
  - Per user, the pass takes the `extracted` items that are neither private nor deleted, oldest first.
  - It stops at 200 items (RG-32) or when the batch body would exceed 200 MB, a margin under the 256 MB limit of the API. The surplus waits for the next night, oldest first, like the surplus over 200.
  - With nothing to send, no call is made (RG-31).
- **Send guards** (RG-81, ADR-0021), checked in this order:
  - no send without `ANTHROPIC_API_KEY`;
  - no new batch while the user's previous batch has no recorded cost. Costs are recorded by E3-02, so E3-01 shipped alone sends at most one batch;
  - no send when the month's recorded spend (Paris calendar month, per user, RG-90) reaches `MONTHLY_BUDGET_USD`.

  Each refusal is logged. A batch may end above the budget, since RG-81 stops the next sends, not the one under way. E3-08 keeps the immediate-analysis side, `analysis_calls` and the alert in Réglages.
- **One request per item**, as described in `docs/prompt/README.md`:
  - prefix: the forced `classify_item` tool, then the fixed part of the prompt, then the référentiel (types and categories in a stable order, with an empty tags block), then the cache point. The référentiel is built once per batch;
  - `max_tokens` 4000, or 8000 when `incomplete_retried` is set, so that E3-05 has nothing to rewire;
  - `custom_id` = item id;
  - one `<element>` message for each form: link, note, image. Absent fields are left out.
- **Image copy sent to the AI** (decided with Ghislain on 2026-11-06). The file on the NAS is never changed.
  - The image is decoded, turned upright from its EXIF orientation (JPEG) or its `irot`/`imir` boxes (HEIC), scaled down to 1 568 px on its long side and re-encoded as JPEG. This drops the metadata (GPS position, device).
  - HEIC is decoded with `gen2brain/h265`.
  - Decoding runs one image at a time, in a child process with its own memory limit and a timeout. An image above a pixel ceiling, one that cannot be decoded, or one whose child fails is sent without the image (title and capture note), so the fiche says « contenu partiel » (RG-37).
  - The child process is moved out of `internal/extraction` into a shared package.
- **Memory.** The request body is written to a temporary file and streamed to the API, so the backend never holds a whole batch in memory. The image child needs far more than the 48 MiB of the page child: a 24 Mpx HEIC photo takes about 100 MB once decoded. The container limit is raised from 128 MiB to 384 MiB, and the design gives the budget for the parent and the child.
- **Submission without a double send.** In order:
  1. Build the requests.
  2. One transaction re-reads `is_private`, `status` and `deleted_at`, moves the items to `submitted`, advances `change_seq` and creates a `batches` row in `pending`.
  3. Send the batch.
  4. Store the Anthropic batch id and switch the row to `in_progress`.

  If the send fails, the items go back to `extracted` without counting an attempt. At start-up, a row still `pending` is reconciled with the batches of the API key created after it; without a match, its items go back to `extracted`. So a crash can cause at most one double send, which is logged.
- **Private items, never sent (RG-12).**
  - The transaction of step 2 excludes any item made private before it, and the request builder refuses a private item as a second guard.
  - A test proves that a private item appears in no request, whatever its status.
  - Once an item is `submitted`, its content is sent or on its way. Checking Privé after that keeps the coming result (RG-14 clarified, decided with Ghislain on 2026-11-06; the result is applied by E3-02).
- **Tables:**
  - `content_types` is shared (ADR-0022) and seeded by migration with the 7 starting keys of `metier.md` §7.
  - `categories` is created by migration and seeded in Go with the tree of `metier.md` §6, in the transaction that creates a user. An existing user is seeded once, guarded by an explicit marker rather than « no category », which would seed again after the user deleted every category. The uniqueness index on the normalised name waits for E3-03, which defines that name (RG-54).
  - `batches`: a ULID `id` (ADR-0035), a unique, nullable `anthropic_batch_id` and the statuses `pending`, `in_progress`, `ended` and `failed`. The row also keeps the model, the prompt version and the envelope version at send time (RG-64), its counters, its tokens and `cost_usd`.
  - Nullable `items` columns `type_id`, `category_id` and `batch_id`. The store checks that a category belongs to the item's user, since `ALTER TABLE` cannot add a reference over two columns.
  - `tags` and `item_tags` wait for E3-04.
- **Anthropic client behind the `Analyzer` interface** (ADR-0005), in `internal/analysis`, the only package allowed to import the SDK. Tests use a fake `Analyzer` and never call the network.
- **Docs:**
  - RG-14 clarified;
  - a new rule under Analyse for the image copy sent to the AI;
  - a byte limit next to RG-32;
  - `spec-v1.md` gets the tables, the batch step and the Paris time;
  - the glossary gets « référentiel »;
  - `backlog.md` marks E3-01 and narrows E3-08.

## Capabilities

### New Capabilities

- `item-analysis`: the analysis of an item by the AI. This change covers the nightly selection, the send guards, the request built for each item, the image copy, the submission and the private-item guarantee. E3-02 to E3-08 add results, retries, immediate analysis and budget.
- `classification`: the content types and categories an item is classified with. This change covers their starting set and how they are listed for the AI. E3-03, E3-04 and epic E4 add proposals, tags and user management.

### Modified Capabilities

- `item-editing`: an item checked Privé while `submitted` stays `submitted`. The kept result is specified with E3-02.

## Non-goals

- Polling the batch, validating and writing results, cost and token accounting (E3-02).
- Category and type proposals, the normalised category name, tags (E3-03, E3-04).
- The retry of a cut response (E3-05). This change only reads `incomplete_retried` to choose the cap.
- Immediate analysis and `analysis_calls`, the budget alert in Réglages (E3-06, E3-08).
- Catching up a night missed while the NAS was off: the next scheduled pass sends everything waiting.
- Preview images of a link (RG-23).
- Any change to the prompt, the fiche schemas or the `classify_item` envelope. No route changes either.
- Changing the model: `ANALYSIS_MODEL` stays `claude-haiku-4-5` (ADR-0005).

## Impact

- **AI cost:** yes. From the first night in production, the waiting non-private items are billed at the batch price, about $0.0035 to $0.025 per item. Until E3-02 records costs, the guard allows only one batch. No eval run is needed: the prompt and schemas do not change.
- **Private items:**
  - never selected, refused again by the request builder, and proven by a test (RG-12);
  - an item made private once `submitted` keeps the result, since its content has already left (RG-14 clarified) (2026-11-06, Ghislain: an item checked Privé before the batch actually leaves is taken out of it and not sent; see design D7.);
  - the image copy carries no location.
- **Persisted data:** one migration adds `content_types` (seeded), `categories`, `batches` and three nullable columns on `items`. The starting categories are seeded per user. The only change to existing items is the `extracted` → `submitted` move.
- **API contract:** none.
- **Prompt and fiche schemas:** none. The request is assembled from `system-prompt.md` and the bundled schema.
- **Infrastructure:** `mem_limit` goes from 128m to 384m in `infra/docker-compose.yml` and `infra/CLAUDE.md`. `.env.example` is corrected on `TZ`.
- **Dependencies** (approved by Ghislain on 2026-11-06), all of which build with `CGO_ENABLED=0`:
  - `github.com/anthropics/anthropic-sdk-go`, planned by ADR-0005;
  - `golang.org/x/image`;
  - `github.com/gen2brain/h265`.
- **Code:**
  - `backend/internal/analysis`;
  - `backend/internal/store`;
  - a shared child-process package taken out of `internal/extraction`;
  - the scheduler in `backend/cmd/hippo`;
  - `backend/internal/config`.
- **User QA:** no. Nothing changes in the app; the fiches appear with E3-02.
