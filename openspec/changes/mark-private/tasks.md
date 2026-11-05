# Tasks

No migration, no prompt nor fiche schema, no dependency in this change (design.md, "Packages touched").

## 1. Contract

- [x] 1.1 Make the description of `updateItem` in `api/openapi.yaml` precise per RG-14 (unchecking private puts an item without fiche back in the analysis queue; an analysed item keeps its fiche without reanalysis; the processing status never changes), then `make generate`; verify `make check-api` (or the contract lint of `make check`) passes and the bundled contract is up to date

## 2. Tests first

- [x] 2.1 Write failing integration tests on a real temporary SQLite, one sub-test per scenario of `specs/item-editing/spec.md`, named after it: flag set and cleared with `updated_at` and `change_seq` advanced, same value writing nothing, `extracted`/`analyzed`/`failed` statuses kept (`analyzed` and `failed` set by SQL), another user's item and unknown id `404`, undeclared member, empty object, wrong type and null `400`, `is_pinned` and mixed request `501` applying nothing; verify they fail

## 3. Implementation

- [x] 3.1 Implement `UpdateItem` in `internal/httpapi/edit.go` per design.md ("Handler"), add `PATCH /api/items/{item_id}` to `strictBody`, remove the `UpdateItem` stub, wire the store into `httpapi.Options`; verify the tests of 2.1 pass with `-race` and `TestEveryOperationAnswers501` still covers `updateItem`
- [x] 3.2 Update the comment of `store.SetPrivate` (no requeue needed: the batch's queue is `extracted` and not private, RG-14); verify `make check-backend` passes

## 4. Documentation

- [x] 4.1 `docs/spec-v1.md`: the `PATCH /api/items/{item_id}` row says only `is_private` is applied until E4-03, other members `501`; `docs/backlog.md`: E2-07's inherited note resolved by RG-14 as made precise; verify `make check-docs` passes

## 5. Checks

- [x] 5.1 `make check` green; code-reviewer pass with no 🛑 nor 🔴 open; every scenario has its named sub-test
