# Spec Delta

## MODIFIED Requirements

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
- **THEN** the answer is `201` with a `note` item whose `user_note` is `buy milk`

#### Scenario: Ambiguity wins over an empty note
- **WHEN** the user sends `{"note": "   ", "user_note": "today"}`
- **THEN** the answer is `400` with type `urn:hippo:problem:ambiguous-capture`

## ADDED Requirements

### Requirement: Image captures are not available yet
Until its story is delivered, an image capture (`multipart/form-data`) SHALL answer `501` with type `urn:hippo:problem:not-implemented`, and a body that is neither JSON nor multipart SHALL answer `415` (RG-01).

#### Scenario: Image capture
- **WHEN** the user sends a `multipart/form-data` capture
- **THEN** the answer is `501` with type `urn:hippo:problem:not-implemented`

#### Scenario: Unsupported body format
- **WHEN** the user sends `POST /api/items` with `Content-Type: text/plain`
- **THEN** the answer is `415` with a problem of type `about:blank`


### Requirement: A note capture creates a queued note item
`POST /api/items` with a JSON body holding a non-empty `note` SHALL create a `note` item owned by the authenticated user and answer `201`: `kind` `note`, `status` `queued`, `read_status` `to_read`, `url` null, the trimmed text as `user_note`, `is_private` and `source` as for a link. A note SHALL never be a duplicate (RG-01, RG-02, RG-04, RG-90).

#### Scenario: Capture a note
- **WHEN** the user sends `{"note": "Buy the 10 mm socket\nfor the bike", "is_private": true, "source": "manual"}`
- **THEN** the answer is `201` with an item whose `kind` is `note`, `status` `queued`, `read_status` `to_read`, `url` null, `user_note` `Buy the 10 mm socket\nfor the bike`, `is_private` true and `source` `manual`

#### Scenario: Same text twice makes two notes
- **WHEN** the user sends `{"note": "call the plumber"}` twice without `client_id`
- **THEN** both answers are `201` with different `id` values

#### Scenario: Replayed note capture
- **GIVEN** the user captured `{"note": "call the plumber", "client_id": "01JB8Z6Q4M1X2Y3Z4A5B6C7D8H"}` and got `201`
- **WHEN** they send the same capture again
- **THEN** the answer is `200` with the same item

#### Scenario: The answer matches the contract for a note
- **WHEN** the user captures a note
- **THEN** the `201` body validates against the `Item` schema of the contract

### Requirement: The text of a note is trimmed and must not be empty
The `note` text SHALL be stored without its surrounding whitespace (spaces, tabs, line breaks), inner whitespace kept; a `note` empty after trimming SHALL be refused with `400` type `urn:hippo:problem:empty-note` and nothing stored (RG-02, RG-09).

#### Scenario: Note with surrounding whitespace
- **WHEN** the user sends `{"note": "\n  first line\n\n  second line  \n"}`
- **THEN** the answer is `201` with `user_note` `first line\n\n  second line`

#### Scenario: Blank note
- **WHEN** the user sends `{"note": ""}` or `{"note": " \n\t "}`
- **THEN** each answer is `400` with type `urn:hippo:problem:empty-note` and no item is created

### Requirement: The provisional title of a note is its first line
Until analysis, the `title` of a note item SHALL be its first non-empty line, trimmed, cut after 80 characters (Unicode code points) and followed by `…` when it was longer (RG-26).

#### Scenario: Short first line
- **WHEN** the user captures `{"note": "Shopping\n- eggs\n- milk"}`
- **THEN** the item's `title` is `Shopping`

#### Scenario: Long first line
- **WHEN** the user captures a note whose first line is 100 characters long
- **THEN** the item's `title` is the first 80 characters of that line followed by `…`

#### Scenario: Exactly 80 characters
- **WHEN** the user captures a note whose single line is 80 characters long, accents included
- **THEN** the item's `title` is that line, without `…`

#### Scenario: Leading blank lines
- **WHEN** the user captures `{"note": "   \n\n  Idea: a shelf  \nmore"}`
- **THEN** the item's `title` is `Idea: a shelf`

### Requirement: A note capture keeps its text out of the logs
The request logs of a note capture SHALL not contain the note text (RG-90, ADR-0027).

#### Scenario: Note text is not logged
- **WHEN** the user captures `{"note": "private thought about the plumber"}`
- **THEN** no log line of this request contains `private thought`

## REMOVED Requirements

### Requirement: Note and image captures are not available yet
**Reason**: Note captures are delivered by this change; only image captures remain unavailable.
**Migration**: Replaced by "Image captures are not available yet" and "A note capture creates a queued note item".
