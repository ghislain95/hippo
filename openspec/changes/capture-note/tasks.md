# Tasks

## 1. Contract

- [ ] 1.1 Check the `createItem` description in `api/openapi.yaml`; if it still says a note answers `501`, correct it and run `make generate`; verify `make lint-api` passes

## 2. Capture domain

- [ ] 2.1 Write failing table tests for `internal/capture`: note trimming, `empty-note` (empty, blank), ambiguity of a blank note with a capture note, and `noteTitle` (short line, long line, exactly 80 code points with accents, leading blank lines, `\r\n`)
- [ ] 2.2 Implement the note path (trim, `empty-note`, `noteTitle`, store call without normalised URL); verify the tests of 2.1 pass

## 3. Store

- [ ] 3.1 Write a failing store test: two identical notes make two items, a note with a known `client_id` is a replay; implement the skip of the duplicate step when `normalized_url` is null; verify with `-race`

## 4. HTTP

- [ ] 4.1 Update and add integration tests in `internal/httpapi`, one sub-test per scenario of `specs/item-capture/spec.md` (the modified "A blank capture note does not make a note ambiguous" included, the removed "Note capture" sub-test deleted), with the `Item` schema validation for a note; map the new error to `empty-note`; verify `make check-backend` passes

## 5. Documentation

- [ ] 5.1 Update `docs/spec-v1.md` (API: note capture available; RG-26 title of a note) and the glossary if needed; verify `make check-docs` passes

## 6. Integration checks

- [ ] 6.1 `make check` is green
- [ ] 6.2 `code-reviewer` pass on the uncommitted diff, with no 🛑 nor 🔴 left open
- [ ] 6.3 `/opsx:verify` passes
- [ ] 6.4 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
