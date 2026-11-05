# Proposal

## Why

The privacy flag is recorded at capture (E2-01) and a duplicate capture can tighten it (RG-15), but nothing lets the user change it afterwards: `store.SetPrivate` exists, no route exposes it, and `PATCH /api/items/{item_id}` answers `501`. The app's fiche needs this switch before the analysis exists (E3), so that an item can be withdrawn from the AI, or handed back to it, at any time. This change implements backlog story **E2-07 — Marquer un élément privé**.

## What Changes

- `PATCH /api/items/{item_id}` (`updateItem`, already in the contract) is implemented for `is_private` only: `{"is_private": true}` or `{"is_private": false}` answers `200` with the complete item. The flag is written with a new `updated_at` and `change_seq` (ADR-0035); a value equal to the current one writes nothing (RG-10, RG-14).
- A request carrying any other member of `ItemPatch` (`content_type`, `category_id`, `tags`, `is_pinned`, `annotations`, `user_note`) answers `501 not-implemented` as a whole, without applying anything, until story E4-03 implements the corrections (decided with Ghislain on 2026-11-05).
- The body follows the contract strictly, as the capture's: an undeclared member, data after the value, a wrong type, an empty object or `is_private` null answer `400`.
- Changing the flag never changes the processing status (RG-14, as made precise with Ghislain on 2026-11-05):
  - checking Privé keeps the item's status and fiche; the flag alone keeps it from the AI (RG-12);
  - unchecking Privé on an item without fiche puts it back in the analysis queue: a private item stops at `extracted` (metier.md §4), so once public it is again an `extracted` non-private item, the selection of the nightly batch (spec-v1, step 3); a `queued` item goes on to its extraction as before;
  - unchecking Privé on an `analyzed` item keeps its fiche, without reanalysis and without cost; a reanalysis is asked by hand (E3-07);
  - unchecking Privé on a `failed` item leaves it `failed`, waiting for « Relancer » (RG-35).
- An unknown item or another user's item answers `404` (RG-90).
- The contract's description of `updateItem` is made precise (no schema change).

## Non-goals

- The other members of `ItemPatch` (corrections, pin, annotations, capture note): E4-03 and later stories.
- An item made private while it is `submitted` in the nightly batch: E3-01 decides what becomes of its batch result (backlog, E3-01).
- Enforcing « never selected for analysis » and « no reanalysis of a private item » in the batch, the immediate analysis and the reanalysis: E3-01, E3-06, E3-07, which do not exist yet.
- The switch in the app's fiche (E4).
- `GET /api/items/{item_id}`: still `501`, owned by another story.

## Capabilities

### New Capabilities

- `item-editing`: changing an existing item through `PATCH /api/items/{item_id}`; this change brings the privacy flag, E4-03 will add the corrections.

### Modified Capabilities

None. The `api-conventions` requirement « Declared but not implemented routes answer 501 » already covers a field not implemented yet.

## Impact

- **Story**: E2-07.
- **Business rules**: RG-10, RG-12, RG-13, RG-14 (made precise on 2026-11-05 in `docs/metier.md`), RG-15, RG-35, RG-90.
- **ADRs**: ADR-0023 (private items), ADR-0035 (identifiers and sync: `updated_at`, `change_seq`), ADR-0019 (JSON body bound).
- **AI cost**: none. No call to the AI, no reanalysis triggered.
- **Private items**: the flag changes, nothing is sent anywhere; the request logs keep their route and status only.
- **Persisted data**: no migration; `is_private`, `updated_at`, `change_seq` written by the existing `store.SetPrivate`.
- **API contract**: description of `updateItem` made precise; no schema change; `make generate` refreshes the bundled contract.
- **Prompt and fiche schemas**: none.
- **Dependencies**: none.
- **User QA: no**.
