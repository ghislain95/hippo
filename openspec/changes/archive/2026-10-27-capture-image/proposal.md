# Proposal

## Why

A screenshot of a recipe or a photo of a whiteboard is the second most common capture of the V1 scenarios (`docs/metier.md` §2), and Hippo still answers `501` to a multipart capture. This change implements backlog story **E2-04 — Capturer une image**, the last capture form.

## What Changes

- `POST /api/items` with a `multipart/form-data` body holding one `image` file creates an `image` item owned by the authenticated user, status `queued`, read status `to_read`, and one asset, and answers `201` with the item and its asset (RG-01, RG-90).
- The image must be JPEG, PNG, HEIC or WebP, recognised from its content, not from the type the client declares, and 20 MB at most (20 × 1024 × 1024 bytes): otherwise `400 unsupported-image` or `413 image-too-large`, and nothing is stored (RG-02).
- The other form fields follow the JSON rules, in any order: `user_note` (trimmed, blank means absent, RG-09), `is_private` (exactly `true` or `false`), `client_id` (RG-08) and `source`. An undeclared field, a second file or a malformed value answers `400 about:blank`. The multipart body is bounded to 21 MiB (`413 about:blank`) and each text field to 1 MiB; an upload has 5 minutes, so a 20 MB image on a slow mobile link is not cut.
- Until analysis, the title of an image is the first line of its capture note, cut at 80 characters, or else « Image du 27 octobre 2026 », the capture day in Paris time (RG-26 extended on 2026-10-27).
- A replayed `client_id` answers `200` with the existing item and stores no new file (RG-08). An image is never a duplicate (RG-04).
- The file is stored on disk in the data volume, unchanged, metadata included (ADR-0004); `GET /api/assets/{asset_id}` returns it to its owner with its type, and `404` to anyone else.
- Width and height are filled for JPEG and PNG, null for HEIC and WebP (no new dependency).

## Non-goals

- Removing metadata (EXIF, GPS position) from what is sent to the AI, and converting HEIC before analysis: decided with Ghislain on 2026-10-27 to settle in E3-01, noted in its backlog entry. The stored file stays as captured.
- Thumbnails, resizing, re-encoding.
- Deleting images (deletion story), backups of the image folder (E6-03).
- The share extension's image format (E5-02).

## Capabilities

### New Capabilities

- `image-assets`: serving a stored image through `GET /api/assets/{asset_id}`.

### Modified Capabilities

- `item-capture`: image captures are created instead of answering `501`; the provisional title covers images.

## Impact

- **Story**: E2-04.
- **Business rules**: RG-01, RG-02, RG-03, RG-04, RG-08, RG-09, RG-10, RG-12, RG-26 (extended on 2026-10-27, decided with Ghislain), RG-90.
- **ADRs**: ADR-0004 (images on disk, referenced by `assets`), ADR-0018 (images cached on demand), ADR-0019 (data volume), ADR-0022 (user_id everywhere), ADR-0032 (SQL migration), ADR-0033 (tests), ADR-0035 (ULID for images).
- **AI cost**: none.
- **Private items**: `is_private` stored; no path reaches the AI. The image keeps its metadata on the NAS; what leaves it is decided in E3-01.
- **Persisted data**: new `assets` table (SQL migration `00004_assets.sql`) and image files under `DATA_DIR/assets`, written before the transaction so that no row ever points to a missing file.
- **API contract**: details of the multipart body (fields, one file, limits, order of checks), `getAsset` response headers (`Cache-Control`, `X-Content-Type-Options`) and owner rule, `Asset.mime_type` as an enum; no new operation.
- **Prompt and fiche schemas**: none.
- **Code**: `backend/internal/capture`, `backend/internal/store`, `backend/internal/httpapi`, new leaf package `backend/internal/assetfs` (image files), `backend/cmd/hippo` (assets directory).
- **Dependencies**: none new (`image/jpeg`, `image/png` from the standard library).
- **User QA: no** (nothing visible in the app yet).

## Open questions

None left for this change; the metadata and HEIC questions belong to E3-01.
