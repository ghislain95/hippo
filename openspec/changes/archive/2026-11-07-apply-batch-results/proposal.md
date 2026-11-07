# Proposal

Backlog stories: **E3-02 — Follow the batch and apply its results**, together with **E3-03 — Create category and type proposals**, **E3-04 — Normalise and attach tags** and **E3-05 — Retry a cut response** (decided with Ghislain on 2026-11-06: one change for step 5 of the pipeline, « Résultats »).

Rules: RG-12, RG-13, RG-14, RG-30, RG-33, RG-34, RG-35, RG-36, RG-37, RG-50 to RG-59, RG-61, RG-64, RG-72, RG-74, RG-79, RG-80, RG-81, RG-90, RG-92, RG-95 · ADRs: ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0009, ADR-0017, ADR-0021, ADR-0022, ADR-0023, ADR-0024, ADR-0032, ADR-0035.

## Why

E3-01 sends the nightly batch, but nothing reads it back: the items stay `submitted`, no fiche is written, and the batch never gets a cost, so the « batch without cost » guard blocks every later night. This change closes the loop, so that the Anthropic API key can go on the NAS.

The four stories ship together because the validation order of `docs/prompt/README.md` needs them all: a response that proposes a category, or carries tags, cannot be applied in full without E3-03 and E3-04, and `metier.md` has no rule for a partial write.

## What Changes

- **Follow the batch, inside the pass.** One `Pass`, one mutex. A new step « follow and apply » runs for each user right after the reconciliation of `pending` batches and before the « batch without cost » guard, so that a batch ending at 01:56 is applied before the 02:00 send decides (RG-30). The same step runs at start-up and every 15 minutes while a batch is `in_progress` or `ended` without cost. A failed poll is retried at the next tick.
- **Read the results line by line** (never the whole file in memory), with Hippo's own reader instead of the SDK's stream, so that an unreadable line is skipped and counted instead of stopping the read. A result whose `custom_id` is not an item `submitted` in that batch row is ignored and logged (design D8 of E3-01).
- **Idempotent application.** An item is applied only while it is `submitted` in that batch and that user, re-read in the write transaction; the batch's tokens are recounted over the whole file on every read; the cost is written once (`cost_usd IS NULL`). A restart in the middle of a read loses nothing and applies nothing twice.
- **The batch row is the reference.** The model, prompt version and envelope version recorded on the item come from the batch row, not from the running configuration. When the embedded envelope version differs from the batch's (a redeploy between send and results), the item goes back to `extracted` without an attempt counted.
- **Per request outcome** (decided with Ghislain on 2026-11-06, RG-35 clarified):
  - `succeeded`, `stop_reason = tool_use`: validated in the order of `docs/prompt/README.md`; the first failure rejects the response whole: nothing is written, `attempts + 1`, back to `extracted`; at the third attempt, `failed` (RG-35);
  - `succeeded`, `stop_reason = max_tokens`: nothing written, no attempt counted; the first time, back to `extracted` with `incomplete_retried` set (8 000-token cap next night); the second time, `failed` « fiche trop longue », extracted content kept (RG-36);
  - `succeeded` with any other `stop_reason` (`end_turn`, `refusal`, `pause_turn`…): an invalid response;
  - `errored`: one attempt counted, like an invalid response;
  - `expired` or `canceled`: back to `extracted` without an attempt counted.
- **`last_error`** holds a closed set of codes (`invalid_response`, `request_errored`, `too_long`…), never a message from the API or the validator, which may quote the request or the fiche. The contract's `last_error` gets the matching `enum`.
- **Write a valid result** in one transaction per item: type, category, tags, title, fiche JSON, language, freshness, `content_ok` and its issue, model, prompt version, fiche schema version (RG-64), `search_text` flattened from the fiche (ADR-0007), `analyzed_at`, status `analyzed`, a new `change_seq` (ADR-0035); `attempts`, `last_error` and `incomplete_retried` reset. A field listed in `user_edited` is left as it is and creates no proposal (RG-61).
- **Proposals** (E3-03): the parent of a proposed category is corrected first (RG-55), then the name is deduplicated against the user's categories and the batch's earlier proposals (RG-54); a new one is created `proposed`, colour inherited from its parent (RG-59), and the item is filed in it (RG-53). A proposed type is deduplicated by key, then created `proposed` without template, fiche `generic` (RG-57). Nothing in the pipeline renames, moves, merges or deletes a category, a type or a tag (RG-56), proven by test.
- **Tags** (E3-04): tables `tags` and `item_tags`, each with `user_id` (RG-90). Tags are normalised, deduplicated, and existing tags reused (RG-58). The référentiel's `<tags>` block, empty since E3-01, lists the user's tags, most used first then by name, 300 at most (RG-52, `system-prompt.md`).
- **Unreadable batch**: a batch whose results Anthropic no longer serves (not found, archived, past retention) is closed; its items without a result go back to `extracted` (decision 1 below).
- **Private and deleted items.** An item checked Privé after its batch left keeps that batch's result (RG-14, decided on 2026-11-06). A deleted item's result is dropped (RG-79).
- **Cost** (RG-80, RG-81): the tokens of **every** `succeeded` line (applied, rejected, cut, deleted item, unknown `custom_id`) are summed per counter (input, output, cache write 5 min and 1 h, cache read) and priced at the batch rate (half the standard price) from two new settings, the standard input and output prices per million tokens of `ANALYSIS_MODEL` (decided with Ghislain on 2026-11-06); cache ratios in code (×1.25 for a 5-minute write, ×2 for a 1-hour write, ×0.1 for a read). The model and the prices in force are logged at start-up.
- **Items served by the API.** `createItem` (duplicate capture) and `updateItem` already return an `Item`; it now carries the item's content type, category, tags and fiche instead of empty values.
- **Logs**: ids, counts, outcome classes, error types, tokens and cost only; never a response, a fiche, an API error message, a tag or category name.

## Non-goals

- Immediate analysis (E3-06), « Relancer » and reanalysis (E3-07), `analysis_calls` and the budget alert in Réglages (E3-08).
- Any new route reading items, fiches, categories or tags (E4); the app displaying them (E5).
- Any change to the system prompt text or a fiche schema.
- Cancelling a batch from Hippo; reading the results of a batch closed as `failed` by an ambiguous reconciliation (residual risk, logged `possible_double_send` since E3-01).

## Decisions taken with Ghislain after the adversarial pass (2026-11-07)

1. **Unreadable batch** (results not found, archived or past retention): its cost is the tokens already read (0 when nothing was read); its items without a result go back to `extracted` without an attempt counted. The budget may be under-counted by one night at most.
2. **Tags before the schema check**: tags are normalised (RG-58) before the envelope is validated; fewer than 2 distinct tags left makes the response invalid. The validation order of `docs/prompt/README.md` changes accordingly; the prompt and the schemas do not.
3. **A proposed type whose key already exists**: the item is filed under the existing type and keeps its `generic` fiche, as after a merge (RG-72).
4. **Proposed types stay shared by all users** (ADR-0022): a known V1 limit, written in the design; an ADR when a second user arrives.
5. **Tags of private items**: the référentiel lists only tags carried by at least one non-private, non-deleted item (RG-12).
6. **Normalised category name** (RG-54): lower case, accents removed, inner spaces collapsed, edges trimmed. « Ménage » = « menage »; « Java & Spring » ≠ « Java et Spring ».
7. **`golang.org/x/text`** becomes a direct dependency (Unicode normalisation to remove accents).
8. **RG-13** gets a cross-reference to RG-14's exception: an item checked Privé after its batch left keeps that batch's fiche.

## Impact

- **AI cost**: none by itself (polling and result downloads are free). It unblocks the nightly batch: about 2 to 4 $ a month at 20 items a day, under the 5 $ budget (ADR-0021). The first real batch goes out only once Ghislain puts the API key on the NAS, after the merge. A paid end-to-end test (2 or 3 fictitious items, a few cents) is proposed to Ghislain before that, not run without his agreement.
- **Private items**: none sent; this change reads results only. An item made private after its batch left gets its result (RG-14). The référentiel lists only tags of non-private items (decision 5).
- **Persisted data**: migration `00007`: `tags` and `item_tags` (with `user_id` and composite foreign keys), a normalised-name column on `categories` filled in Go at start-up, unique partial indexes (`deleted_at IS NULL`, `COALESCE(parent_id, 0)`); writes of the analysis columns of `items`, `proposed` rows in `categories` and `content_types`, and the token and cost columns of `batches`. No existing data is rewritten.
- **API contract**: `last_error` becomes an `enum` of codes; `createItem` and `updateItem` return the classification and fiche they had hidden. No new route.
- **Prompt and fiche schemas**: no prompt text or fiche schema changes. The validation order of `docs/prompt/README.md` changes (normalise tags before the schema check), through `prompt-engineer`, without a version change.
- **Configuration**: two price settings, defaults 1 $ and 5 $ per million tokens (Haiku 4.5, checked on 2026-11-06), in `infra/.env.example`.
- **Docs**: RG-35 clarified (expired and canceled requests count no attempt), RG-13 cross-reference, RG-54 normalised name, RG-58 normalisation before validation; `docs/spec-v1.md`; glossary; backlog E3-02 to E3-05.
- **User QA: no.** Nothing is visible in the app yet.
