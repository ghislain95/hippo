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
| `api/openapi.yaml` | Documentation only: `createItem` already declares `note` and `empty-note`, and its description did not say a note answers `501`, but it did not describe a note either. It now says that `note` is trimmed (RG-09), gives the title of a note (RG-26), that the text is stored as `user_note` and never a duplicate (RG-04), and lists `empty-note` in the order of the checks; then `make generate`. |
| `backend/internal/capture` | Trimming of `note` (RG-09), `ErrEmptyNote` (RG-02), `noteTitle` (RG-26), note path (`captureNote`) calling the store with a nil merge; the link path moves to `captureLink`. `ErrNoteNotImplemented` is removed. |
| `backend/internal/store` | No code change: `CaptureItem` already skipped the duplicate lookup when `normalized_url` is null (E2-02, for links captured before it), so a note is never a duplicate (RG-04); replay and insert unchanged. A store test covers the note case. |
| `backend/internal/httpapi` | Mapping of `ErrEmptyNote` to `urn:hippo:problem:empty-note`; the `501` mapping of a note is removed; tests. |

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
