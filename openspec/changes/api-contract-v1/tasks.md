# Tasks

## 1. Business rules and decisions

- [ ] 1.1 Add RG-07 and RG-08, complete RG-33 and RG-74, amend RG-36 in `docs/metier.md` as written in the proposal, dated 2026-10-25; verify the numbering stays unique (`git grep -n "RG-0[78]" docs/metier.md`)
- [ ] 1.2 Update `docs/glossary.md`: capture identifier (`client_id`), sync token (`sync_token`), change sequence, tombstone, and the new meaning of "fiche incomplète" after RG-36; verify `make check-docs`
- [ ] 1.3 Write ADR-0035 (precises ADR-0017 and ADR-0018: design D1, D3, D6) with `/new-adr`, update the status lines of ADR-0017 and ADR-0018 and the ADR index; verify `make check-docs`

## 2. Generation pipeline

- [ ] 2.1 Add `github.com/oapi-codegen/runtime` and `github.com/oapi-codegen/nullable` to `backend/go.mod` (approved during framing); verify `go mod tidy` leaves them required once the server is generated (task 3.5)
- [ ] 2.2 Write the failing tests of the OpenAPI output of `schemabundle`: one `Fiche<Key>` component per fiche file, common definitions named `Fiche<Name>`, no `$id`/`$schema`, `type: string` on each `template` const, internal refs to `#/components/schemas/`, and a `Fiche` `oneOf` whose discriminator mapping keys equal the set of `template` constants; verify they fail
- [ ] 2.3 Implement the `-openapi-out` output of `schemabundle` (design D1) and make the tests of 2.2 pass; verify `go test ./cmd/schemabundle/`
- [ ] 2.4 Rewrite `make generate` in the order of design D1 (schemabundle, `redocly bundle`, oapi-codegen with `nullable-type: true`, openapi-typescript from the bundle), remove the `go:generate` directives, add the new generated files to `GENERATED`, `.gitattributes` and the generated-file pattern of `.claude/hooks/protect-files.sh` (with regression cases); verify `make generate && make check-generated && bash .claude/hooks/protect-files.test.sh`
- [ ] 2.5 Add `docs/prompt/schemas/**` and `backend/cmd/schemabundle/**` to the CI `api` filter; verify `actionlint`
- [ ] 2.6 Document the pipeline: generated-files table and nullability rule in `docs/dev/conventions-backend.md`, `api/CLAUDE.md`, step "run `make generate`" in "Modifier le prompt ou un schéma" of `docs/prompt/README.md`; verify `make check-docs`

## 3. Contract

- [ ] 3.1 Write the shared components of `api/openapi.yaml`: `Item`, `ItemSummary`, `ItemPage`, `Asset`, `Category`, `ContentType`, `Tag`, request bodies, `Review`, `Stats`, `SyncPage`, `Tombstone`, the shared responses, following design D2 and D3 and the glossary names; verify `make lint-api`
- [ ] 3.2 Write the capture, item, progress, analyze, retry and asset operations (design D4), with their provisional status codes and problem types; verify `make lint-api`
- [ ] 3.3 Write the review, categories, content types, tags, sync (design D6), export and stats operations; verify `make lint-api`
- [ ] 3.4 Write the problem type catalogue (design D5) and the conventions (binding operations, provisional codes, nullability, identifiers, `fiche.template`) in the contract description; verify `make lint-api`
- [ ] 3.5 Regenerate; verify `make generate && make check-generated`, `go build ./...` and `go mod tidy` without diff

## 4. Backend: stubs and conventions

- [ ] 4.1 Write the failing tests of the `api-conventions` scenarios on the real server and a real SQLite (401, bearer case, unknown path without token, healthz 200/503, problem shape, 501, 404 unknown path and wrong method, 400 for a bad parameter and a malformed body), each named after its scenario; verify they fail where the behaviour is missing
- [ ] 4.2 Implement the `notImplemented` stubs and the `errNotImplemented` mapping to the `501` problem, logged at DEBUG (design D7); verify the tests of 4.1 pass
- [ ] 4.3 Write the test "every declared operation answers 501 until implemented": one valid request per operation of the bundled contract, read from `api/openapi.bundled.gen.yaml` so that a new operation without a case fails the test; verify it passes
- [ ] 4.4 Write the test that every fiche example of `docs/prompt/schemas/examples/` decodes into the generated `Fiche` union and that `ValueByDiscriminator` returns the type of its template; verify it passes
- [ ] 4.5 Adjust the existing `httpapi` tests (log test: `GET /api/items/{id}` now matches a declared route); verify `make check-backend`

## 5. App

- [ ] 5.1 Add a type-level check that each fiche example is assignable to `components["schemas"]["Fiche"]` and narrows by `template`; verify `make typecheck-app` passes, and fails on a deliberately wrong template value
- [ ] 5.2 Verify the existing app tests and lint still pass on the regenerated types: `make check-app`

## 6. Documentation alignment

- [ ] 6.1 Align `docs/spec-v1.md`: "API REST" table on the contract (paths with `/{id}`, content types by key, sync token), "Modèle de données" (change sequence, `client_id`, tombstones and `updated_at` for tags and content types, integer tag ids, RG-36 fields); verify `make check-docs`
- [ ] 6.2 Align `docs/backlog.md`: E3-05 (RG-36 amended), E4-09 (sync token, `sync-expired`), E5-04 (no "fiche incomplète" mention, "fiche trop longue" failure), E2-01/E2-03 (RG-07, RG-08); verify `make check-docs`
- [ ] 6.3 Align the tasks rule of `openspec/config.yaml` on the OpenSpec instruction (each group lands its own tests and docs; final group for integration only); verify `make check-openspec`

## 7. Integration

- [ ] 7.1 `make check` green; `code-reviewer` pass on the uncommitted diff without 🛑 nor 🔴
- [ ] 7.2 `/opsx:verify` on the change
- [ ] 7.3 `/dod-check` passes (make check green, definition of done in docs/dev/workflow.md)
