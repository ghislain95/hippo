# Tasks

## 1. Contract and migration

- [x] 1.1 Update the description of the `200` of `createItem` in `api/openapi.yaml` (first note undated, identical note ignored, privacy only tightens; RG-04, RG-15); verify `make generate` and `make lint-api` pass
- [x] 1.2 Write `backend/internal/store/migrations/00003_capture_ids.sql` per design.md (table, copy of `items.client_id`, index and column dropped, unique `normalized_url` index); verify a store test that captures with a `client_id` at version 2, migrates, then replays it gets the same item

## 2. Capture domain

- [x] 2.1 Write failing table tests for `capture.Normalize`: every rule of design.md (fragment, scheme and host case, user info and port kept, IDN lower-cased, root and trailing slash, `/a//`, `%2F` kept, empty pairs, tracking names in any case, `ref` with and without value, stable sort of repeated names, empty query) and the delta's normalisation scenarios
- [x] 2.2 Write failing table tests for `capture.MergeNote`: no note, first note kept undated, dated append, identical to the first note, identical to a dated entry of another day, note contained in another one, multi-paragraph first note
- [x] 2.3 Implement `normalize.go`, `note.go` and the Paris day (location loaded once, injectable for tests, `time/tzdata`), including the DST days `2026-10-24T22:30Z` → `2026-10-25` and `2026-10-25T23:30Z` → `2026-10-26`; verify the tests of 2.1 and 2.2 pass

## 3. Store

- [x] 3.1 Write failing store tests: replay through `capture_ids` (created and merged captures), duplicate with and without changes (`change_seq` and `updated_at` advance only on change), `capture_ids` row recorded on a duplicate, privacy only tightens, two users isolated, safety net on each statement
- [x] 3.2 Rework `items.go` into replay → duplicate → insert in one transaction with the merge function argument, `setPrivate`, and the statement-aware safety net; verify the tests of 3.1 pass with `-race`

## 4. HTTP and wiring

- [x] 4.1 Update and add integration tests in `internal/httpapi`, one sub-test per scenario of `specs/item-capture/spec.md`, named after it (the modified "Captures without client_id are never replays" included), with an injected clock for the dated-note scenarios; verify they pass
- [x] 4.2 Load the Paris location in `cmd/hippo/main.go` (start refused on failure) and pass it to the capture service; verify `make check-backend` passes and a local run answers `201` then `200` for the same link

## 5. Documentation

- [x] 5.1 Update `docs/spec-v1.md` (model: `capture_ids`, `items.client_id` gone, unique `normalized_url`; API: duplicate `200`), `docs/glossary.md` (identifiant de capture → `capture_ids`) and `docs/dev/conventions-backend.md` if needed; verify `make check-docs` passes

## 6. Integration checks

- [ ] 6.1 `make check` is green
- [x] 6.2 `code-reviewer` pass on the uncommitted diff, with no 🛑 nor 🔴 left open
- [x] 6.3 `/opsx:verify` passes
- [ ] 6.4 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
