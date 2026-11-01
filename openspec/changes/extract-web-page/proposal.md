# Proposal

## Why

Every captured item stays `queued` forever: nothing extracts a link's page, and the nightly analysis (E3) only takes `extracted` items. Extracting the main text locally, without AI, is what makes the analysis cheap and a later re-analysis possible (ADR-0008). This change implements backlog story **E2-05 — Extraire une page web**.

## What Changes

- An extraction worker runs inside the backend, in the background, right after a capture and again at start-up for anything left `queued`; no AI call (RG-20).
- For a link, the page is downloaded once (15 s in total, redirects included, 5 MB of body at most) and read with Readability: title, site name and main content (RG-21). The item becomes `extracted` with `extraction_mode` `full`, the page title replaces the provisional title (RG-26), the site name is filled, the reading time is computed at about 230 words per minute (RG-25), and the content is kept up to about 120 000 characters, cut and marked `content_truncated` beyond (RG-24).
- On failure — timeout, HTTP error status, empty or too short content, a page that is not HTML — the item still becomes `extracted` with `extraction_mode` `fallback`, keeping what it could read (`og:title`, `og:description` on a success status) (RG-23). A link never gets `none`, which the contract and the prompt keep for notes and images. There is no blocking extraction failure.
- A link whose address is local or private — loopback, private networks, link-local, the Tailscale range, IPv6 local ranges — is never fetched, including through a redirect: fallback without content (RG-27, new, decided with Ghislain on 2026-11-01).
- Private items are extracted too: the extraction requests the captured URL from its own site and sends nothing else (RG-92, exception added with Ghislain on 2026-11-01). The NAS presents itself honestly (`Hippo/<version>` User-Agent, decided with Ghislain).
- YouTube links (E2-06) and links answered as PDF (E2-08) stay `queued` until their stories, so that nothing is lost (RG-63; decided with Ghislain on 2026-11-01).
- A stop of the backend or a network outage on the NAS side leaves the item `queued` for a later attempt; a link whose extraction was under way when the backend crashed is not fetched again and falls back, so a hostile page cannot crash the service in a loop.
- A note or an image has nothing to extract: it becomes `extracted` at once, `extraction_mode` `none`.
- Each extraction writes the item once, advancing `updated_at` and the modification sequence (ADR-0035).

## Non-goals

- PDF text extraction: new story E2-08, framed right after this one with a comparison of libraries (decided with Ghislain on 2026-11-01).
- YouTube metadata and transcripts (E2-06): a YouTube link stays `queued` until then.
- Retrying an extraction that fell back, JavaScript rendering, persistent cookies or logins, the preview image of RG-23 (no asset is created).
- Any API change beyond the fields already in `Item`.

## Capabilities

### New Capabilities

- `item-extraction`: what happens to a captured item between `queued` and `extracted`, observed through the item (status, extraction mode, title, site, reading time, truncation).

### Modified Capabilities

None.

## Impact

- **Story**: E2-05.
- **Business rules**: RG-11, RG-12, RG-20, RG-21, RG-23, RG-24, RG-25, RG-26, RG-27 (new), RG-63, RG-90, RG-92 (exception added).
- **ADRs**: ADR-0003, ADR-0004, ADR-0008 (extraction with go-readability: the maintained module is the Readeck fork, see below), ADR-0019 (resources of the NAS), ADR-0022, ADR-0033, ADR-0035.
- **Dependencies** (approved by Ghislain on 2026-11-01): `codeberg.org/readeck/go-readability/v2` pinned at v2.1.3 (the `go-shiori` module named by ADR-0008 was archived on 2025-12-30 and points to this fork) and `golang.org/x/net` (`html` for `og:*`, `html/charset` for the declared encoding, which pulls `golang.org/x/text`, already indirect). Licences MIT and BSD, compatible with the public repository.
- **AI cost**: none.
- **Private items**: extracted like the others (RG-92 exception); nothing is sent to the AI. Fetching a URL reveals the NAS public address to the site, as any browser would; a single-use link would be consumed.
- **Persisted data**: `items` columns already created by E2-01 (`status`, `extraction_mode`, `content_text`, `content_truncated`, `reading_minutes`, `site_name`, `title`, `extracted_at`); a new SQL migration for the non-synced table `extraction_attempts` (attempt started, outcome).
- **API contract**: none (fields already declared); `Item` never carries the extracted content.
- **Security**: outbound HTTP from the NAS to Internet addresses only (RG-27); bounded time and size.
- **Prompt and fiche schemas**: none.
- **User QA: no**.

## Open questions

None left.
