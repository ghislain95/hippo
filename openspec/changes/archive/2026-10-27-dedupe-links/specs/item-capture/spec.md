# Spec Delta

## MODIFIED Requirements

### Requirement: A replayed capture returns the existing item
A valid capture carrying a `client_id` that the same user already used, for a capture that created an item or for one merged into an existing item, SHALL create nothing, change nothing and answer `200` with that item in its current state, whatever the rest of the body. A `client_id` used by another user SHALL have no effect. A `client_id` that is not a ULID SHALL be refused with `400` type `about:blank` (RG-08, RG-90).

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
- **WHEN** the user captures two different URLs without `client_id`
- **THEN** both answers are `201` with different `id` values

#### Scenario: Malformed client_id
- **WHEN** the user captures `{"url": "https://example.com/a", "client_id": "not-a-ulid"}` or `{"url": "https://example.com/a", "client_id": ""}`
- **THEN** each answer is `400` with type `about:blank` and no item is created

#### Scenario: Replay of a duplicate capture appends its note once
- **GIVEN** an item for `https://example.com/a` without note, and a capture `{"url": "https://example.com/a?utm_source=x", "user_note": "to test", "client_id": "01JB8Z6Q4M1X2Y3Z4A5B6C7D8F"}` answered `200`
- **WHEN** the user sends that capture again
- **THEN** the answer is `200` with the same item and `user_note` is still `to test`, once

## ADDED Requirements

### Requirement: Links are normalised for deduplication
Every link capture SHALL compute a normalised URL from the trimmed URL as a string: scheme and host in lower case (user info and port kept), fragment removed, empty query pairs and every pair whose name is `fbclid`, `gclid`, `igsh`, `si` or `ref` or starts with `utm_` removed (names compared in lower case), one trailing `/` of the path removed (the root included), remaining pairs sorted by name with values of a same name kept in order, no `?` left on an empty query, percent-encoding untouched. The item's `url` SHALL stay as sent (RG-05).

#### Scenario: Tracking parameters do not make a new item
- **GIVEN** an item for `https://example.com/article?id=3`
- **WHEN** the user captures `https://example.com/article?utm_source=x&id=3&fbclid=abc&UTM_Medium=y`
- **THEN** the answer is `200` with the existing item, whose `url` is still `https://example.com/article?id=3`

#### Scenario: Fragment, trailing slash and host case
- **GIVEN** an item for `https://example.com/docs/page`
- **WHEN** the user captures `HTTPS://Example.COM/docs/page/#section-2`
- **THEN** the answer is `200` with the existing item

#### Scenario: Root, empty query and repeated parameters
- **GIVEN** items for `https://example.com` and `https://example.com/list?tag=b&tag=a&x=1`
- **WHEN** the user captures `https://example.com/?` and `https://example.com/list/?x=1&&tag=b&ref=home&tag=a`
- **THEN** both answers are `200` with the existing items

#### Scenario: Parameter order does not matter
- **GIVEN** an item for `https://example.com/search?b=2&a=1`
- **WHEN** the user captures `https://example.com/search?a=1&b=2`
- **THEN** the answer is `200` with the existing item

#### Scenario: Other differences make another item
- **WHEN** the user captures `https://example.com/a`, then `http://example.com/a`, `https://www.example.com/a`, `https://example.com:443/a`, `https://example.com/A` and `https://example.com/a?id=4`
- **THEN** every answer is `201` with a new item

#### Scenario: Duplicates are per user
- **GIVEN** user A has an item for `https://example.com/a`
- **WHEN** user B captures `https://example.com/a`
- **THEN** user B gets `201` with a new item of their own

### Requirement: A duplicate link returns the existing item
A link capture whose normalised URL matches an item of the same user SHALL create no item and answer `200` with the existing item in its current state; its `url`, `source`, title and processing status SHALL stay as they are (RG-04, RG-90).

#### Scenario: Duplicate without note
- **GIVEN** an item for `https://example.com/a` captured from the share sheet
- **WHEN** the user captures `{"url": "https://example.com/a/", "source": "manual"}`
- **THEN** the answer is `200` with the existing item, `source` still `share`, and its body equals the item as it was before the capture

#### Scenario: Concurrent captures of one link create one item
- **WHEN** the user captures `https://example.com/a` several times at once, each with its own `client_id`
- **THEN** exactly one answer is `201`, the others are `200`, all carry the same item `id`, and replaying any of these captures answers `200` with that item

### Requirement: The note of a duplicate is appended, dated
When a duplicate link capture carries a capture note, the note SHALL become the item's note if the item has none; otherwise it SHALL be appended after a blank line, prefixed by the day the server receives it in Paris time and ` — ` (`2026-10-27 — to test`), unless it is byte for byte equal to the first note or to the text of one dated entry, whatever its date (RG-04).

#### Scenario: First note of an item
- **GIVEN** an item for `https://example.com/a` without note
- **WHEN** the user captures `{"url": "https://example.com/a", "user_note": "to test"}`
- **THEN** the answer is `200` and the item's `user_note` is `to test`

#### Scenario: Further note is dated
- **GIVEN** an item for `https://example.com/a` whose note is `for the trip`
- **WHEN** the user captures `{"url": "https://example.com/a", "user_note": "  to test "}` on 27 October 2026 at 23:30 in Paris
- **THEN** the item's `user_note` is `for the trip` followed by a blank line and `2026-10-27 — to test`

#### Scenario: The day is the day in Paris
- **GIVEN** an item for `https://example.com/a` whose note is `for the trip`
- **WHEN** the user captures it again with `user_note` `to test` at 2026-10-27T23:30:00Z (00:30 on 28 October in Paris)
- **THEN** the appended entry starts with `2026-10-28 — `

#### Scenario: Same note is not repeated
- **GIVEN** an item whose note is `for the trip` followed by a blank line and `2026-10-27 — to test`
- **WHEN** the user captures the same link with `user_note` `to test`, then with `user_note` `for the trip`
- **THEN** both answers are `200` and the item's `user_note` is unchanged

#### Scenario: A note contained in another one is still appended
- **GIVEN** an item whose note is `buy milk and bread`
- **WHEN** the user captures the same link with `user_note` `buy milk`
- **THEN** the item's `user_note` is `buy milk and bread` followed by a blank line and a dated `buy milk` entry

#### Scenario: A note change is synchronised
- **GIVEN** an item whose `updated_at` is T
- **WHEN** a duplicate capture appends a note
- **THEN** the item's `updated_at` is later than T, and a duplicate capture without note leaves `updated_at` at its value

### Requirement: A duplicate can only make an item private
A duplicate link capture with `is_private` true SHALL make the existing item private and keep its fiche; a duplicate capture without `is_private`, or with `is_private` false, SHALL never make a private item public (RG-15, RG-12, RG-14).

#### Scenario: Duplicate marked private
- **GIVEN** a public item for `https://example.com/a`
- **WHEN** the user captures `{"url": "https://example.com/a", "is_private": true}`
- **THEN** the answer is `200` with the existing item and `is_private` true

#### Scenario: Replaying an old private capture does not make the item private again
- **GIVEN** a public item for `https://example.com/a` that a duplicate capture with `is_private` true and `client_id` `01JB8Z6Q4M1X2Y3Z4A5B6C7D8G` made private, then made public again by the user
- **WHEN** that capture is replayed
- **THEN** the answer is `200` and the item's `is_private` is false

#### Scenario: Unmarked duplicate keeps an item private
- **GIVEN** a private item for `https://example.com/a`
- **WHEN** the user captures `{"url": "https://example.com/a", "is_private": false}`
- **THEN** the answer is `200` and the item's `is_private` is still true

### Requirement: The normalised URL is never logged
The request logs of a capture SHALL contain neither the URL as sent, nor its normalised form, nor any capture note, on the duplicate path as on the others (RG-90, ADR-0027).

#### Scenario: Duplicate capture logs no content
- **WHEN** the user captures `https://secret.example.com/a?utm_source=x` with `user_note` `private thought`, twice
- **THEN** no log line of these requests contains `secret.example.com` nor `private thought`
