# Tasks

## 1. Contract and migration

- [x] 1.1 Update `api/openapi.yaml` per design.md (`createItem` multipart order and both `413`, `CaptureImageRequest` fields, `getAsset` headers and owner rule, `Asset.mime_type` enum); verify `make generate` and `make lint-api` pass
- [x] 1.2 Write `backend/internal/store/migrations/00004_assets.sql` (table, composite foreign key, indexes); verify a store test applies it and refuses an asset whose `user_id` differs from its item's

## 2. Files

- [x] 2.1 Write failing tests then implement `internal/assetfs` (temporary file under the assets directory, `fsync`, rename, directory `fsync`, remove, open through `os.Root`, leftover `.tmp-*` cleanup); verify the tests pass

## 3. Capture domain

- [x] 3.1 Write failing table tests for format detection (JPEG, PNG, WebP chunks, HEIC brands incl. `mif1`+`heic` accepted and AVIF refused, truncated `ftyp`, truncated JPEG/PNG header, GIF, SVG, text) and for the image title (note line, dated title, `1er`, Paris day)
- [x] 3.2 Implement `internal/capture/image.go` and the image path (checks order, store call, file removal on every non-created path); verify the tests of 3.1 pass

## 4. Store

- [x] 4.1 Write failing store tests then implement `assets.go` and the assets of the item read back (insert in the capture transaction, by id and `user_id`, list for an item); verify with `-race`

## 5. HTTP and wiring

- [x] 5.1 Implement the multipart bound, the per-route deadlines and the multipart parser in `body.go`/`items.go`, `GetAsset`, the `nosniff` middleware and the `Item.assets` mapping; remove the `GetAsset` stub and take `getAsset` out of the 501 sweep
- [x] 5.2 Write integration tests, one sub-test per scenario of both deltas, named after it, with the data directory checked for leftover files on every refusal and on concurrent replays; verify `make check-backend` passes
- [x] 5.3 Create `DATA_DIR/assets` and clean `.tmp-*` at start-up in `cmd/hippo/main.go`; verify a local run captures a PNG (`201`), replays it (`200`), downloads it, and gets `413`/`400` for an oversized or unsupported file

## 6. Documentation

- [x] 6.1 Update `docs/spec-v1.md` (model: `assets` columns and path; API: image capture and `getAsset`), `docs/glossary.md` if needed, `docs/dev/conventions-backend.md` (layout: `assetfs`; files written before the transaction), and the backlog notes for E5-02/E5-05 (size checked before sending), the deletion story (purge files) and E6-03 (exclude `.tmp-*`); verify `make check-docs` passes

## 7. Integration checks

- [ ] 7.1 `make check` is green
- [x] 7.2 `code-reviewer` pass on the uncommitted diff, with no 🛑 nor 🔴 left open
- [x] 7.3 `/opsx:verify` passes
- [ ] 7.4 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
