# Proposal: api-contract-v1

## Why

Backlog story **E0-05**: `api/openapi.yaml` only holds the skeleton written at setup (`/healthz`, bearer security, problem+json, cursor pagination). E2 to E5 implement the V1 routes of `docs/spec-v1.md` ("API REST") on both sides, often in separate sessions. ADR-0017 makes the contract the source of truth and requires it to change **before** the code: writing the whole V1 contract now lets the backend and the app work from one stable, generated interface, and turns any divergence into a compile error.

## What Changes

- **Full V1 contract** in `api/openapi.yaml`: every route of the spec table, with request and response schemas named after the glossary, query parameters, status codes and problem types. Operations and schemas are binding; the status codes and problem types of each route stay provisional until the story that implements the route, which amends the contract first. No route is implemented by this change.
- **Fiche typing from a single source**: `backend/cmd/schemabundle` also emits the fiche schemas of `docs/prompt/schemas/fiches/` as OpenAPI components (stable names such as `FicheRecipe`, no `$id`/`$schema`, a `Fiche` union with a `template` discriminator and an explicit mapping derived from the schemas). The contract references them; `redocly bundle` then produces one self-contained file from which the Go server and the TypeScript types are generated.
- **Capture**: one operation `POST /api/items` with two request formats, JSON (link or note) and multipart (image); `201` for a new item, `200` with the existing item for a duplicate link (RG-04) or a replayed capture; an optional client-generated `client_id` makes an offline replay idempotent (new **RG-08**).
- **Problem type catalogue**: stable `urn:hippo:problem:<slug>` types, each with its HTTP status, for the business errors of the V1 (capture, analysis, categories, tags, types, sync) plus `not-implemented` (`501`); generic HTTP errors keep `about:blank`.
- **Sync format**: `GET /api/sync` returns, page by page, the items, categories, content types and tags changed since a **server-issued `sync_token`**, and the deletions as tombstones `{id, deleted_at}`; a token older than the 30-day tombstone retention answers `410 sync-expired` and the app resyncs from scratch. Items never carry `content_text` (ADR-0018).
- **Nullability rule** (checked against the generators): a nullable scalar is `type: [T, "null"]`; a nullable reference is `anyOf: [$ref, {type: "null"}]`; the fiche is optional rather than nullable; never a named nullable schema, never `type: [object, "null"]` with a `oneOf`. `PATCH` bodies distinguish an absent field from an explicit `null` (for example moving an item back to À trier).
- **Identifiers**: items, assets and batches use ULIDs; categories and tags use integer ids; content types are named by their `key`.
- **New ADR-0035**, which makes precise ADR-0017 (identifiers, generated fiche components and bundle step) and ADR-0018 (sync token, tombstones, `sync-expired`, a per-row change sequence instead of `updated_at` as the sync cursor).
- **Backend**: the generated strict server declares every route; each operation, request format or field not yet implemented answers `501 not-implemented`, until the story that owns it replaces the stub.
- **Two new Go dependencies, approved by Ghislain**: `github.com/oapi-codegen/runtime` (needed by the generated server) and `github.com/oapi-codegen/nullable` (absent versus `null` in `PATCH` bodies).
- **Business rules decided by Ghislain during framing** (`docs/metier.md`):
  - **RG-07** (new): a JSON capture that holds both a URL and a note, or a note and a capture note, is rejected (`400 ambiguous-capture`).
  - **RG-08** (new): a capture that carries a `client_id` already used by the same user returns the existing item (`200`) instead of creating a second one.
  - **RG-33** (completed): an immediate analysis is refused (`409`) while the item is `queued` (extraction not finished) or `submitted` (already in tonight's batch).
  - **RG-36** (amended): an answer cut off by the output limit is not stored; the item is queued again once with the limit doubled, keeping its previous fiche when it is a reanalysis; cut off again, it becomes `failed` with the reason "fiche trop longue". The extracted content stays.
  - **RG-74** (completed): renaming a category or a tag to a name already taken under the same parent is refused (`409 name-taken`); merging stays the explicit gesture.

## Non-goals

- Implementing any route other than `/healthz` (E2 to E4 do, one story each).
- Specifying the behaviour of each route (normalisation, deduplication, filters, review, merge…): each owning story's change writes those requirements and their tests.
- Changing a fiche schema, the prompt or the database schema: no migration, no `x-schema-version` bump. The data model corrections (tombstones for tags and content types, change sequence, `client_id`) are written in the spec and done by the stories that create those tables.
- Any app screen; the multipart upload from React Native (`FormData`) is E5's.

## Capabilities

### New Capabilities

- `api-conventions`: the rules every route of the HTTP API follows and that can be observed now — bearer authentication on `/api/*` and the public `/healthz`, problem+json errors, `501 not-implemented` for a declared route, `404` for an unknown one, and `400` for a request that breaks the contract.

### Modified Capabilities

None (no existing spec).

## Impact

- **Story**: E0-05. **Business rules**: RG-02, RG-03, RG-04, RG-06, RG-12, RG-33, RG-35, RG-36, RG-51, RG-64, RG-72, RG-74, RG-79, RG-81, RG-90, RG-91, RG-95, RG-96, new RG-07 and RG-08. **ADRs**: ADR-0006, ADR-0007, ADR-0016, ADR-0017, ADR-0018, ADR-0022, ADR-0031, new ADR-0035.
- **AI cost**: none (no call to the Anthropic API).
- **Private items**: none (no route that reaches the AI is implemented).
- **Persisted data**: none in this change (no migration); the spec's data model is corrected for the stories to come.
- **API contract**: yes, the whole V1 surface. Breaking for nobody: only `/healthz` is served today, and it is unchanged.
- **Prompt and fiche schemas**: unchanged; RG-36 changes how the pipeline handles a cut-off answer (E3-05), which `docs/prompt/README.md` records.
- **Code**: `api/openapi.yaml`, its lint rules and two generated files (fiche components, bundled contract); `backend/cmd/schemabundle` (OpenAPI output); `make generate` and `make check-generated`; the CI `api` filter (fiche schemas); `backend/internal/httpapi` (regenerated server, `501` stubs, adjusted tests); `app/src/api/schema.gen.ts` (regenerated types, type check of the fiche examples); the hook pattern of generated files.
- **Docs**: `docs/metier.md`, `docs/glossary.md`, `docs/spec-v1.md` ("Modèle de données", "API REST"), `docs/backlog.md` (E3-05, E4-09, E5-04 aligned; E0-05 done at archive), `docs/prompt/README.md`, `docs/dev/conventions-backend.md`, `api/CLAUDE.md`, ADR-0035, `openspec/config.yaml` (task rule aligned on the OpenSpec instruction: each group lands its own tests and docs).
- **User QA**: no (no app change visible to the user).
