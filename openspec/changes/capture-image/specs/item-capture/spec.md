# Spec Delta

## ADDED Requirements

### Requirement: A capture body must be JSON or multipart
`POST /api/items` with a body that is neither `application/json` nor `multipart/form-data` SHALL answer `415` with a problem of type `about:blank` and store nothing (RG-01).

#### Scenario: Unsupported body format
- **WHEN** the user sends `POST /api/items` with `Content-Type: text/plain`
- **THEN** the answer is `415` with a problem of type `about:blank`

### Requirement: An image capture creates a queued image item with its asset
`POST /api/items` with a `multipart/form-data` body holding one `image` file SHALL create an `image` item owned by the authenticated user with one asset, and answer `201`: `kind` `image`, `status` `queued`, `read_status` `to_read`, `url` null, `assets` holding one asset with its `id`, `mime_type`, `size_bytes`, and `width` and `height` for JPEG and PNG (null for HEIC and WebP). An image SHALL never be a duplicate (RG-01, RG-04, RG-90).

#### Scenario: Capture a PNG screenshot
- **WHEN** the user sends a multipart capture with a 640×480 PNG `image`, `user_note` `recipe for Sunday`, `is_private` `true` and `source` `manual`
- **THEN** the answer is `201` with an item whose `kind` is `image`, `status` `queued`, `read_status` `to_read`, `url` null, `user_note` `recipe for Sunday`, `is_private` true, `source` `manual`, and one asset with `mime_type` `image/png`, `width` 640, `height` 480 and `size_bytes` the file size

#### Scenario: Capture a JPEG photo with defaults
- **WHEN** the user sends a multipart capture with only a JPEG `image`
- **THEN** the answer is `201` with `user_note` null, `is_private` false, `source` `share`, and an asset with `mime_type` `image/jpeg` and its dimensions

#### Scenario: HEIC and WebP are accepted without dimensions
- **WHEN** the user captures a HEIC image, then a WebP image
- **THEN** both answers are `201` with an asset whose `mime_type` is `image/heic` or `image/webp` and whose `width` and `height` are null

#### Scenario: Same image twice makes two items
- **WHEN** the user captures the same PNG twice without `client_id`
- **THEN** both answers are `201` with different item and asset ids

#### Scenario: The answer matches the contract for an image
- **WHEN** the user captures an image
- **THEN** the `201` body validates against the `Item` schema of the contract

### Requirement: The image format is recognised from its content
An image capture SHALL be refused with `400` type `urn:hippo:problem:unsupported-image`, and nothing stored, when its file content is not a JPEG, PNG, HEIC or WebP image, whatever type the client declares; the stored `mime_type` SHALL be the recognised one (RG-02).

#### Scenario: Unsupported format
- **WHEN** the user sends a multipart capture whose `image` is a GIF, an SVG, an AVIF or a text file
- **THEN** each answer is `400` with type `urn:hippo:problem:unsupported-image` and no item is created

#### Scenario: Truncated JPEG or PNG
- **WHEN** the user sends a JPEG or a PNG whose header cannot be decoded
- **THEN** the answer is `400` with type `urn:hippo:problem:unsupported-image`

#### Scenario: Declared type is ignored
- **WHEN** the user sends a PNG file declared as `image/jpeg`
- **THEN** the answer is `201` with an asset whose `mime_type` is `image/png`

### Requirement: An image is 20 MB at most
An image capture whose file exceeds 20 MB (20 × 1024 × 1024 bytes) SHALL be refused with `413` type `urn:hippo:problem:image-too-large`, and nothing stored (RG-02).

#### Scenario: Image just at the limit
- **WHEN** the user captures a valid image of exactly 20 × 1024 × 1024 bytes
- **THEN** the answer is `201`

#### Scenario: Image over the limit
- **WHEN** the user captures a valid image of 20 × 1024 × 1024 + 1 bytes
- **THEN** the answer is `413` with type `urn:hippo:problem:image-too-large`, no item is created and no file is left in the data directory

### Requirement: The fields of an image capture follow the capture rules
The multipart body SHALL hold exactly one `image` file and, optionally, the text fields `user_note`, `is_private` (exactly `true` or `false`), `client_id` and `source`, in any order, with the rules of a JSON capture (RG-03, RG-08, RG-09, RG-10). A missing `image`, a second file, an undeclared or repeated field (`url` and `note` included), a text field over 1 MiB, invalid UTF-8 or a malformed value SHALL answer `400` type `about:blank`, nothing stored.

#### Scenario: Blank capture note
- **WHEN** the user captures an image with `user_note` `  \n `
- **THEN** the answer is `201` with `user_note` null

#### Scenario: Missing image
- **WHEN** the user sends a multipart capture with a `user_note` and no `image`
- **THEN** the answer is `400` with type `about:blank`

#### Scenario: Two images
- **WHEN** the user sends a multipart capture with two `image` files
- **THEN** the answer is `400` with type `about:blank` and no item is created

#### Scenario: Undeclared or malformed field
- **WHEN** the user sends an image capture with a field `isPrivate`, or `is_private` `yes`, `1` or empty, or `client_id` `not-a-ulid`, or `source` `rss`, or a `note` field
- **THEN** each answer is `400` with type `about:blank` and no item is created

#### Scenario: Fields after the file
- **WHEN** the user sends an image capture whose `client_id`, `user_note` and `source` parts come after the `image` part
- **THEN** the answer is `201` with those values applied

#### Scenario: Oversized multipart body
- **WHEN** the user sends a multipart capture whose body exceeds 21 MiB, through an oversized text field
- **THEN** the answer is `413` with type `about:blank` and no item is created

### Requirement: A replayed image capture stores no new image
A valid image capture whose `client_id` the same user already used SHALL answer `200` with the existing item and SHALL store no new item, asset nor file (RG-08, RG-90).

#### Scenario: Replay of an image capture
- **GIVEN** the user captured an image with `client_id` `01JB8Z6Q4M1X2Y3Z4A5B6C7D8J` and got `201`
- **WHEN** they send the same capture again
- **THEN** the answer is `200` with the same item and asset, and the data directory holds one image file

#### Scenario: Concurrent replays of an image capture
- **WHEN** the user sends the same image capture with the same `client_id` several times at once
- **THEN** exactly one answer is `201`, the others are `200` with the same item, and the data directory holds one image file

### Requirement: The provisional title of an image
Until analysis, the `title` of an image item SHALL be the first non-empty line of its capture note, cut after 80 characters and followed by `…` when longer, or else `Image du <day> <month> <year>` with the day the server receives it in Paris time, the month in French and `1er` for the first day (RG-26).

#### Scenario: Title from the capture note
- **WHEN** the user captures an image with `user_note` `Tarte aux pommes\nde mamie`
- **THEN** the item's `title` is `Tarte aux pommes`

#### Scenario: Dated title without note
- **WHEN** the user captures an image without note at 2026-10-27T23:30:00Z (00:30 on 28 October in Paris)
- **THEN** the item's `title` is `Image du 28 octobre 2026`

#### Scenario: First day of the month
- **WHEN** the user captures an image without note on 1 November 2026 in Paris
- **THEN** the item's `title` is `Image du 1er novembre 2026`

### Requirement: An image capture keeps its content out of the logs
The request logs of an image capture SHALL contain neither the capture note nor the file name sent by the client (RG-90, ADR-0027).

#### Scenario: Image capture logs no content
- **WHEN** the user captures an image named `secret-plan.png` with `user_note` `private thought`
- **THEN** no log line of this request contains `secret-plan` nor `private thought`

## REMOVED Requirements

### Requirement: Image captures are not available yet
**Reason**: Image captures are delivered by this change.
**Migration**: Replaced by "An image capture creates a queued image item with its asset"; the `415` scenario moves to "A capture body must be JSON or multipart".
