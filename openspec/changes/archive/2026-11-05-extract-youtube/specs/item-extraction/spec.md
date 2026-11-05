## REMOVED Requirements

### Requirement: YouTube links wait for their own extraction
**Reason**: E2-06 extracts YouTube links: a video's metadata are read, any other YouTube link is read as a web page.
**Migration**: the links left `queued` are picked up by the next extraction pass (requirement « Links left waiting as YouTube links are picked up »).

## MODIFIED Requirements

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

## ADDED Requirements

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
