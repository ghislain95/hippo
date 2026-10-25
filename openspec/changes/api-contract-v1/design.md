# Design: api-contract-v1

## Context

See `proposal.md` for the motivation. Current state:

- `api/openapi.yaml` declares `GET /healthz` and the shared components (bearer security, `Problem`, cursor parameters). `make generate` runs `go generate ./...` (oapi-codegen 2.8.1 strict server into `backend/internal/httpapi/api.gen.go`, `schemabundle` into `backend/internal/analysis/schema/`) then openapi-typescript 7.13.0 into `app/src/api/schema.gen.ts`.
- The generated code imports only the standard library today; request bodies and unions will make it import `github.com/oapi-codegen/runtime`.
- oapi-codegen cannot follow a `$ref` to a JSON Schema file (`unrecognized external reference`); `redocly bundle` resolves them, but copies `$id`/`$schema` (which breaks a 2020-12 validator by changing the reference base) and names components after file names (`tech_article.schema`).
- Experiments of the framing and of the adversarial pass (scratchpad, not committed) fixed the generator behaviours this design relies on: property-level nullability works on both sides; a named nullable schema does not (Go); `type: [object, "null"]` combined with a `oneOf` gives an unusable TypeScript type; a discriminator without an explicit `mapping` uses component names as values on both sides.

## Goals / Non-Goals

**Goals:**

- One hand-written contract, one source per fact: the fiche format stays in `docs/prompt/schemas/`.
- Generated Go and TypeScript that compile, are usable without casts, and are checked up to date in CI whenever any of their sources changes.
- A backend that serves the whole surface with `501` until each story implements its part, without editing the contract or a shared file to do so.

**Non-Goals:**

- Runtime validation of responses against the contract (a later change may add a test that does it; the bundle is kept valid for that use).
- Choosing the React Native multipart serialisation (E5).

## Decisions

### D1 — Generation pipeline

```
docs/prompt/schemas/              api/openapi.yaml (hand-written)
  classify-item + fiches/            |  $ref ./fiches.gen.json#/components/schemas/Fiche
        |                            |
   schemabundle ------------------> api/fiches.gen.json   (OpenAPI components, generated)
        |                            |
        v                      redocly bundle
 backend/internal/analysis/          |
 schema/classify-item.bundled.json   v
                               api/openapi.bundled.gen.yaml (self-contained, generated)
                                 |                     |
                           oapi-codegen          openapi-typescript
                                 v                     v
               backend/internal/httpapi/api.gen.go   app/src/api/schema.gen.ts
```

- `schemabundle` gains a second output, `-openapi-out api/fiches.gen.json`: one component per fiche (`Fiche<PascalCaseKey>`: `FicheTechArticle`, `FicheGeneric`…), one per common definition (`FicheText`, `FicheTextList`, `FicheCodeBlock`, `FicheSection`, `FicheLink`), internal references rewritten to `#/components/schemas/...`, `$id` and `$schema` removed, `type: string` added next to each `template` `const` (otherwise Go types it `interface{}`), and a `Fiche` component: `oneOf` of the fiche components with `discriminator: {propertyName: template, mapping: {<const>: <component>}}` derived from the `template` constants. The mapping can therefore never drift from the schemas; a test asserts it covers every fiche file.
- `api/openapi.yaml` references `./fiches.gen.json#/components/schemas/Fiche` only. `redocly bundle` copies the referenced components under their own names into `api/openapi.bundled.gen.yaml`, which both generators read.
- `make generate` runs, in order: `schemabundle` (both outputs), `redocly bundle`, oapi-codegen, openapi-typescript. The `go:generate` directives are removed: the order spans Go and Node, so the Makefile owns it. Every output is committed, listed in `GENERATED`, marked `linguist-generated` and protected by the hook.
- **Alternatives**: wrapper components pointing at the fiche files (`FicheTechArticle: {$ref: ../docs/...}`) and a Redocly decorator stripping `$id` — two mechanisms instead of one, and the mapping written by hand. A free-form `fiche: object` — loses the typing ADR-0006 asks for.

### D2 — Nullability and PATCH semantics

- Scalar: `type: [T, "null"]` on the property (Go `*T` or `nullable.Nullable[T]`, TS `T | null`).
- Reference: `anyOf: [{$ref: ...}, {type: "null"}]`.
- Fiche: not required and omitted when absent (`fiche?:` in TS); never `type: [object, "null"]` with the `oneOf`.
- Never a named nullable schema (oapi-codegen renders it non-nullable).
- oapi-codegen `output-options.nullable-type: true` with `github.com/oapi-codegen/nullable`: a `PATCH` handler can tell an absent field (no change) from an explicit `null` (for example `category_id: null`, back to À trier). **Alternative**: a dedicated `unsorted: true` field — one special case per nullable field, and the contract would lie about JSON merge semantics.

### D3 — Identifiers and references

- ULID (`TEXT`, 26 characters) for items, assets and batches; integer for categories and tags (the model copies category ids in `classify_item`; short ids save tokens); content types are named by their `key` everywhere in the API (it is how `classify_item`, the filters and the templates name them, and keys are global per ADR-0022).
- An item references its category by `category_id`, its type by `content_type` (key) and its tags by `tag_ids`; it never embeds names or the category path, so that renaming a category or a tag does not leave cached items stale (ADR-0018). The app resolves them from the synced categories and tags.
- An item exposes `schema_version` (RG-64): the app renders a fiche by `fiche.template`, never by the item's content type, because the two differ after a type correction (RG-60), a type merge (RG-72) or for a proposed type.

### D4 — Capture

- `POST /api/items` accepts `application/json` (`CaptureRequest`: `url` or `note`, optional `user_note`, `is_private`, `client_id`) and `multipart/form-data` (`image` binary, optional `user_note`, `is_private`, `client_id`).
- `201` returns a new item; `200` returns the existing item for a duplicate link (RG-04) or a replayed `client_id` (RG-08).
- The JSON body schema does not encode the exclusivity of RG-07 with `oneOf` (generators turn it into an opaque union); the handler of E2-01/E2-03 enforces it and answers `ambiguous-capture`. The contract documents it.
- The multipart body is handed to the handler as a raw reader by the strict server: E2-04 wraps it in `http.MaxBytesReader` (20 MB, `image-too-large`) and pins the field names in its tests.

### D5 — Problem type catalogue

| Type (`urn:hippo:problem:…`) | Status | Rule | Raised by |
| --- | --- | --- | --- |
| `invalid-url` | 400 | RG-02 | capture |
| `empty-note` | 400 | RG-02 | capture |
| `empty-capture` | 400 | RG-02 | capture with neither URL, note nor image |
| `ambiguous-capture` | 400 | RG-07 | capture |
| `unsupported-image` | 400 | RG-02 | capture |
| `image-too-large` | 413 | RG-02 | capture |
| `private-item` | 409 | RG-12, RG-33 | analyze |
| `budget-exceeded` | 409 | RG-81 | analyze |
| `not-ready` | 409 | RG-33 | analyze while `queued` |
| `already-submitted` | 409 | RG-33 | analyze while `submitted` |
| `not-failed` | 409 | RG-35 | retry |
| `name-taken` | 409 | RG-54, RG-74 | create or rename a category, rename a tag |
| `category-too-deep` | 400 | RG-51 | create or move a category |
| `category-not-empty` | 409 | E4-07 | delete a category |
| `invalid-merge-target` | 400 | RG-72, RG-74 | merge into itself or into one's own subcategory |
| `type-not-proposed` | 409 | RG-72 | merge a content type that is not `proposed` |
| `sync-expired` | 410 | ADR-0018 | sync with a token older than the tombstone retention |
| `not-implemented` | 501 | this change | any declared operation, format or field not implemented |

`400` for a request that breaks the contract, `401`, `404` (unknown route, or an item of another user, RG-90) and `415` use `about:blank`. The catalogue lives in the description of `api/openapi.yaml` (one place); each operation lists the types it may return, provisionally until its story.

### D6 — Sync

- `GET /api/sync?sync_token=&cursor=`: without `sync_token`, everything; with it, the changes since it. Each page holds `items`, `categories`, `content_types`, `tags` and `deleted` (`{items, categories, tags, content_types}` of `{id, deleted_at}`, ids as strings for items and integers or keys for the others), plus `next_cursor`; only the last page carries the new `sync_token`. Rows come in change order.
- The token encodes a per-user, per-row **change sequence** (a monotonic integer bumped on every write), not a timestamp: SQLite has a single writer, so the sequence is cheap and immune to same-millisecond writes and clock jumps. The tables are created by E2 to E4 with it; the spec's data model says so.
- A token older than the 30-day tombstone retention answers `410 sync-expired`; the app drops its cache and resyncs without a token.
- Tombstones carry only `{id, deleted_at}`: the fiche, extracted content, notes and images are erased at deletion (RG-79).
- Items carry their assets metadata (`id`, MIME type, width, height, size) so that an image item reads offline; images are fetched on demand.

### D7 — 501 stubs

- `handlers` embeds a `notImplemented` struct that implements every method of `StrictServerInterface` by returning the sentinel `errNotImplemented`. A story implements an operation as a method of `handlers` in its own file, which shadows the stub; it deletes the stub method in the same change.
- `ResponseErrorHandlerFunc` maps `errNotImplemented` to the `501` problem and logs it at `DEBUG`, not `ERROR`; any other error keeps the `500` path.
- A story that implements part of an operation (a format, a field) returns `errNotImplemented` for the rest.
- **Alternative**: a `501` response declared on every operation of the contract — 30 responses to add, then to remove story by story.

### D8 — Unknown method

A wrong method on a declared path falls through to the catch-all and answers `404` (spec), like an unknown path. Go's `ServeMux` would answer `405` only without the catch-all, in plain text; keeping one problem body for every unknown route is simpler for the app.

### D9 — ADR-0035

D1 (generated fiche components and bundle step), D3 (identifiers) and D6 (sync token, change sequence, tombstones, `sync-expired`) make ADR-0017 and ADR-0018 precise; they go into ADR-0035, which those ADRs reference by their status line only.

## Risks / Trade-offs

- [Incompatible fiche schema change becomes an API break] → ADR-0035 states it: a `x-schema-version` bump that changes a component reanalyses the type before the app ships, or adds a versioned component; `make check-generated` now runs when a fiche schema changes (CI `api` filter).
- [Route-level codes are provisional] → each story's change amends `openapi.yaml` first and its tests pin the codes; the catalogue is the only binding list.
- [`nullable-type` retypes every nullable field in Go] → accepted: handlers are written against it from the first story.
- [Long responses (export zip, synchronous analysis) exceed the 60 s server write timeout] → E3-06 and E4-10 set per-route deadlines with `http.ResponseController`.
- [The bundle is large and regenerated on every fiche change] → generated files are collapsed in PR diffs and protected from hand edits.

## Migration Plan

None: only `/healthz` is served today and it is unchanged. Rollback is a revert of the merge commit.
