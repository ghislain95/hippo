# Tasks

## 1. Contract and migration

- [x] 1.1 In `api/openapi.yaml`: add the optional `source` (`$ref: CaptureSource`, default `share`) to `CaptureRequest` and `CaptureImageRequest`, extend the `createItem` description and its `413`, and state the strict-body and 1 MiB rules in the introduction; verify `make generate` updates `api.gen.go` and the TypeScript types, and `make lint-api` passes
- [x] 1.2 Write `backend/internal/store/migrations/00002_items.sql` (`sync_sequence` seeded, `items` with its `CHECK` constraints, the `client_id` partial unique index and the `(user_id, change_seq)` index, per design.md); verify `go test ./internal/store/...` applies it on a fresh database

## 2. Store

- [x] 2.1 Add `CreateUser` to the store and make `EnsureSingleUser` use it; verify the existing store tests still pass
- [x] 2.2 Write failing store tests: a new item gets `created = true` and a `change_seq` above the previous write's; a known `client_id` of the same user returns the stored item with `created = false` and leaves the sequence unchanged; the same `client_id` for another user creates a second item; two captures without `client_id` create two items; absent values are stored as NULL
- [x] 2.3 Implement `sequence.go` (the only writer of `sync_sequence`) and `items.go` (insert-or-replay in one transaction, row read back, unique violation answered as a replay, every query filtered by `user_id`); verify the tests of 2.2 pass with `-race`

## 3. Capture domain

- [x] 3.1 Write failing table tests for `internal/capture`: `client_id` (`""`, malformed) and `source` checks, trimming (RG-09), ambiguity (RG-07, including `url: ""` with a note and a blank `user_note` with a note), empty capture, invalid URL (RG-02: `javascript:`, `data:`, `mailto:`, `ftp:`, relative, `https://`, `https:example.com`, `https://:443/a`, blank), upper-case scheme accepted, note → not implemented, provisional title (RG-26: case, port, IPv6), and that an invalid-URL error never carries the URL
- [x] 3.2 Implement `internal/capture` (typed errors, validation order of design.md, ULID from the injected clock, call to the store); verify the tests of 3.1 pass and `make lint-backend` reports no `depguard` violation

## 4. HTTP layer

- [x] 4.1 Write failing integration tests in `internal/httpapi`, one sub-test per scenario of `specs/item-capture/spec.md` and of the `api-conventions` delta, named after it, on a real SQLite database with a two-user test authenticator; the "answer matches the contract" scenario validates the body against the `Item` schema of `api/openapi.bundled.gen.json`; the log scenario covers the `201`, `200` and `400 invalid-url` paths
- [x] 4.2 Implement the strict JSON body and 1 MiB middleware (registry with `POST /api/items`) and the `413` mapping in the request error handler; verify the strict-body, empty-body and oversized-body tests pass
- [x] 4.3 Implement the `CreateItem` handler (JSON → capture, multipart → `501`, no body → `415`), the error-to-problem mapping and the item-to-`Item` mapping; remove the `CreateItem` stub and take `createItem` out of the "every operation answers 501" sweep (keep its multipart case); verify the tests of 4.1 pass
- [x] 4.4 Wire the capture service in `cmd/hippo/main.go`; verify with the backend running locally that a `curl` capture answers `201` and its replay `200`

## 5. Documentation

- [x] 5.1 Update `docs/spec-v1.md` ("Modèle de données": `sync_sequence`, columns added by later stories, no unique `normalized_url` before E2-02; "API REST": `source` in the capture body, strict bodies, 1 MiB) and `docs/dev/conventions-backend.md` ("Stockage SQLite": the sequence is advanced by the store's single helper; "Contrat d'API": strict-body registry); verify `make check-docs` passes

## 6. Integration checks

- [x] 6.1 `make check` is green
- [x] 6.2 `code-reviewer` pass on the uncommitted diff, with no 🛑 nor 🔴 left open
- [x] 6.3 `/opsx:verify` passes
- [x] 6.4 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
