# Design

## Context

See `proposal.md`. Since E2-01 and E2-02, `capture.Service.Capture` validates the JSON body (strict body, `client_id`, `source`, RG-09 trimming of `url` and `user_note`, RG-07 ambiguity, RG-02 empty capture) and returns a not-implemented error for a valid `note`; links go through `Normalize` and the store's replay → duplicate → insert transaction (`store.CaptureItem`). The `items` table already has every column a note needs.

## Goals / Non-Goals

**Goals:**
- Notes reuse the link path end to end: same transaction, same replay through `capture_ids`, same mapping to `Item`.

**Non-Goals:**
- A note-specific table or column: none needed, so no migration.

## Decisions

### Packages and contract touched

| Area | Change |
| --- | --- |
| `api/openapi.yaml` | None expected: `createItem` already declares `note` and `empty-note`. If its description still says a note answers `501`, it is corrected (documentation only), then `make generate`. |
| `backend/internal/capture` | Trimming of `note` (RG-09), `empty-note` check (RG-02), `noteTitle` (RG-26), note path calling the store. |
| `backend/internal/store` | `CaptureItem` accepts an item without normalised URL: the duplicate lookup is skipped when `normalized_url` is null (a note is never a duplicate, RG-04); replay and insert unchanged. |
| `backend/internal/httpapi` | Mapping of the new domain error to `urn:hippo:problem:empty-note`; tests. |

No migration. Prompt and fiche schemas unchanged.

### Validation order

Unchanged up to the ambiguity check, then:

1. Trim `note` with `strings.TrimSpace` together with `url` and `user_note` (RG-09).
2. Ambiguity (RG-07): presence of `note` is "present and not null", before trimming matters, as for `url` (`{"note": "   ", "user_note": "x"}` is ambiguous).
3. Neither `url` nor `note` → `empty-capture`.
4. `note` present → empty after trimming → `empty-note`; otherwise the note path.
5. `url` → link path (E2-01, E2-02).

### Provisional title (RG-26)

`noteTitle(text)`: split on `\n` (a `\r` left at a line end is trimmed with the line), take the first line that is non-empty after `strings.TrimSpace`, count Unicode code points (`[]rune`); more than 80 → the first 80 code points followed by `…` (U+2026). A grapheme made of several code points (some emoji, combined accents) can be cut; accepted, the analysis replaces the title.

### Store

The note goes through the same `store.CaptureItem` with `kind = note`, `url` null, `normalized_url` null, `user_note` = text, `title` = provisional title. The duplicate step is skipped when `normalized_url` is null, so two identical notes make two items; the replay step works unchanged through `capture_ids` (RG-08). The note merge function is not called on the note path.

### user_id, private items and budget

Unchanged from E2-02: every query filters by `user_id`; `is_private` is stored; no AI path; no budget check needed.

### Logs

The note text is never logged; the log scenario covers the note path.

## Risks / Trade-offs

- [A grapheme cut by the 80-code-point limit shows a broken glyph in the provisional title] → rare, replaced at analysis.
- [Very long notes up to 1 MiB are accepted] → decided with Ghislain; the AI input is bounded by RG-24 and the budget (RG-81).
