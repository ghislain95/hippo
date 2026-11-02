# Tasks

## 1. Dependencies and migration

- [x] 1.1 Add `codeberg.org/readeck/go-readability/v2@v2.1.3` and `golang.org/x/net` to `backend/go.mod` (`go mod tidy`); add the `depguard` rule that only `internal/extraction` imports them; verify `make lint-backend` passes
- [x] 1.2 Write `backend/internal/store/migrations/00005_extraction_attempts.sql` per design.md; verify a store test applies it and refuses an attempt row for another user's item

## 2. Store

- [x] 2.1 Write failing store tests then implement `extraction.go`: users list, next `queued` items per user (oldest first, deferred PDFs excluded), start / defer / delete an attempt, write a result (extraction columns only, `change_seq` and `updated_at` advanced, zero row ignored); verify with `-race`

## 3. Reading pages

- [x] 3.1 Write fictional fixtures in `backend/testdata/pages/` (see design.md) and failing table tests for the page reader: charset, `og:*` only on 2xx, `full` threshold, title and site name order, truncation at a word boundary in code points, reading time before truncation
- [x] 3.2 Implement the page reader and the address policy (table test of allowed and refused addresses); verify the tests of 3.1 pass
- [x] 3.3 Measure the heap peak of one extraction of a 5 MB fixture (`testing.B` with `ReportAllocs` or `runtime.ReadMemStats`); lower the body limit to 2 MB if it exceeds 40 MB, and write the measure in design.md

- [x] 3.4 Read each page in a child process of the same binary (`hippo extract-page`) with a memory watchdog and a time limit, and an environment holding no secret; remove the element count (`countElements`, `FuzzElementBound`); verify the hostile pages of the reviews fall back without growing the memory of the service, a list of cards is read `full`, and measure the child peak on the 2 MB article (design.md)

## 4. Worker

- [x] 4.1 Write failing integration tests, one sub-test per scenario of `specs/item-extraction/spec.md` named after it, on a real SQLite file with `httptest` servers and a test address policy (plus the production-policy tests of design.md), two users included
- [x] 4.2 Implement the fetcher (own client, dialer `Control`, no proxy, 15 s, redirects, user info removed, headers, limit), the worker (wake-up, scan, notes and images, YouTube skip, crash marker, stop, network outage, write) and the logs; verify the tests of 4.1 pass with `-race`

## 5. Wiring

- [x] 5.1 `capture` calls a `Waker` after a created item; `cmd/hippo` starts the worker with the production policy, wakes it at start-up, stops it before closing the store; `httpapi` and `capture` tests use a no-op waker; verify `make check-backend` passes and a local run extracts a public page served by a local fixture server only when the test policy is not used (link to `127.0.0.1` falls back)

## 6. Documentation

- [x] 6.1 Update `docs/spec-v1.md` (pipeline: extraction worker, modes, limits, RG-27; model: `extraction_attempts`), `docs/dev/conventions-backend.md` (`extraction` package, outbound HTTP rules, `depguard`), `docs/adr/0008-extraction-et-reanalyse.md` is not rewritten (the fork is noted in `docs/spec-v1.md`), and the backlog notes of E2-06 and E2-08 (pick up `queued` YouTube links and `deferred_pdf` attempts) and E4-01 (`search_text` after extraction); verify `make check-docs` passes

## 7. Integration checks

- [x] 7.1 `make check` is green
- [x] 7.2 `code-reviewer` pass on the uncommitted diff, with no 🛑 nor 🔴 left open
- [x] 7.3 `/opsx:verify` passes
- [x] 7.4 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
