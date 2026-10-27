# Design

## Context

See `proposal.md`. Since E2-03, `POST /api/items` handles JSON links and notes; a multipart body reaches `CreateItem` as a `*multipart.Reader` created by the generated code without any limit (`api.gen.go`, `r.MultipartReader()`), and answers `501`. The strict handler sees neither `http.ResponseWriter` nor `*http.Request`. `body.go` bounds and checks JSON bodies only. The server has global 60 s read and write timeouts (`cmd/hippo/main.go`). Write transactions take the SQLite write lock at `BEGIN` (`_txlock=immediate`, `store.go`). `getAsset` is a `501` stub; `itemFromStore` returns `assets: []`.

## Goals / Non-Goals

**Goals:**
- No upload can exhaust the NAS memory or disk, nor hold the database write lock.
- No database row ever points to a missing file; an orphan file after a crash is acceptable.

**Non-Goals:**
- Range requests and validators (`ETag`) on `getAsset`: the strict handler cannot see the request headers, and an asset is immutable.
- Orphan file sweeping beyond temporary files, deletion of image files (deletion story), backups (E6-03).
- Correct orientation of `width`/`height` for EXIF-rotated photos (`image.DecodeConfig` ignores EXIF): left to the analysis stories.

## Decisions

### Packages and contract touched

| Area | Change |
| --- | --- |
| `api/openapi.yaml` | `createItem`: order of the multipart checks, `413 about:blank` for a body over 21 MiB, `image-too-large` for the file; `CaptureImageRequest` fields described (exact `true`/`false`, one file); `getAsset`: response headers `Cache-Control` and `X-Content-Type-Options`, owner only; `Asset.mime_type` an enum of the four types. Then `make generate`. |
| `backend/internal/capture` | `image.go`: format detection, `DecodeConfig` for JPEG/PNG, image title (RG-26), image path calling the store. |
| `backend/internal/store` | `00004_assets.sql`; `assets.go` (insert in the capture transaction, read by id for a user, list for an item); `items.go`: the item read back carries its assets. |
| `backend/internal/assetfs` (new, leaf) | The image files: write a temporary file under `DATA_DIR/assets`, `fsync`, rename, `fsync` the directory, remove, open for reading through `os.Root`. A package of its own because both `capture` and `httpapi` use it and it knows neither HTTP nor the database. |
| `backend/internal/httpapi` | `body.go`: bound and deadlines for multipart on `POST /api/items` and for `GET /api/assets/{asset_id}`; multipart parsing into a capture request; `GetAsset`; `nosniff` header; `Item.assets` mapping; problem mapping. |
| `backend/cmd/hippo` | Creates `DATA_DIR/assets` at start-up (fails early if the user cannot write), removes leftover temporary files, wires `assetfs`. |

Prompt and fiche schemas: unchanged.

### Migration `00004_assets.sql` (new file, SQL)

`CREATE TABLE assets (id TEXT PRIMARY KEY, item_id TEXT NOT NULL, user_id TEXT NOT NULL REFERENCES users(id), path TEXT NOT NULL, mime_type TEXT NOT NULL CHECK (mime_type IN ('image/jpeg','image/png','image/heic','image/webp')), width INTEGER, height INTEGER, size_bytes INTEGER NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id)) STRICT`, with `CREATE UNIQUE INDEX items_id_user ON items (id, user_id)` (required by the composite foreign key, so an asset can only belong to an item of the same user, RG-90) and `CREATE INDEX assets_item ON assets (item_id)`. `path` is relative to `DATA_DIR/assets`, built only from server identifiers (`<user_id>/<asset_id>.<ext>`). Assets are not a synced table of their own: they travel inside `Item` (`SyncPage.items`), and they are inserted in the transaction that advances the item's `change_seq`; any later change of an item's assets must advance it too. `assets` is not tied to `kind = image`: RG-23 plans a preview image for links.

### Bounding the upload (B1, B3)

The middleware of `body.go`, for `POST /api/items` with `multipart/form-data`:

- refuses at once a declared `Content-Length` above 21 MiB, and wraps the body in `http.MaxBytesReader(21 MiB)`;
- sets a 5-minute read deadline through `http.ResponseController(w).SetReadDeadline` (`statusRecorder` already exposes `Unwrap`), so a 20 MiB image on a slow mobile link is not cut by the global 60 s; the same middleware sets a 5-minute write deadline on `GET /api/assets/{asset_id}`.

While parsing: each text part is read with a 1 MiB cap; the `image` part is copied to disk through `io.LimitReader(20 MiB + 1)`, so a file over 20 MiB is detected without reading on. Hitting the 21 MiB body limit answers `413 about:blank`; a file over 20 MiB answers `413 image-too-large`.

### Order of the checks (C1)

Parts may come in any order (`FormData` guarantees none), so the parser reads every part first, then checks; the answer never depends on the order:

1. `401` (`requireAuth`); declared `Content-Length` over 21 MiB → `413 about:blank`.
2. Body over 21 MiB while reading → `413 about:blank`; `image` file over 20 MiB → `413 image-too-large`.
3. Structure → `400 about:blank`: unknown or repeated field (a `url` or `note` field is just an undeclared field in multipart, not `ambiguous-capture`), second file, missing `image`, text field over 1 MiB, invalid UTF-8, `is_private` other than exactly `true` or `false` (no `strconv.ParseBool`, which accepts `1` and `t`).
4. `client_id` (ULID, canonical form) and `source` → `400 about:blank`, as in JSON.
5. Format → `400 unsupported-image`.
6. Trim `user_note` (RG-09); replay or insert.

A client disconnect or a deadline hit while reading answers `400 about:blank`, logged without the error text (it may name the part).

### Format detection (C4, C5)

From the first bytes of the stored file, bounded:

- JPEG: `FF D8 FF`; PNG: the 8-byte PNG signature. Both then go through `image.DecodeConfig` (`image/jpeg`, `image/png`): a failure is `unsupported-image`, not null dimensions (so a truncated or polyglot header is refused). Arithmetic-coded JPEG, unsupported by `image/jpeg`, is refused too (rare).
- WebP: `RIFF`, a size, `WEBP`, then a `VP8 `, `VP8L` or `VP8X` chunk.
- HEIC: an `ftyp` box read within its declared size (refused if truncated), major brand `heic`, `heix`, `heim` or `heis`, or major brand `mif1`/`msf1` with `heic` or `heix` among the compatible brands (an AVIF is refused).
- HEIC and WebP are not decoded: a truncated one is accepted and will fail at analysis (E3-01).

The declared part `Content-Type` and file name are ignored, never stored, never logged.

### File then transaction (B2)

1. Before any transaction: copy the `image` part to a temporary file `DATA_DIR/assets/.tmp-<random>` (same file system as the destination, no `EXDEV`), mode `0640`; directories `0750`.
2. After parsing and checks: `fsync` the file, rename it to `<user_id>/<asset_id>.<ext>`, `fsync` the directory.
3. Capture transaction (replay → insert): item, `capture_ids` row, `assets` row, commit.
4. On every path that does not end with a committed new asset — a check failing after the file was written, a replay (`created = false`), the `capture_ids` safety net, any error — the file is removed.

There is never a rename after the commit, so a row never points to a missing file. A crash between the rename and the commit, or a commit lost on power cut (`synchronous(NORMAL)`), leaves an orphan file: accepted; start-up removes `.tmp-*` files only. Concurrent replays of one `client_id` each write their own file; the serialised transaction keeps one item, and the losers remove their file.

### Replay

Through `capture_ids` as for links and notes (RG-08): the replay answers `200` with the item and its existing asset, and its uploaded file is removed.

### Serving an asset

`GetAsset` reads the asset row by `id` and `user_id` (`404` otherwise; the path parameter is never used to build a path), opens `path` through `os.Root` on `DATA_DIR/assets` (no traversal by construction, Go ≥ 1.24, `go.mod` 1.27.3), and streams it with `Content-Type` = stored `mime_type`, `Content-Length`, and `Cache-Control: private, max-age=31536000, immutable` declared in the contract. `X-Content-Type-Options: nosniff` is set by a middleware on every response. A missing file for an existing row answers `500` and is logged with the asset id only.

### Title (RG-26)

The first non-empty line of the trimmed capture note, cut at 80 code points with `…` (the note rule of E2-03), or else `Image du <day> <month> <year>` with French month names and `1er` for the first day. The day is the server's reception day in Paris (`capture`'s Paris location and the store's clock, as for E2-02 notes): an offline capture sent the next day carries the next day.

### user_id, private items and budget

Every query on `assets` filters by `user_id`; the composite foreign key ties an asset to an item of the same user. Private items: stored, image served to its owner, no path reaches the AI. Budget: no AI call.

### Logs

No log line carries the capture note, the client file name, the part names' values or a path built from them; errors while reading the body are logged by kind only.

## Risks / Trade-offs

- [Orphan files after a crash] → harmless on disk; a sweep can come with the deletion story.
- [A 413 sent mid-upload may reach iOS as a network error, and an offline capture (RG-06) would retry it] → the app and the extension check the size before sending (E5-02, E5-04), noted in their backlog entries.
- [Portrait photos report swapped dimensions] → dimensions are informative; analysis stories may correct them.
- [Backups] → E6-03 copies the database (`VACUUM INTO`) then the image folder, excluding `.tmp-*`: an orphan file is possible, a row without file is not.

## Migration Plan

`00004_assets.sql` runs at start-up (ADR-0032); `DATA_DIR/assets` is created by `main`. Rollback is the backup (ADR-0020).
