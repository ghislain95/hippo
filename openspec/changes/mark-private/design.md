# Design

## Context

See `proposal.md`. What exists:

- `store.SetPrivate(ctx, userID, id, private)` (`backend/internal/store/items.go`): one transaction, `itemByID` filtered by `user_id` and `deleted_at IS NULL`, `ErrNotFound` otherwise; when the flag changes, `nextChangeSeq` then `UPDATE items SET is_private, updated_at, change_seq`; the status is never touched. It returns the item with its assets. Its comment says that putting a public item back in the queue « belongs to the story of the fiche's switch »: with RG-14 as made precise on 2026-11-05, nothing more is needed, since the queue of the nightly batch is « `extracted` and not private » (spec-v1, « Envoi du batch »).
- `itemFromStore` (`internal/httpapi/items.go`) maps a stored item to the contract's `Item`, as the capture answers it.
- `strictBody` (`internal/httpapi/body.go`) lists the operations whose JSON body is decoded strictly (undeclared member, trailing data) before the generated decoder; only `POST /api/items` today.
- `notImplemented` (`internal/httpapi/stubs.go`) answers `501` for every operation not implemented; its comment already foresees an operation implemented in part.
- `TestEveryOperationAnswers501` (`internal/httpapi/operations_test.go`) sends `{"is_pinned": true}` to `updateItem`: it keeps answering `501` with this change, so the operation stays in the sweep.
- The extraction writes only its own columns, guarded by `status = 'queued'` (`store/extraction.go`): a change of the flag during an extraction does not conflict with it.

## Goals / Non-Goals

**Goals:**
- The user can check and uncheck Privé on any of their items, with the sync fields advanced.
- No path changes the processing status or triggers an AI call.

**Non-Goals:** see `proposal.md`.

## Decisions

### Packages touched

| Package | Change |
| --- | --- |
| `api/openapi.yaml` | Description of `updateItem` made precise (RG-14); no schema change; `make generate` |
| `internal/httpapi` | `UpdateItem` handler in a new file `edit.go`; `strictBody` gains `PATCH /api/items/{item_id}` → `&ItemPatch{}`; the `UpdateItem` stub is removed; a narrow interface for the store |
| `internal/store` | Comment of `SetPrivate` updated; no new query |
| `cmd/hippo` (or where `httpapi.Options` is built) | The store passed to the new option |

### Handler

`UpdateItem(ctx, request)`:

1. The body is already strict (`strictBody`). A nil body answers `400`.
2. If any member other than `is_private` is present (`ContentType != nil`, `CategoryId.IsSpecified()`, `Tags != nil`, `IsPinned != nil`, `Annotations.IsSpecified()`, `UserNote.IsSpecified()`), return `errNotImplemented`: the whole request answers `501` before any read or write, so a mixed request applies nothing.
3. If `IsPrivate == nil`, answer `400`: this covers `{}` (`minProperties: 1`) and `{"is_private": null}` (the member is not nullable; the generated decoder leaves a nil pointer for both).
4. `SetPrivate(ctx, userID, item_id, *IsPrivate)`; `store.ErrNotFound` → `404` problem `about:blank`; any other error → `500`.
5. `200` with `itemFromStore(item)`.

The user id comes from the request context, as for the capture. No log line beyond the request line (route and status): no title, URL nor note.

Alternative rejected: apply `is_private` and ignore the other members. A client would believe its pin or tags saved.

### Strict body and `null`

`decodeStrict` uses `encoding/json` with `DisallowUnknownFields`: `{"is_private": "yes"}` fails the decode (`400`), an undeclared member too. `null` for `is_private` decodes to a nil pointer and is caught by step 3. `null` for `annotations`, `category_id` or `user_note` is legal (`nullable.Nullable` reports it specified) and answers `501` by step 2.

### Sync and idempotence

`SetPrivate` already writes nothing when the value is unchanged, so a replayed request (offline queue of the app) leaves `updated_at` and `change_seq` as they are. Each real change takes a new `change_seq` in the same transaction (ADR-0035).

### Statuses

No status change, by RG-14 as made precise with Ghislain on 2026-11-05: `extracted` made public is in the batch's selection again; `analyzed` keeps its fiche without reanalysis; `failed` waits for « Relancer »; `queued` goes on to its extraction. `submitted` is E3-01's business (backlog). The tests set `analyzed` and `failed` by SQL on the temporary database, since no code writes them yet (no mock of SQLite).

### user_id, private items and budget

Every query filters by `user_id` (RG-90, existing `itemByID`). Nothing is sent to the AI; no budget check is needed (RG-81 does not apply).

## Risks / Trade-offs

- [The app sends a mixed PATCH before E4-03] → `501` applies nothing; the app gets a clear refusal and the item does not end half-changed.
- [An item made private while `submitted`] → no item is `submitted` until E3-01 exists; E3-01 owns the case (backlog).
- [`analyzed` made private, then public: no new fiche even if the prompt evolved] → accepted with Ghislain; reanalysis by hand (E3-07).

## Migration Plan

None: no migration, no data to rewrite.

## Open Questions

None.
