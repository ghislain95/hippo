# item-extraction Specification

## Purpose
Describes what happens to a captured item between `queued` and `extracted`: the local extraction of a link's web page, PDF or YouTube video metadata without AI, its fallback, and the immediate passage of notes and images, as observed on the stored item.

## Requirements

### Requirement: Every captured item reaches extracted without blocking
After a capture, and at start-up for any item left `queued`, the backend SHALL extract the item in the background, without any AI call, and set its `status` to `extracted` with `extracted_at`, whatever the outcome of the page; a capture SHALL never wait for it. Only a stop of the backend, a network outage on the NAS side, or a failure of the NAS itself (the reading process cannot start, no room to store a PDF) leaves an item `queued`, to be retried; a link that alone keeps failing on the network, while other links succeed, falls back at its third such failure (RG-20, RG-23).

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

#### Scenario: No room on the NAS for a PDF leaves the item queued
- **GIVEN** a URL answered as a PDF, and a NAS that cannot write the PDF's temporary file
- **WHEN** the user captures it
- **THEN** the item stays `queued` without being written as a fallback, and becomes `extracted` with `extraction_mode` `full` once the NAS can write again

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
When the page cannot be read — no answer within 15 s in total, an HTTP error status, an HTML body over 2 MB, a body that is neither HTML nor PDF, an empty or too short main content, a page whose reading needs more memory or time than allowed — the item SHALL still become `extracted` with `extraction_mode` `fallback`: its title is `og:title`, else the page `<title>`, else the provisional title, `og:description` read only on a success status; `reading_minutes` SHALL stay null, and a description longer than 120 000 characters is cut and marked `content_truncated` as content is (RG-24). A link never gets `extraction_mode` `none`, which is kept for notes and images (RG-23, RG-26). A PDF that cannot be read falls back as its own requirement says (RG-28). A YouTube video is always written with `extraction_mode` `fallback` and follows its own requirements instead: its title comes from its player data, else from oEmbed, else the provisional title, and its duration is its `reading_minutes` (RG-22).

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

### Requirement: Extraction keeps content out of the logs
The logs of the extraction SHALL contain neither the URL, nor the host, the title or the content of the page, nor, for a PDF, the file name of its URL, its metadata, the path of its temporary file or a message of the PDF reader; only the item id, the extraction mode, a reason class, the HTTP status and durations (RG-90, ADR-0027).

#### Scenario: Extraction logs no content
- **WHEN** the user captures `https://secret.example.com/a` whose page is titled `Private thought`
- **THEN** no log line contains `secret.example.com` nor `Private thought`

#### Scenario: PDF extraction logs no content
- **GIVEN** a PDF titled `Private thought` in its metadata at `https://secret.example.com/private-report.pdf`, and a damaged PDF at `https://secret.example.com/broken-plan.pdf`
- **WHEN** the user captures both
- **THEN** no log line contains `secret.example.com`, `private-report`, `broken-plan`, `Private thought`, nor the path of the temporary directory

### Requirement: A PDF is read with its text
For a link answered with a success status as a PDF (media type `application/pdf`, or a body starting with `%PDF-` whatever its declared type) of at most 50 MB, whose text holds at least 200 characters, the extraction SHALL keep that text as content with `extraction_mode` `full`, the host name as `site_name`, and a reading time of the document's words divided by 230, rounded up, at least 1 (RG-25, RG-26, RG-28).

#### Scenario: PDF article
- **GIVEN** a generated fictional PDF of 10 pages, titled `Tuning SQLite` in its metadata, holding 1 951 words, served with `Content-Type: application/pdf` at `https://docs.example.com/papers/tuning.pdf`
- **WHEN** the user captures its URL
- **THEN** the item has `title` `Tuning SQLite`, `site_name` `docs.example.com`, `extraction_mode` `full`, `reading_minutes` 9, `content_truncated` false, and its content holds the first and the last sentence of the document

#### Scenario: PDF served under another type
- **GIVEN** the same PDF served with `Content-Type: application/octet-stream`, then with `Content-Type: text/html`
- **WHEN** the user captures each URL
- **THEN** both items have `extraction_mode` `full` and the title `Tuning SQLite`

#### Scenario: Words placed without spaces
- **GIVEN** a PDF whose sentence `Le moteur lit chaque page` is drawn glyph by glyph, its words separated by moves of the text position and no space character
- **WHEN** the user captures its URL
- **THEN** the item's content holds `Le moteur lit chaque page`

#### Scenario: Text in a two-byte font
- **GIVEN** a PDF whose text `Crème brûlée à l'œuf` is written with a font of two-byte codes and a map to Unicode
- **WHEN** the user captures its URL
- **THEN** the item's content holds `Crème brûlée à l'œuf`

#### Scenario: Ligatures and hyphenated line ends
- **GIVEN** a PDF of three lines: `Une`, the ligature U+FB01 for `fi`, `ne réfle-`; then `xion sur le porte` followed by a soft hyphen (U+00AD); then `monnaie`
- **WHEN** the user captures its URL
- **THEN** the item's content holds `Une fine réfle-xion sur le portemonnaie`

#### Scenario: Encrypted PDF that opens without a password
- **GIVEN** a PDF encrypted with AES-128 that opens without a password, titled `Copie fictive` in its metadata
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full`, `title` `Copie fictive` with no trailing control character, and its content holds the document's text

#### Scenario: PDF of version 2.0
- **GIVEN** a PDF whose header declares version 2.0
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full` and its content holds the document's text

#### Scenario: A page that cannot be read is skipped
- **GIVEN** a PDF of 3 pages whose second page's content cannot be decoded
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full` and its content holds the text of pages 1 and 3

#### Scenario: Private PDF links are read too
- **WHEN** the user captures a PDF link marked private
- **THEN** the PDF is read and the item becomes `extracted` with `extraction_mode` `full` (RG-92)

### Requirement: The title of a PDF follows its metadata, then its file name
A PDF link's title SHALL be its metadata title, without control characters and with spaces collapsed, unless it is empty or junk: starting with `Microsoft Word - `, `Microsoft PowerPoint - ` or `Microsoft Excel - `, a file name (ending with a document extension such as `.docx` or `.html`), or `Untitled`, `Sans titre`, `Document1` or `PowerPoint Presentation` ignoring case. Else the last path segment of the URL the PDF was served from, when it ends with `.pdf`, percent-decoded, without that suffix; else the provisional title. This holds when a PDF falls back too (RG-23, RG-26, RG-28).

#### Scenario: Junk metadata title
- **GIVEN** PDFs served at `https://example.com/docs/rapport-annuel-2025.pdf` whose metadata title is `Microsoft Word - rapport.docx`, `page.html`, `Untitled`, or only tabs
- **WHEN** the user captures each of them
- **THEN** each item's title is `rapport-annuel-2025`

#### Scenario: Metadata title kept when it only looks like a file name
- **GIVEN** a PDF whose metadata title is `Node.js en production`
- **WHEN** the user captures its URL
- **THEN** the item's title is `Node.js en production`

#### Scenario: File name of the URL is decoded
- **GIVEN** a PDF without metadata title served at `https://example.com/files/Rapport%20d%27activit%C3%A9.PDF`
- **WHEN** the user captures its URL
- **THEN** the item's title is `Rapport d'activité`

#### Scenario: File name after a redirect
- **GIVEN** `https://example.com/download?id=7` redirecting to `https://cdn.example.com/files/guide-installation.pdf`, which serves a PDF without metadata title
- **WHEN** the user captures `https://example.com/download?id=7`
- **THEN** the item's title is `guide-installation` and its `site_name` is `example.com`

#### Scenario: PDF without title nor file name
- **GIVEN** a PDF without metadata title served at `https://example.com/download?id=7`
- **WHEN** the user captures its URL
- **THEN** the item's title is `example.com`

#### Scenario: Broken metadata does not lose the text
- **GIVEN** a PDF at `https://example.com/docs/notice.pdf` whose metadata cannot be decoded but whose pages can
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full`, `title` `notice`, and its content holds the document's text

### Requirement: Long PDF text is cut and its reading time counts the whole document
A PDF's text longer than 120 000 characters SHALL be cut and marked as any content, while its reading time SHALL count the words of every page. When the reading reaches its time limit first, the item SHALL keep the text read, be marked `content_truncated`, and its reading time SHALL add the words of the pages not read, estimated pro rata of the pages read (RG-24, RG-25, RG-28).

#### Scenario: Very long PDF
- **GIVEN** a PDF of 400 pages holding 78 000 words
- **WHEN** the user captures its URL
- **THEN** the item has `content_truncated` true, its content is at most 120 000 characters long and ends on a whole word, and `reading_minutes` is 340

#### Scenario: Page with an enormous text
- **GIVEN** a PDF whose only page decompresses to more than 100 MB of words
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full`, `content_truncated` true, a content of at most 120 000 characters and a `reading_minutes`, and the backend keeps answering

#### Scenario: PDF whose reading reaches the time limit
- **GIVEN** a PDF of 400 pages of 200 words each, whose reading reaches its time limit once 50 pages are read
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full`, its content holds the text of the first 50 pages and not of the 51st, `content_truncated` is true and `reading_minutes` is 348

### Requirement: A PDF that cannot be read falls back
When a PDF holds less than 200 characters of text, needs a password, is encrypted with AES-256, is damaged, exceeds 50 MB, does not download within 15 s, or needs more memory or time to read than allowed, the item SHALL become `extracted` with `extraction_mode` `fallback`, its title in the order of the PDF title requirement, the host name as `site_name`, no content and a null `reading_minutes` (RG-23, RG-28).

#### Scenario: Scanned PDF
- **GIVEN** a PDF of 3 pages holding only images, titled `Facture fictive` in its metadata
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `fallback`, `title` `Facture fictive`, no content and `reading_minutes` null

#### Scenario: Password-protected PDF
- **GIVEN** a PDF that needs a password to open, served at `https://example.com/docs/plan-fictif.pdf`
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `fallback`, `title` `plan-fictif` and no content

#### Scenario: PDF encrypted with AES-256
- **GIVEN** a PDF encrypted with AES-256 that needs no password to open, served at `https://example.com/docs/notice-fictive.pdf`
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `fallback`, `title` `notice-fictive` and no content

#### Scenario: Damaged PDF
- **GIVEN** a PDF cut in its middle, a PDF whose pointer to its cross-reference table is wrong, and an HTML page served as `application/pdf`, each at a URL whose file name is `rapport-fictif.pdf`
- **WHEN** the user captures each URL
- **THEN** each item has `extraction_mode` `fallback`, `title` `rapport-fictif` and no content

#### Scenario: Oversized PDF
- **GIVEN** a PDF body of 50 MB plus one byte, served with its exact `Content-Length`, without `Content-Length`, and compressed with gzip; and a PDF announced by its `Content-Length` as larger than 50 MB
- **WHEN** the user captures each URL
- **THEN** each item has `extraction_mode` `fallback` and the title of its file name, no body is read beyond 50 MB, and the announced one is not read at all

#### Scenario: PDF that does not download in time
- **GIVEN** a PDF at `https://example.com/docs/lent.pdf` whose headers arrive at once and whose body takes 20 s
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` and `title` `lent`, within about 15 s

#### Scenario: PDF too heavy to read
- **GIVEN** a PDF whose page holds a single string of more than 100 MB
- **WHEN** the user captures its URL
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback` and the title of its file name, and the backend keeps answering

#### Scenario: PDF whose structure loops
- **GIVEN** a PDF whose page tree contains itself, and a PDF whose cross-reference sections point to each other
- **WHEN** the user captures each URL
- **THEN** each item becomes `extracted` with `extraction_mode` `fallback` within the reading time limit, and the backend keeps answering

### Requirement: A PDF leaves no copy on the NAS
A downloaded PDF SHALL never be kept on the NAS: its temporary file under the data directory SHALL be gone from the directory before the download starts and released once the item is written or left `queued`; a file left by a crash SHALL be removed at start-up; and the memory of the service SHALL not grow with the size of the PDF (RG-92, ADR-0019).

#### Scenario: No file is left after a PDF
- **GIVEN** a PDF read in full, a PDF over 50 MB, and a PDF whose download is cut by a stop of the backend
- **WHEN** each is extracted or left `queued`
- **THEN** the temporary directory holds no file during nor after the extraction, and the service keeps no descriptor open on a temporary file

#### Scenario: Leftover temporary files are removed at start-up
- **GIVEN** a file left in the temporary directory by a crash
- **WHEN** the backend starts
- **THEN** the file is removed

#### Scenario: A large PDF does not grow the memory of the service
- **GIVEN** a PDF of 45 MB
- **WHEN** the user captures its URL
- **THEN** the item has `extraction_mode` `full`, and the heap of the service grows by less than 8 MiB while the PDF is downloaded and read

### Requirement: Links left waiting as PDFs are picked up
A link left `queued` as a PDF by the previous version SHALL be downloaded once more and extracted like any queued link, each for its own user (RG-28, RG-90).

#### Scenario: Link left waiting as a PDF is extracted
- **GIVEN** a PDF link left `queued` as a PDF before this version
- **WHEN** the backend starts
- **THEN** its URL is requested once and the item becomes `extracted` with `extraction_mode` `full`

#### Scenario: Links left waiting as PDFs of two users
- **GIVEN** two users, each with a PDF link left `queued` as a PDF before this version, at different URLs
- **WHEN** the backend starts
- **THEN** each item becomes `extracted` with the content of its own URL, written for its own user only

### Requirement: A YouTube video is read with its metadata
For a link whose host is `youtube.com`, one of its subdomains or `youtu.be`, and whose URL names an 11-character video id (`watch?v=<id>`, `youtu.be/<id>`, `/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>`), the extraction SHALL download the page `https://www.youtube.com/watch?v=<id>`, never the captured URL, and keep, from the player data of that video, its title as `title`, its channel as `site_name`, its description as content, cut at 120 000 characters and marked `content_truncated` beyond, and its duration as `reading_minutes`, in minutes rounded up, at least 1, null when the duration is 0 or unknown. The item SHALL get `extraction_mode` `fallback`, since no transcript is read. Private videos are read too: only the video id is sent, to YouTube (RG-22, RG-23, RG-24, RG-25, RG-92).

#### Scenario: Video with a description
- **GIVEN** a fictional video page for id `aaaaaaaaaa1` whose player data hold the title `Tuning SQLite`, the channel `Example Talks`, a duration of 754 s and a description of 900 characters on several lines
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1&t=42s&utm_source=x`
- **THEN** the only page requested is `https://www.youtube.com/watch?v=aaaaaaaaaa1`, and the item has `title` `Tuning SQLite`, `site_name` `Example Talks`, `extraction_mode` `fallback`, `reading_minutes` 13, `content_truncated` false and the description as content, its line breaks kept

#### Scenario: Short link, Short and live forms
- **WHEN** the user captures `https://youtu.be/aaaaaaaaaa1?si=x`, `https://m.youtube.com/shorts/aaaaaaaaaa1` and `https://www.youtube.com/live/aaaaaaaaaa1`
- **THEN** each extraction requests `https://www.youtube.com/watch?v=aaaaaaaaaa1`, and neither `youtu.be` nor `m.youtube.com` is requested

#### Scenario: Video without description
- **GIVEN** a fictional video page whose player data hold an empty description and a duration of 59 s
- **WHEN** the user captures its URL
- **THEN** the item has its title and channel, no content, `reading_minutes` 1 and `extraction_mode` `fallback`

#### Scenario: Live stream without duration
- **GIVEN** a fictional video page whose player data hold a duration of 0 s
- **WHEN** the user captures its URL
- **THEN** the item has its title and channel and `reading_minutes` null

#### Scenario: Very long description
- **GIVEN** a fictional video page whose description holds 130 000 characters
- **WHEN** the user captures its URL
- **THEN** the content holds 120 000 characters at most, ends on a whole word, and `content_truncated` is true

#### Scenario: Player data of another video are ignored
- **GIVEN** a fictional video page for id `aaaaaaaaaa1` whose player data name the video `bbbbbbbbbb2` with the title `Other video`, and oEmbed answering the title `Tuning SQLite`
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1`
- **THEN** the item has `title` `Tuning SQLite`, not `Other video`

#### Scenario: Private video links are extracted too
- **WHEN** the user captures a video link marked private
- **THEN** its page is requested and the item becomes `extracted` with the video's title and `extraction_mode` `fallback` (RG-92)

### Requirement: A video whose page gives no metadata falls back on oEmbed
When the video page gives no usable player data — an error status, no answer within 10 s, a body over 2 MB, a consent page, a redirect off YouTube, no player data, player data without `videoDetails`, of another video, or malformed — the extraction SHALL ask `https://www.youtube.com/oembed?url=<page>&format=json`, within 5 s, and keep its `title` as `title` and its `author_name` as `site_name`, without content nor reading time. When oEmbed fails too, the item SHALL fall back with its provisional title and its host as `site_name`. The download of a video SHALL take at most about 15 s in total, and the item SHALL always become `extracted` with `extraction_mode` `fallback`, except as the requirement « Every captured item reaches extracted without blocking » says for a stop, a network outage of the NAS or a failure of the NAS (RG-20, RG-23, RG-26).

#### Scenario: Page without player data
- **GIVEN** a video page answered `200` without player data, and oEmbed answering the title `Tuning SQLite` and `author_name` `Example Talks`
- **WHEN** the user captures the video URL
- **THEN** the item has `title` `Tuning SQLite`, `site_name` `Example Talks`, no content, `reading_minutes` null and `extraction_mode` `fallback`

#### Scenario: Unavailable video
- **GIVEN** a video page answered `200` with player data of status `ERROR` and no `videoDetails`, and oEmbed answering `400`
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1`
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback`, its provisional title `www.youtube.com` and `site_name` `www.youtube.com`

#### Scenario: Consent page
- **GIVEN** a video page request redirected to `https://consent.youtube.com/…`, and oEmbed answering a title
- **WHEN** the user captures the video URL
- **THEN** the item gets the oEmbed title

#### Scenario: Video page that does not answer in time
- **GIVEN** a video page that answers after 20 s, and oEmbed answering the title `Tuning SQLite`
- **WHEN** the user captures the video URL
- **THEN** the item has `title` `Tuning SQLite` and `extraction_mode` `fallback`, within about 15 s

#### Scenario: Neither the page nor oEmbed answers in time
- **GIVEN** a video page and an oEmbed answer that both come after 20 s
- **WHEN** the user captures `https://youtu.be/aaaaaaaaaa1`
- **THEN** the item becomes `extracted` with `extraction_mode` `fallback`, its provisional title `youtu.be` and `site_name` `youtu.be`, within about 15 s

#### Scenario: A NAS network outage leaves a video queued
- **GIVEN** the NAS cannot resolve any host name
- **WHEN** the user captures a video link
- **THEN** oEmbed is not asked, the item stays `queued`, and becomes `extracted` with the video's metadata once the network is back

#### Scenario: A video that crashed the backend is not fetched again
- **GIVEN** a video link whose extraction had started when the backend stopped abruptly
- **WHEN** the backend starts again
- **THEN** neither the video page nor oEmbed is requested, and the item becomes `extracted` with `extraction_mode` `fallback` and its provisional title

### Requirement: A YouTube link that is not a video is read as a web page
A link whose host is `youtube.com`, one of its subdomains or `youtu.be`, and whose URL names no video id (a channel, a playlist without video, the home page), SHALL be extracted as any web page (RG-21, RG-23).

#### Scenario: Channel page
- **GIVEN** a fictional channel page at `https://www.youtube.com/@exampletalks` with `og:title` `Example Talks`
- **WHEN** the user captures its URL
- **THEN** the page itself is requested and the item becomes `extracted` with the title `Example Talks`

### Requirement: Links left waiting as YouTube links are picked up
The YouTube links left `queued` by the previous extraction SHALL be extracted by the next pass, without action from the user (RG-20).

#### Scenario: Waiting YouTube link
- **GIVEN** a YouTube video link left `queued`
- **WHEN** the extraction runs
- **THEN** the item becomes `extracted` with the video's metadata

### Requirement: Video extraction keeps content out of the logs
The logs of a video's extraction SHALL contain neither the URL, the host nor the video id, the title, the channel or the description; only the item id, the extraction mode, a reason class, the HTTP statuses and durations (RG-90, ADR-0027).

#### Scenario: Video extraction logs no content
- **GIVEN** a fictional video `aaaaaaaaaa1` titled `Private thought` by the channel `Secret Channel`
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1`, and a second video whose page fails and whose oEmbed answers the same title
- **THEN** no log line contains `aaaaaaaaaa1`, `youtube.com`, `Private thought` nor `Secret Channel`
