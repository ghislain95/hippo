# item-extraction Specification

## Purpose
Describes what happens to a captured item between `queued` and `extracted`: the local extraction of a link's page without AI, its fallback, and the immediate passage of notes and images, as observed on the stored item.

## Requirements

### Requirement: Every captured item reaches extracted without blocking
After a capture, and at start-up for any item left `queued`, the backend SHALL extract the item in the background, without any AI call, and set its `status` to `extracted` with `extracted_at`, whatever the outcome of the page; a capture SHALL never wait for it. Only a stop of the backend or a network outage on the NAS side leaves an item `queued`, to be retried; a link that alone keeps failing on the network, while other links succeed, falls back at its third such failure (RG-20, RG-23).

#### Scenario: A captured link becomes extracted
- **GIVEN** a page served with a title and an article
- **WHEN** the user captures its URL
- **THEN** the capture answers `201` with `status` `queued`, and the stored item later has `status` `extracted` and an `extracted_at`

#### Scenario: Items left queued are extracted at start-up
- **GIVEN** a link item still `queued` when the backend stops
- **WHEN** the backend starts again
- **THEN** the item becomes `extracted`

#### Scenario: A stop during a download leaves the item queued
- **GIVEN** a page that answers slowly
- **WHEN** the backend is stopped while downloading it, then started again
- **THEN** the item was not written as a fallback and becomes `extracted` after the restart

#### Scenario: A NAS network outage leaves items queued
- **GIVEN** the NAS cannot resolve any host name
- **WHEN** the user captures a link
- **THEN** the item stays `queued`, and becomes `extracted` once the network is back

#### Scenario: A link that keeps failing on the network falls back
- **GIVEN** a link whose resolution fails on the NAS side at every pass, while another link succeeds between passes
- **WHEN** three passes have taken place
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback`

#### Scenario: A NAS outage never makes a link fall back
- **GIVEN** no link can succeed during 5 passes
- **WHEN** the five passes have taken place
- **THEN** the item stays `queued`

#### Scenario: A page that crashed the backend is not fetched again
- **GIVEN** a link whose extraction had started when the backend stopped abruptly
- **WHEN** the backend starts again
- **THEN** the link is not downloaded again and becomes `extracted` with `extraction_mode` `fallback`

#### Scenario: Private links are extracted too
- **WHEN** the user captures a link marked private
- **THEN** its page is read and the item becomes `extracted` with `extraction_mode` `full` (RG-92)

#### Scenario: Notes and images are extracted at once
- **WHEN** the user captures a note, then an image
- **THEN** both items become `extracted` with `extraction_mode` `none`, without any network request

### Requirement: A web page is read with its main content
For a link to an HTML page answered with a success status, the extraction SHALL keep the page title as the item `title`, the site name (`og:site_name`, else the page's, else the host name) as `site_name`, the main content as plain text, `extraction_mode` `full`, and a reading time of the content's word count divided by 230, rounded up, at least 1 (RG-21, RG-25, RG-26).

#### Scenario: Article page
- **GIVEN** a recorded fictional article page titled `Tuning SQLite` with `og:site_name` `Example Blog`, a menu, a comment section and an article of 920 words
- **WHEN** the user captures its URL
- **THEN** the item has `title` `Tuning SQLite`, `site_name` `Example Blog`, `extraction_mode` `full`, `reading_minutes` 4, and its content holds the article text but neither the menu nor the comments

#### Scenario: Page without site name
- **GIVEN** a recorded article page without `og:site_name` nor site name
- **WHEN** the user captures `https://blog.example.com/post`
- **THEN** the item's `site_name` is `blog.example.com`

#### Scenario: Page in another charset
- **GIVEN** a recorded page encoded in ISO-8859-1 with the title `Crème brûlée`
- **WHEN** the user captures its URL
- **THEN** the item's `title` is `Crème brûlée`

### Requirement: Long content is truncated and marked
Content longer than 120 000 characters SHALL be cut at a word boundary at most 120 000 characters long and the item marked `content_truncated`; shorter content SHALL be kept whole (RG-24). This applies to the description kept by a fallback as to the main content of a page.

#### Scenario: Very long article
- **GIVEN** a recorded page whose main content is 200 000 characters long
- **WHEN** the user captures its URL
- **THEN** the item has `content_truncated` true and its content is at most 120 000 characters long and ends on a whole word

#### Scenario: Normal article
- **WHEN** the user captures the URL of an article of 5 000 characters
- **THEN** the item has `content_truncated` false and its content holds the whole article

#### Scenario: Very long description in a fallback
- **GIVEN** a page answered `200` whose only text is an `og:description` of 2 000 000 characters
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `fallback`, `content_truncated` true, `reading_minutes` null, and its content is at most 120 000 characters long and ends on a whole word

### Requirement: A failed extraction falls back
When the page cannot be read — no answer within 15 s in total, an HTTP error status, a body over 2 MB, a body that is not HTML, an empty or too short main content, a page whose reading needs more memory or time than allowed — the item SHALL still become `extracted` with `extraction_mode` `fallback`: its title is `og:title`, else the page `<title>`, else the provisional title, `og:description` read only on a success status; `reading_minutes` SHALL stay null, and a description longer than 120 000 characters is cut and marked `content_truncated` as content is (RG-24). A link never gets `extraction_mode` `none`, which is kept for notes and images (RG-23, RG-26).

#### Scenario: Page that does not answer in time
- **GIVEN** a page that answers after 20 s
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` and its provisional title, within about 15 s

#### Scenario: Error status ignores the page metadata
- **GIVEN** a page answered `404` whose body holds `og:title` `Page not found`
- **WHEN** the user captures `https://example.com/gone`
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` and keeps the title `example.com`

#### Scenario: Page whose content needs JavaScript
- **GIVEN** a recorded page with an empty body but `og:title` `App` and `og:description` `Loading`
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` and `title` `App`

#### Scenario: Fallback keeps the description as content
- **GIVEN** a recorded page answered `200` whose only text is `og:description` `A short summary`
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `fallback`, its content is `A short summary` and `reading_minutes` is null

#### Scenario: Page too heavy to read
- **GIVEN** a page of about 200 KB built so that its tree needs more memory than the reading is allowed
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback`, and the backend keeps answering

#### Scenario: Oversized page
- **GIVEN** a page whose body exceeds 2 MB, compressed or not
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` without the body being read beyond the limit

### Requirement: Local and private addresses are never fetched
The extraction SHALL never connect to a loopback, private, link-local, unspecified, multicast, Tailscale (`100.64.0.0/10`) or IPv6 unique-local address, whether the captured URL names it directly, its host resolves to it, or a redirect leads to it; such a link SHALL become `extracted` with `extraction_mode` `fallback` and its provisional title (RG-27).

#### Scenario: Link to the home network
- **WHEN** the user captures `http://192.168.1.1/admin`
- **THEN** no connection is opened and the item becomes `extracted` with `extraction_mode` `fallback`

#### Scenario: Host resolving to a local address
- **GIVEN** a host name that resolves to `127.0.0.1`
- **WHEN** the user captures a URL on that host
- **THEN** no connection is opened and the item becomes `extracted` with `extraction_mode` `fallback`

#### Scenario: Redirect to a private address
- **GIVEN** a public page that redirects to `http://10.0.0.5/`
- **WHEN** the user captures its URL
- **THEN** the redirect is not followed and the item becomes `extracted` with `extraction_mode` `fallback`

### Requirement: YouTube and PDF links wait for their own extraction
Until their stories (E2-06, E2-08), a link whose host is `youtube.com`, a subdomain of it or `youtu.be` SHALL stay `queued` without being downloaded, and a link answered as a PDF SHALL stay `queued` without content and SHALL not be downloaded again, so that their later extraction loses nothing (RG-63).

#### Scenario: YouTube link
- **WHEN** the user captures `https://www.youtube.com/watch?v=abc` or `https://youtu.be/abc`
- **THEN** no request is sent and the item stays `queued`

#### Scenario: PDF link
- **GIVEN** a URL answered with `Content-Type: application/pdf`
- **WHEN** the user captures it, and the backend restarts
- **THEN** the item stays `queued` and the URL was requested only once

### Requirement: Extraction keeps content out of the logs
The logs of the extraction SHALL contain neither the URL, nor the host, the title or the content of the page; only the item id, the extraction mode and durations (RG-90, ADR-0027).

#### Scenario: Extraction logs no content
- **WHEN** the user captures `https://secret.example.com/a` whose page is titled `Private thought`
- **THEN** no log line contains `secret.example.com` nor `Private thought`
