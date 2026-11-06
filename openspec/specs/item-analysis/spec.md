# item-analysis Specification

## Purpose
Describes how an item is analysed by the AI: which items are sent each night, the guards checked before any send, the request built for each item, the copy of an image sent to the AI, the submission of the batch and the guarantee that a private item never leaves the NAS. Results, retries, immediate analysis and budget accounting are added by later stories.

## Requirements

### Requirement: The nightly pass runs once per Paris date at the configured time
The backend SHALL run the analysis pass once per calendar date in Europe/Paris, at the minute and hour of `BATCH_SCHEDULE` read as Paris wall-clock time, whatever the `TZ` variable says. When that time does not exist on a date (spring clock change), the pass SHALL run at the first instant after it; when it occurs twice (autumn clock change), it SHALL run once. A date missed while the backend was stopped SHALL NOT be caught up (RG-30).

#### Scenario: The pass runs at the scheduled Paris time
- **GIVEN** `BATCH_SCHEDULE` `0 2 * * *`, `TZ` set to `UTC`, and one extracted public item
- **WHEN** the clock reaches 02:00 in Paris on 10 January 2027 (01:00 UTC)
- **THEN** the AI service receives one batch, and nothing before that instant

#### Scenario: A schedule inside the spring clock change runs once, after the gap
- **GIVEN** `BATCH_SCHEDULE` `30 2 * * *` and one extracted public item
- **WHEN** the clock goes through 28 March 2027 in Paris, where 02:00 jumps to 03:00
- **THEN** the AI service receives exactly one batch that day, at 03:00 Paris time

#### Scenario: A schedule inside the autumn clock change runs once
- **GIVEN** `BATCH_SCHEDULE` `30 2 * * *`
- **WHEN** the clock goes through both 02:30 of 31 October 2027 in Paris, where 03:00 goes back to 02:00
- **THEN** the pass starts exactly once that date, at the first 02:30 (00:30 UTC)

#### Scenario: A restart after the scheduled time does not catch up
- **GIVEN** `BATCH_SCHEDULE` `0 2 * * *` and one extracted public item
- **WHEN** the backend starts at 09:00 Paris time
- **THEN** no batch is sent before 02:00 Paris time the next day

### Requirement: Only a daily schedule is accepted
`BATCH_SCHEDULE` SHALL accept only the daily form `M H * * *`, with `M` from 0 to 59 and `H` from 0 to 23, and default to `0 2 * * *` when unset. Any other value SHALL stop the start-up with an error naming the variable (RG-30).

#### Scenario: The default schedule is 02:00
- **GIVEN** `BATCH_SCHEDULE` unset
- **WHEN** the backend starts
- **THEN** it starts, and its pass runs at 02:00 Paris time

#### Scenario: A schedule that is not daily stops the start-up
- **WHEN** the backend starts with `BATCH_SCHEDULE` `0 2 * * 1`, then with `*/5 * * * *`, then with `0 25 * * *`, then with `nightly`
- **THEN** each start-up fails with an error naming `BATCH_SCHEDULE`

### Requirement: The pass selects each user's waiting items, oldest first
For each user separately, the pass SHALL select only that user's items whose status is `extracted`, that are not private and not deleted, oldest capture first, and SHALL send them in one batch holding only that user's items. A user with nothing to send SHALL cause no call to the AI service (RG-12, RG-30, RG-31, RG-90).

#### Scenario: Only extracted items are sent
- **GIVEN** public items of the user in each status `queued`, `extracted`, `submitted`, `analyzed` and `failed`
- **WHEN** the pass runs
- **THEN** the batch holds one request, for the `extracted` item

#### Scenario: Nothing waiting makes no call
- **GIVEN** a user whose items are all `analyzed`
- **WHEN** the pass runs
- **THEN** the AI service receives no call at all

#### Scenario: A deleted item is not sent
- **GIVEN** an `extracted` public item of the user that was deleted
- **WHEN** the pass runs
- **THEN** no request carries its id

#### Scenario: Each user's batch holds only their items
- **GIVEN** two users, each with two `extracted` public items
- **WHEN** the pass runs
- **THEN** the AI service receives two batches, each holding exactly the requests of one user's items, and each user's items are `submitted` with their own batch

### Requirement: A batch holds at most 200 items and 200 MiB
A batch SHALL hold at most 200 requests, and its request body SHALL stay within 200 MiB (209 715 200 bytes), measured on the body as it is written. The items left out SHALL stay `extracted` and wait for the next pass, the oldest leaving first (RG-32).

#### Scenario: Beyond 200 items, the oldest leave first
- **GIVEN** 203 `extracted` public items of the user, captured in an order different from their creation in the fixture
- **WHEN** the pass runs
- **THEN** the batch holds the 200 oldest items, and the 3 newest stay `extracted`

#### Scenario: Beyond the body limit, the surplus waits
- **GIVEN** a body limit lowered for the test so that only two of three waiting items fit
- **WHEN** the pass runs
- **THEN** the batch holds the two oldest items, and the newest stays `extracted` and is sent by the next pass

### Requirement: Sends are guarded in a fixed order
Before building a user's batch, the pass SHALL check in this order, and send nothing and log the reason on the first refusal: an Anthropic API key is configured; the user has no earlier batch whose cost is not recorded (still being sent, in progress, or ended without a cost); the recorded cost of the user's batches created in the current Paris calendar month is below `MONTHLY_BUDGET_USD` (RG-81, RG-90).

#### Scenario: No API key, no send
- **GIVEN** no `ANTHROPIC_API_KEY` and one `extracted` public item
- **WHEN** the pass runs
- **THEN** the AI service receives no call, the item stays `extracted`, and a log line gives the reason `no_api_key`

#### Scenario: A batch without recorded cost blocks the next one
- **GIVEN** a batch of the user in progress, and one new `extracted` public item
- **WHEN** the pass runs
- **THEN** no batch is sent, the item stays `extracted`, and a log line gives the reason `cost_pending`

#### Scenario: An ended batch without cost blocks the next one
- **GIVEN** an ended batch of the user without a recorded cost, and one `extracted` public item
- **WHEN** the pass runs
- **THEN** no batch is sent and a log line gives the reason `cost_pending`

#### Scenario: Budget reached, no send
- **GIVEN** `MONTHLY_BUDGET_USD` 5, ended batches of the user created this Paris month costing 5.00 in total, and one `extracted` public item
- **WHEN** the pass runs
- **THEN** no batch is sent, the item stays `extracted`, and a log line gives the reason `budget_reached`

#### Scenario: Spend below the budget allows the send
- **GIVEN** `MONTHLY_BUDGET_USD` 5 and ended batches of the user created this Paris month costing 4.99
- **WHEN** the pass runs
- **THEN** the batch is sent

#### Scenario: Last month's spend does not count
- **GIVEN** `MONTHLY_BUDGET_USD` 5, an ended batch of the user costing 5.00 created at 23:30 Paris time on 31 January 2027, and one `extracted` public item
- **WHEN** the pass runs on 1 February 2027 at 02:00 Paris time
- **THEN** the batch is sent

#### Scenario: The month follows Paris time
- **GIVEN** `MONTHLY_BUDGET_USD` 5, an ended batch of the user costing 5.00 created at 00:30 Paris time on 1 February 2027 (23:30 UTC on 31 January), and one `extracted` public item
- **WHEN** the pass runs on 2 February 2027 at 02:00 Paris time
- **THEN** no batch is sent and a log line gives the reason `budget_reached`

#### Scenario: Another user's spend does not count
- **GIVEN** `MONTHLY_BUDGET_USD` 5, another user whose batches this month cost 5.00, and one `extracted` public item of the user
- **WHEN** the pass runs
- **THEN** the user's batch is sent and the other user's is not

#### Scenario: A failed send does not block the next night
- **GIVEN** a batch of the user that failed to be sent, and one `extracted` public item
- **WHEN** the pass runs
- **THEN** the batch is sent

### Requirement: Each request follows the prompt contract
Each request SHALL carry the item id as `custom_id`, the configured model, the forced `classify_item` tool with the bundled schema as its input schema (without its `$schema` keyword nor its `x-*` extension keywords), then a system made of the fixed part of the prompt and the référentiel, with a cache point after the référentiel, then one user message for the item. The output cap SHALL be 4000 tokens, or 8000 for an item whose cut response was already retried once. The référentiel SHALL be identical in every request of a batch (RG-34, RG-36, RG-52, ADR-0005, ADR-0006).

#### Scenario: A request carries the tool, the system and the item
- **GIVEN** one `extracted` public link item of the user
- **WHEN** the pass runs
- **THEN** its request has `custom_id` equal to the item id, the model `claude-haiku-4-5`, the single tool `classify_item` with the bundled schema, without its `$schema` and `x-*` keywords, as input schema and forced by `tool_choice`, a system of two text blocks (the fixed part of the prompt, then the référentiel with a cache point), one user message, and an output cap of 4000

#### Scenario: A retried cut response gets the doubled cap
- **GIVEN** an `extracted` public item marked as already retried after a cut response
- **WHEN** the pass runs
- **THEN** its request has an output cap of 8000

#### Scenario: The référentiel is the same in every request
- **GIVEN** three `extracted` public items of the user of different forms
- **WHEN** the pass runs
- **THEN** the three requests carry byte-identical tools and system blocks

### Requirement: The item message describes the item by its form
The user message SHALL start with an `<element>` block of `key: value` lines in the order `forme`, `url`, `site`, `titre_original`, `extraction`, `contenu_tronqué`, `note_de_capture`, leaving out every absent field. A link SHALL add its extracted content in a `<contenu>` block when it has one; a note SHALL put its text in `<contenu>` and carry no `note_de_capture`; an image SHALL follow the `<element>` block with its image. `extraction` SHALL read `complète`, `repli sur les métadonnées` or `aucune` (RG-03, RG-23, RG-24, RG-26).

#### Scenario: A fully extracted link
- **GIVEN** an `extracted` public link with a site name, a title from its page, extracted content, content not truncated and a capture note
- **WHEN** the pass runs
- **THEN** its message is a single text with `forme: link`, its URL, `site:`, `titre_original:`, `extraction: complète`, `contenu_tronqué: non`, `note_de_capture:`, then the content inside `<contenu>`

#### Scenario: A fallback link without content
- **GIVEN** an `extracted` public link in fallback whose title is still its host name, without content and without capture note
- **WHEN** the pass runs
- **THEN** its `<element>` block has `forme: link`, its URL, `site:`, `extraction: repli sur les métadonnées` and `contenu_tronqué: non`, no `titre_original`, no `note_de_capture`, and the message has no `<contenu>` block

#### Scenario: A truncated content is flagged
- **GIVEN** an `extracted` public link whose content was truncated
- **WHEN** the pass runs
- **THEN** its `<element>` block has `contenu_tronqué: oui`

#### Scenario: Credentials of a URL are not sent
- **GIVEN** an `extracted` public link captured as `https://user:secret@example.com/page`
- **WHEN** the pass runs
- **THEN** its `<element>` block has `url: https://example.com/page` and no request contains `secret`

#### Scenario: A note
- **GIVEN** an `extracted` public note whose text is two lines
- **WHEN** the pass runs
- **THEN** its message has `forme: note` and `extraction: aucune`, no `url`, `site`, `titre_original`, `contenu_tronqué` nor `note_de_capture`, and the two lines inside `<contenu>`

#### Scenario: An image with a capture note
- **GIVEN** an `extracted` public image with a capture note
- **WHEN** the pass runs
- **THEN** its message is a text block with `forme: image`, `extraction: aucune` and `note_de_capture:`, without `titre_original` nor `<contenu>`, followed by one JPEG image block

### Requirement: The AI receives a reduced, upright copy of an image, without metadata
For an image item, the request SHALL carry a copy of the stored image: decoded, turned upright from its orientation metadata, scaled down so that its longer side is at most 1568 pixels (never scaled up), and encoded as JPEG without any metadata. The stored file SHALL stay unchanged. JPEG, PNG, WebP and HEIC images SHALL be copied (RG-12, RG-92).

#### Scenario: A large photo is scaled down
- **GIVEN** an `extracted` public image stored as a 4000 × 3000 JPEG
- **WHEN** the pass runs
- **THEN** its request carries a JPEG of 1568 × 1176 pixels

#### Scenario: A small image is not scaled up
- **GIVEN** an `extracted` public image stored as an 800 × 600 PNG
- **WHEN** the pass runs
- **THEN** its request carries a JPEG of 800 × 600 pixels

#### Scenario: A rotated JPEG is turned upright
- **GIVEN** an `extracted` public image stored as a 400 × 200 JPEG whose EXIF orientation says to rotate it a quarter turn clockwise
- **WHEN** the pass runs
- **THEN** its request carries a 200 × 400 JPEG whose top-left corner shows the colour of the stored image's bottom-left corner

#### Scenario: The copy carries no metadata
- **GIVEN** an `extracted` public image stored as a JPEG with an EXIF block holding a GPS position and a device name
- **WHEN** the pass runs
- **THEN** the JPEG of its request has no EXIF, XMP nor comment segment, and the stored file is byte-identical to the one captured

#### Scenario: A HEIC image is converted
- **GIVEN** an `extracted` public image stored as a HEIC photo
- **WHEN** the pass runs
- **THEN** its request carries a JPEG of the photo, turned upright, with its longer side at most 1568 pixels

#### Scenario: A WebP image is converted
- **GIVEN** an `extracted` public image stored as a WebP
- **WHEN** the pass runs
- **THEN** its request carries a JPEG of the same dimensions

### Requirement: An image that cannot be copied is sent without its image
An image whose pixel count is above the ceiling, that cannot be decoded, or whose copy fails or exceeds its memory or time limit SHALL be sent with its `<element>` block only, so that its fiche is marked partial; the other items of the batch SHALL be unaffected and the backend SHALL keep running. When the copying process cannot start at all, the NAS is at fault: the user's batch SHALL NOT be sent (RG-37).

#### Scenario: Too many pixels
- **GIVEN** an `extracted` public image whose header declares more than 50 million pixels
- **WHEN** the pass runs
- **THEN** its request has the `<element>` block and no image block, and a log line gives the item id and the reason `too_many_pixels`

#### Scenario: An undecodable image
- **GIVEN** an `extracted` public image whose stored file is a JPEG cut after its header
- **WHEN** the pass runs
- **THEN** its request has the `<element>` block and no image block, and a log line gives the reason `undecodable`

#### Scenario: A copy that fails does not stop the batch
- **GIVEN** two `extracted` public images, the first one's copy exceeding its memory limit
- **WHEN** the pass runs
- **THEN** the batch holds both requests, the first without an image block and the second with its JPEG

#### Scenario: A copier that cannot start sends nothing
- **GIVEN** an `extracted` public image and a NAS on which the copying process cannot start
- **WHEN** the pass runs
- **THEN** no batch is sent, the item stays `extracted`, and a log line gives the reason `copier_unavailable`

### Requirement: Sent items are submitted with their batch recorded
Before the send, one transaction SHALL move the selected items still `extracted`, public and not deleted to `submitted` with their batch, advancing each item's `updated_at` and `change_seq`, and SHALL record the batch with its model, prompt version, envelope version and item count. Once the AI service accepts it, the batch SHALL be marked in progress with the service's batch id (RG-30, RG-64, ADR-0035).

#### Scenario: Items become submitted
- **GIVEN** two `extracted` public items of the user
- **WHEN** the pass sends their batch and the AI service accepts it
- **THEN** both items are `submitted` with a greater `change_seq` and later `updated_at`, and one batch of the user is in progress with the service's id, 2 items, the model, prompt version 3 and envelope version 1

#### Scenario: The items' attempts are unchanged
- **GIVEN** an `extracted` public item with one previous attempt
- **WHEN** its batch is sent
- **THEN** it is `submitted` with `attempts` 1

### Requirement: A refused send puts the items back
When the AI service answers the send with a client error status (4xx), the items SHALL go back to `extracted` without any attempt counted, with a greater `change_seq`, and the batch SHALL be recorded as failed (RG-30, RG-35).

#### Scenario: The service refuses the batch
- **GIVEN** two `extracted` public items and an AI service that answers the send with a client error status (4xx)
- **WHEN** the pass runs
- **THEN** both items are `extracted` again with their previous `attempts` and a greater `change_seq`, and the batch is failed

### Requirement: A send whose outcome is unknown is reconciled
When the send gets no answer (timeout, lost connection, stop of the backend during the send, crash) or a server error status (5xx), the batch SHALL stay pending with its items `submitted`. At start-up and at the start of each pass, a pending batch SHALL be matched with the batches the AI service holds for the key: exactly one unrecorded batch created around its creation with the same number of requests is adopted; otherwise its items go back to `extracted` and the batch is failed, and this is logged as a possible double send (RG-30).

#### Scenario: A lost answer is matched at the next start
- **GIVEN** a batch sent by the backend, accepted by the AI service, whose answer was lost, and the backend restarted
- **WHEN** the backend starts
- **THEN** the batch is in progress with the service's id and its items stay `submitted`

#### Scenario: A send that never arrived puts the items back
- **GIVEN** a pending batch for which the AI service holds no matching batch
- **WHEN** the backend starts
- **THEN** its items are `extracted` again, the batch is failed, and a log line gives the reason `possible_double_send`

#### Scenario: An unknown outcome blocks the user's next batch until reconciled
- **GIVEN** a pending batch of the user that cannot be reconciled because the AI service cannot be listed, and one new `extracted` public item
- **WHEN** the pass runs
- **THEN** no batch is sent and a log line gives the reason `cost_pending`

### Requirement: A private item is never sent to the AI
No request of any batch SHALL contain a private item, its id or any of its content, whatever its status, including an item checked Privé after the pass selected it and before its items are submitted; such an item SHALL stay `extracted`. An item checked Privé after its items were submitted and before the batch is sent SHALL be left out of the send: it is back to `extracted` without batch, and the batch is sent the same night with the other items only, or not at all when none is left. A private item reaching the request building SHALL stop the user's batch, sending nothing (RG-12, ADR-0023).

#### Scenario: Private items at every status are never sent
- **GIVEN** private items of the user in each status `queued`, `extracted`, `submitted`, `analyzed` and `failed`, with distinctive titles, notes and contents, and one `extracted` public item
- **WHEN** the pass runs
- **THEN** the batch holds only the public item's request, and no byte of any request contains a private item's id, title, note or content

#### Scenario: An item made private after selection is not sent
- **GIVEN** two `extracted` public items, one of which is checked Privé after the pass selected it and before the submission
- **WHEN** the pass sends its batch
- **THEN** the batch holds only the other item's request, and the item made private is still `extracted`

#### Scenario: An item made private after submission and before the send is left out of the send
- **GIVEN** two `extracted` public items selected and submitted by the pass, one of which is checked Privé before the batch is sent
- **WHEN** the pass sends its batch
- **THEN** the AI service receives a batch with only the other item's request, no byte of it contains the private item's id, title or content, the private item is back to `extracted` without batch, and the other item is `submitted`

#### Scenario: When every item of the batch is made private, nothing is sent
- **GIVEN** two `extracted` public items selected and submitted by the pass, both checked Privé before the batch is sent
- **WHEN** the pass is about to send its batch
- **THEN** the AI service receives nothing, the batch is `failed`, and both items are back to `extracted`

#### Scenario: A private item reaching the request building stops the batch
- **GIVEN** a request building handed a private item
- **WHEN** it builds the request
- **THEN** it refuses with an error, and the AI service receives nothing for this user

### Requirement: The pass logs no item content nor secret
Log lines of the pass SHALL carry only identifiers, counts, sizes, durations, statuses and reason classes; never a title, URL, note, extracted content, image, request or response body, nor the API key (RG-92, ADR-0027).

#### Scenario: Logs carry no content
- **GIVEN** items with distinctive titles, URLs, notes and contents, and an API key
- **WHEN** a pass sends a batch, refuses a send, and fails to copy an image
- **THEN** no log line contains any of these values, nor the API key
