# item-capture Specification

## Purpose
Lets the user capture an item through `POST /api/items` and get an immediate confirmation: what a capture must contain, how a replayed capture is recognised, and what a freshly captured item looks like before extraction and analysis.

## Requirements

### Requirement: A link capture creates a queued item
`POST /api/items` with a JSON body holding a valid `url` SHALL create a `link` item owned by the authenticated user and answer `201` with the full item: `kind` `link`, `status` `queued`, `read_status` `to_read`, `url` as captured, no content type, no category, no tags, no fiche, no extraction or analysis date. No extraction and no analysis SHALL run before the answer (RG-01, RG-90).

#### Scenario: Capture a link
- **GIVEN** an authenticated user
- **WHEN** they send `POST /api/items` with `{"url": "https://example.com/article"}`
- **THEN** the answer is `201` with an item whose `kind` is `link`, `status` is `queued`, `read_status` is `to_read`, `url` is `https://example.com/article`, `content_type`, `category_id`, `extracted_at` and `analyzed_at` are null, `tag_ids` is empty and no `fiche` is present

#### Scenario: Each capture gets its own identifier
- **GIVEN** an authenticated user
- **WHEN** they capture two links in a row
- **THEN** both answers are `201` with two different ULID `id` values

### Requirement: The provisional title of a link is its host name
Until extraction has run, the `title` of a link item SHALL be the host name of its URL, in lower case and without port (RG-26).

#### Scenario: Title of a freshly captured link
- **WHEN** the user captures `https://Blog.Example.com:8443/posts/1?x=1`
- **THEN** the item's `title` is `blog.example.com`

### Requirement: The capture note, privacy and source are recorded
A link capture SHALL store its `user_note` as the capture note, its `is_private` flag (`false` when absent) and its `source` (`share` when absent, otherwise `share` or `manual`) (RG-03, RG-10).

#### Scenario: Capture with a note, private, from the app
- **WHEN** the user captures `{"url": "https://example.com/a", "user_note": "for the trip", "is_private": true, "source": "manual"}`
- **THEN** the answer is `201` with `user_note` `for the trip`, `is_private` true and `source` `manual`

#### Scenario: Defaults of a bare capture
- **WHEN** the user captures `{"url": "https://example.com/a"}`
- **THEN** the item has `user_note` null, `is_private` false and `source` `share`

#### Scenario: Unknown source is refused
- **WHEN** the user captures `{"url": "https://example.com/a", "source": "rss"}`
- **THEN** the answer is `400` with a problem of type `about:blank` and no item is created

### Requirement: Surrounding whitespace is removed from a capture
Before any check, leading and trailing whitespace (spaces, tabs, line breaks) SHALL be removed from `url` and `user_note`; a `user_note` that is empty after this SHALL be treated as absent. The stored `url` is the trimmed one (RG-09).

#### Scenario: Link shared with a trailing line break
- **WHEN** the user captures `{"url": "  https://example.com/a\n"}`
- **THEN** the answer is `201` with `url` `https://example.com/a`

#### Scenario: Blank capture note
- **WHEN** the user captures `{"url": "https://example.com/a", "user_note": "  \n "}`
- **THEN** the answer is `201` with `user_note` null

### Requirement: A capture must be a valid link or a note
A capture SHALL be refused with `400` and nothing stored when it holds neither a `url` nor a `note` (`urn:hippo:problem:empty-capture`), and when its `url`, once trimmed, is not an absolute `http` or `https` URL with a non-empty host (`urn:hippo:problem:invalid-url`). The scheme is matched in any case. The stored `url` is the trimmed string as sent (RG-02, RG-09).

#### Scenario: Empty capture
- **WHEN** the user sends `POST /api/items` with `{}`
- **THEN** the answer is `400` with type `urn:hippo:problem:empty-capture`

#### Scenario: JSON null body
- **WHEN** the user sends `POST /api/items` with `Content-Type: application/json` and the body `null`
- **THEN** the answer is `400` with type `urn:hippo:problem:empty-capture`

#### Scenario: Blank URL alone
- **WHEN** the user sends `{"url": "   "}`
- **THEN** the answer is `400` with type `urn:hippo:problem:invalid-url`

#### Scenario: URL with another scheme
- **WHEN** the user captures `{"url": "ftp://example.com/file"}`, `{"url": "javascript:alert(1)"}`, `{"url": "data:text/html,hi"}` or `{"url": "mailto:someone@example.com"}`
- **THEN** each answer is `400` with type `urn:hippo:problem:invalid-url`

#### Scenario: URL without a host
- **WHEN** the user captures `{"url": "example.com/article"}`, `{"url": "https://"}`, `{"url": "https:example.com"}` or `{"url": "https://:443/a"}`
- **THEN** each answer is `400` with type `urn:hippo:problem:invalid-url`

#### Scenario: Upper-case scheme is accepted as sent
- **WHEN** the user captures `{"url": "HTTPS://Example.com/A"}`
- **THEN** the answer is `201` with `url` `HTTPS://Example.com/A` and `title` `example.com`

#### Scenario: A refused capture stores nothing
- **GIVEN** a capture refused with `invalid-url` that carried a `client_id`
- **WHEN** the user then sends a valid link capture with the same `client_id`
- **THEN** the answer is `201`: the refused capture left no item behind

### Requirement: An ambiguous capture is refused
A capture holding both a `url` and a `note`, or both a `note` and a `user_note`, SHALL be refused with `400` and type `urn:hippo:problem:ambiguous-capture`, before the URL is checked, and nothing SHALL be stored. A field counts as held when it is present and not null; a `user_note` blank after trimming does not count (RG-07, RG-09).

#### Scenario: URL and note together
- **WHEN** the user sends `{"url": "https://example.com/a", "note": "buy milk"}`
- **THEN** the answer is `400` with type `urn:hippo:problem:ambiguous-capture`

#### Scenario: Note and capture note together
- **WHEN** the user sends `{"note": "buy milk", "user_note": "today"}`
- **THEN** the answer is `400` with type `urn:hippo:problem:ambiguous-capture`

#### Scenario: Ambiguity wins over an invalid URL
- **WHEN** the user sends `{"url": "not a url", "note": "buy milk"}` or `{"url": "", "note": "buy milk"}`
- **THEN** each answer is `400` with type `urn:hippo:problem:ambiguous-capture`

#### Scenario: A blank capture note does not make a note ambiguous
- **WHEN** the user sends `{"note": "buy milk", "user_note": "   "}`
- **THEN** the answer is not `ambiguous-capture` (it is the `501` of a note capture)

### Requirement: A replayed capture returns the existing item
A valid capture carrying a `client_id` that the same user already used SHALL create nothing and answer `200` with the existing item in its current state, whatever the rest of the body. A `client_id` used by another user SHALL have no effect. A `client_id` that is not a ULID SHALL be refused with `400` type `about:blank` (RG-08, RG-90).

#### Scenario: Replay of the same capture
- **GIVEN** the user captured `{"url": "https://example.com/a", "client_id": "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"}` and got `201`
- **WHEN** they send exactly the same capture again
- **THEN** the answer is `200` with the same item `id`, and the item is unchanged

#### Scenario: Replay with a different body
- **GIVEN** the user captured `https://example.com/a` with `client_id` `01JB8Z6Q4M1X2Y3Z4A5B6C7D8E`
- **WHEN** they send `{"url": "https://example.com/b", "user_note": "other", "client_id": "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"}`
- **THEN** the answer is `200` with the first item, whose `url` is still `https://example.com/a` and `user_note` still null

#### Scenario: Concurrent replays create one item
- **WHEN** the user sends the same capture with the same `client_id` several times at once
- **THEN** exactly one answer is `201`, the others are `200`, and all carry the same item `id`

#### Scenario: Same client_id from another user
- **GIVEN** user A captured a link with `client_id` `01JB8Z6Q4M1X2Y3Z4A5B6C7D8E`
- **WHEN** user B captures a link with the same `client_id`
- **THEN** user B gets `201` with a new item of their own, and user A's item is not returned

#### Scenario: Captures without client_id are never replays
- **WHEN** the user captures the same URL twice without `client_id`
- **THEN** both answers are `201` with different `id` values

#### Scenario: Malformed client_id
- **WHEN** the user captures `{"url": "https://example.com/a", "client_id": "not-a-ulid"}` or `{"url": "https://example.com/a", "client_id": ""}`
- **THEN** each answer is `400` with type `about:blank` and no item is created

### Requirement: The capture body follows the contract strictly
A JSON capture body SHALL be refused with `400` type `about:blank`, before any business check and with nothing stored, when it holds a member the contract does not declare, a `source` other than `share` or `manual`, or data after the JSON value (RG-12, ADR-0017).

#### Scenario: Misspelt member
- **WHEN** the user captures `{"url": "https://example.com/a", "isPrivate": true}`
- **THEN** the answer is `400` with type `about:blank` and no item is created

#### Scenario: Data after the body
- **WHEN** the user sends `{"url": "https://example.com/a"} {"url": "https://example.com/b"}`
- **THEN** the answer is `400` with type `about:blank`

#### Scenario: Empty body
- **WHEN** the user sends `POST /api/items` with `Content-Type: application/json` and no body
- **THEN** the answer is `400` with type `about:blank`

### Requirement: The answer is a complete item
The `201` and `200` answers SHALL be valid against the `Item` schema of the contract: every required member present, arrays empty rather than null, dates in ISO 8601 UTC, and a replay SHALL return exactly what the first answer returned when the item has not changed since (RG-08).

#### Scenario: The answer matches the contract
- **WHEN** the user captures a link
- **THEN** the `201` body validates against the `Item` schema, with `tag_ids`, `assets` and `user_edited` empty arrays

#### Scenario: A replay returns the same body
- **GIVEN** a link captured with a `client_id`
- **WHEN** the user replays it
- **THEN** the `200` body equals the `201` body

### Requirement: Note and image captures are not available yet
Until their stories are delivered, a valid note capture (JSON `note`) and an image capture (`multipart/form-data`) SHALL answer `501` with type `urn:hippo:problem:not-implemented`, and a body that is neither JSON nor multipart SHALL answer `415` (RG-01).

#### Scenario: Note capture
- **WHEN** the user sends `{"note": "buy milk"}`
- **THEN** the answer is `501` with type `urn:hippo:problem:not-implemented`

#### Scenario: Image capture
- **WHEN** the user sends a `multipart/form-data` capture
- **THEN** the answer is `501` with type `urn:hippo:problem:not-implemented`

#### Scenario: Unsupported body format
- **WHEN** the user sends `POST /api/items` with `Content-Type: text/plain`
- **THEN** the answer is `415` with a problem of type `about:blank`

### Requirement: Capture requires the token and keeps the content out of the logs
`POST /api/items` SHALL answer `401` without a valid bearer token and store nothing, and the request log SHALL contain neither the captured URL nor the capture note (RG-90, ADR-0016).

#### Scenario: Capture without token
- **WHEN** a client sends a valid link capture without `Authorization` header
- **THEN** the answer is `401` and a later authenticated replay of the same `client_id` answers `201`

#### Scenario: The captured URL is not logged
- **WHEN** the user captures `{"url": "https://secret.example.com/path", "user_note": "private thought"}`, replays it, and sends `{"url": "https://secret.example.com/%zz", "user_note": "private thought"}`
- **THEN** none of the log lines of these three requests (`201`, `200`, `400 invalid-url`) contains `secret.example.com` nor `private thought`
