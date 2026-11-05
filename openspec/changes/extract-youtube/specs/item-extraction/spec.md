## REMOVED Requirements

### Requirement: YouTube links wait for their own extraction
**Reason**: E2-06 extracts YouTube links: a video's metadata are read, any other YouTube link is read as a web page.
**Migration**: the links left `queued` are picked up by the next extraction pass (requirement « Links left waiting as YouTube links are picked up »).

## ADDED Requirements

### Requirement: A YouTube video is read with its metadata
For a link whose host is `youtube.com`, one of its subdomains or `youtu.be`, and whose URL names an 11-character video id (`watch?v=<id>`, `youtu.be/<id>`, `/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>`), the extraction SHALL download the page `https://www.youtube.com/watch?v=<id>` and keep, from the player data of that video, its title as `title`, its channel as `site_name`, its description as content, cut at 120 000 characters and marked `content_truncated` beyond, and its duration as `reading_minutes`, in minutes rounded up, at least 1. The item SHALL get `extraction_mode` `fallback`, since no transcript is read (RG-22, RG-23, RG-24, RG-25).

#### Scenario: Video with a description
- **GIVEN** a fictional video page for id `aaaaaaaaaa1` whose player data hold the title `Tuning SQLite`, the channel `Example Talks`, a duration of 754 s and a description of 900 characters
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1&t=42s&utm_source=x`
- **THEN** the page requested is `https://www.youtube.com/watch?v=aaaaaaaaaa1`, and the item has `title` `Tuning SQLite`, `site_name` `Example Talks`, `extraction_mode` `fallback`, `reading_minutes` 13, `content_truncated` false and the description as content

#### Scenario: Short link, Short and live forms
- **WHEN** the user captures `https://youtu.be/aaaaaaaaaa1?si=x`, `https://m.youtube.com/shorts/aaaaaaaaaa1` and `https://www.youtube.com/live/aaaaaaaaaa1`
- **THEN** each extraction requests `https://www.youtube.com/watch?v=aaaaaaaaaa1`

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
- **THEN** the content holds 120 000 characters at most and `content_truncated` is true

#### Scenario: Player data of another video are ignored
- **GIVEN** a fictional video page for id `aaaaaaaaaa1` whose player data name the video `bbbbbbbbbb2`
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1`
- **THEN** the title does not come from the page, and the extraction asks oEmbed

### Requirement: A video whose page gives no metadata falls back on oEmbed
When the video page gives no usable player data — an error status, no answer within 15 s, a consent page, no player data, player data without `videoDetails`, of another video, or malformed — the extraction SHALL ask `https://www.youtube.com/oembed?url=<page>&format=json` and keep its `title` as `title` and its `author_name` as `site_name`, without content nor reading time. When oEmbed fails too, the item SHALL fall back with its provisional title and its host as `site_name`. The item SHALL always become `extracted` with `extraction_mode` `fallback` (RG-23, RG-26).

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
The logs of a video's extraction SHALL contain neither the URL nor the video id, the title, the channel or the description; only the item id, the extraction mode, a reason class, the HTTP status and durations (RG-90, ADR-0027).

#### Scenario: Video extraction logs no content
- **GIVEN** a fictional video `aaaaaaaaaa1` titled `Private thought` by the channel `Secret Channel`
- **WHEN** the user captures `https://www.youtube.com/watch?v=aaaaaaaaaa1`, and a second video whose page fails and whose oEmbed answers the same title
- **THEN** no log line contains `aaaaaaaaaa1`, `Private thought` nor `Secret Channel`
