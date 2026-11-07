# Spec Delta

## MODIFIED Requirements

### Requirement: Sends are guarded in a fixed order
Before building a user's batch, and after reconciling and following that user's batches, the pass SHALL check in this order, and send nothing and log the reason on the first refusal: an Anthropic API key is configured; the user has no earlier batch whose cost is not recorded (still being sent, in progress, or ended without a cost); the recorded cost of the user's batches created in the current Paris calendar month is below `MONTHLY_BUDGET_USD` (RG-30, RG-81, RG-90).

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

#### Scenario: A batch that ended before the pass is applied before the guards
- **GIVEN** a batch of the user in progress that the AI service reports ended with valid results, not yet followed, and one new `extracted` public item
- **WHEN** the nightly pass runs
- **THEN** the results are applied, the batch is ended with its cost, and a new batch is sent with the new item the same night

### Requirement: The pass logs no item content nor secret
Log lines of the pass, of the follow-up of a batch and of the application of its results SHALL carry only identifiers, counts, sizes, durations, statuses, tokens, costs, error types and reason classes; never a title, URL, note, extracted content, image, request or response body, fiche, tag or category name, API error message, validator message, nor the API key (RG-92, ADR-0027).

#### Scenario: Logs carry no content
- **GIVEN** items with distinctive titles, URLs, notes and contents, and an API key
- **WHEN** a pass sends a batch, refuses a send, and fails to copy an image
- **THEN** no log line contains any of these values, nor the API key

#### Scenario: Result logs carry no content
- **GIVEN** a batch whose results hold a valid response with a distinctive title, fiche text, tag and proposed category name, an invalid response, and an errored request whose error message is distinctive
- **WHEN** the batch is followed and applied
- **THEN** no log line contains any of these values, and the batch's log line carries its counts by outcome, its tokens and its cost

## ADDED Requirements

### Requirement: A batch in progress is followed until it ends
With an API key, the backend SHALL ask the AI service for the state of each user's batch in progress at start-up after the reconciliation, every 15 minutes, and at the start of each user's nightly pass. A batch still processing SHALL be left as it is; a failed query SHALL change nothing and be retried at the next occasion. Without a batch in progress, no call SHALL be made (RG-30, RG-31, RG-90).

#### Scenario: A batch is followed after a restart
- **GIVEN** a batch of the user in progress, the backend restarted, and the AI service reporting the batch ended with valid results
- **WHEN** the backend starts
- **THEN** the results are applied and the batch is ended with its cost

#### Scenario: A batch is followed every 15 minutes
- **GIVEN** a batch of the user in progress that the AI service reports processing, then ended 10 minutes later
- **WHEN** 15 minutes pass after start-up
- **THEN** the batch is queried once at start-up and once at the 15-minute tick, and its results are applied at the tick

#### Scenario: A batch still processing is left in progress
- **GIVEN** a batch of the user in progress that the AI service reports processing or canceling
- **WHEN** the batch is followed
- **THEN** the batch stays in progress, its items stay `submitted`, and no result is read

#### Scenario: A failed query is retried at the next tick
- **GIVEN** a batch of the user in progress and an AI service that answers its state query with a server error status (503), then with the batch ended
- **WHEN** the batch is followed twice, 15 minutes apart
- **THEN** after the first follow-up nothing changed and a log line gives the reason `poll_failed` with the status, and after the second the results are applied

#### Scenario: Nothing to follow makes no call
- **GIVEN** a user whose batches are all ended or failed
- **WHEN** the 15-minute tick fires
- **THEN** the AI service receives no call

#### Scenario: No API key, no follow-up
- **GIVEN** no `ANTHROPIC_API_KEY` and a batch of the user in progress
- **WHEN** the backend starts and 15 minutes pass
- **THEN** the AI service receives no call and the batch stays in progress

### Requirement: The results of an ended batch are read one line at a time
The backend SHALL read the results of an ended batch line by line. A line that cannot be read SHALL be skipped and counted; a result whose `custom_id` is not an item of the same user `submitted` in that batch SHALL be ignored and counted; every other result SHALL be applied once (RG-35, RG-90, ADR-0006).

#### Scenario: An unreadable line does not stop the read
- **GIVEN** an ended batch of three items whose results file holds a valid line, a line that is not JSON, and a valid line
- **WHEN** the batch is followed
- **THEN** the two readable results are applied, the item of the unreadable line is back to `extracted` without attempt counted, and the batch's log line counts one unreadable line

#### Scenario: A line longer than 4 MiB is unreadable
- **GIVEN** an ended batch whose results file holds a line of more than 4 MiB, then a valid line
- **WHEN** the batch is followed
- **THEN** the long line is skipped and counted as unreadable, and the valid line is applied

#### Scenario: A result for an item of another batch is ignored
- **GIVEN** an ended batch whose results hold a line for an item of the same user that is not `submitted` in this batch
- **WHEN** the batch is followed
- **THEN** that item is unchanged, the other results are applied, and the batch's log line counts one unknown result

#### Scenario: A result for another user's item is ignored
- **GIVEN** an ended batch of user A whose results hold a line for an item of user B `submitted` in B's own batch
- **WHEN** A's batch is followed
- **THEN** B's item, categories and tags are unchanged, and A's other results are applied

#### Scenario: A repeated line is applied once
- **GIVEN** an ended batch whose results hold the same valid line twice for one item
- **WHEN** the batch is followed
- **THEN** the item is `analyzed` with one `change_seq` advance for that line, and the second line counts as unknown

### Requirement: Applying the results survives a restart
Each item's result SHALL be written in its own transaction, only while the item is still `submitted` in that batch for that user. The batch SHALL be ended, with its tokens and cost written once, only after every line was read and every item settled. A stop during the read SHALL lose nothing and apply nothing twice (RG-35, RG-80).

#### Scenario: A restart in the middle of the read applies nothing twice
- **GIVEN** an ended batch of three items with valid results, and a backend stopped after the first item was written
- **WHEN** the backend starts again and follows the batch
- **THEN** the first item keeps the `change_seq` and `analyzed_at` of its first write, the two others are `analyzed`, and the batch is ended with the tokens of all three lines

#### Scenario: The cost is written once
- **GIVEN** an ended batch already applied with its cost recorded
- **WHEN** the backend follows the user's batches again
- **THEN** the AI service receives no call and the batch's tokens and cost are unchanged

### Requirement: A valid response is written to the item
A response that passes the validation SHALL set the item `analyzed` with its type, category, tags, title, fiche, language, freshness, content flag and issue, the model, prompt version and envelope version of its batch, the version of its fiche schema, its search text, `analyzed_at`, a later `updated_at` and a greater `change_seq`, and SHALL reset `attempts`, `last_error` and the cut-retry flag (RG-34, RG-37, RG-64, ADR-0007, ADR-0035).

#### Scenario: A valid response analyses the item
- **GIVEN** a `submitted` public item and a valid response choosing the type `tip`, an existing category, two tags, a title, a `tip` fiche, language `en`, freshness `timeless`, `content_ok` true
- **WHEN** the batch is applied
- **THEN** the item is `analyzed` with these values, `schema_version` 1, `analyzed_at` set, a later `updated_at` and a greater `change_seq`

#### Scenario: The versions come from the batch
- **GIVEN** a batch recorded with model `claude-haiku-4-5`, prompt version 3 and envelope version 1, and a backend now configured with another model
- **WHEN** a valid response of that batch is applied
- **THEN** the item records model `claude-haiku-4-5`, prompt version 3 and envelope version 1

#### Scenario: A poor content is recorded as partial
- **GIVEN** a valid response with `content_ok` false and `content_issue` « paywall »
- **WHEN** the batch is applied
- **THEN** the item is `analyzed` with `content_ok` false and `content_issue` « paywall »

#### Scenario: Failure counters are reset by a success
- **GIVEN** a `submitted` item with `attempts` 2, `last_error` `invalid_response` and its cut response already retried once
- **WHEN** a valid response of its batch is applied
- **THEN** the item is `analyzed` with `attempts` 0, `last_error` null, and its next request would carry the 4000 output cap

#### Scenario: The search text is the flattened fiche
- **GIVEN** a valid response whose `generic` fiche has the summary « Résumé A », a section « Titre B » with body « Corps C » and a code block `ls -la` in language `bash`, and the key point « Point D »
- **WHEN** the batch is applied
- **THEN** the item's search text holds « Résumé A », « Titre B », « Corps C », `ls -la` and « Point D » in that order, one per line, and neither `generic` nor `bash`

#### Scenario: A field corrected by hand is kept and proposes nothing
- **GIVEN** a `submitted` item whose category was corrected by hand, and a valid response proposing a new category « Kubernetes »
- **WHEN** the batch is applied
- **THEN** the item is `analyzed` and keeps its category, no category « Kubernetes » is created, and its type, tags, title and fiche are written

#### Scenario: Tags corrected by hand are kept
- **GIVEN** a `submitted` item whose tags were corrected by hand to `perso`, and a valid response with the new tags `alpha` and `beta`
- **WHEN** the batch is applied
- **THEN** the item keeps the single tag `perso` and no tag `alpha` nor `beta` is created

### Requirement: A response is validated in a fixed order and rejected whole
A `succeeded` request SHALL be validated in this order, the first failure rejecting the whole response: stop reason `tool_use`; exactly one call to `classify_item`; tags normalised, at least 2 distinct; the envelope schema; the fiche template consistent with the type; the type and the user's category exist. A rejected response SHALL write nothing and count one attempt (RG-35, RG-58, ADR-0006).

#### Scenario: A response without tool call is rejected
- **GIVEN** a `submitted` item with `attempts` 0 whose response holds only text
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with `attempts` 1, `last_error` `invalid_response`, no fiche, and its batch cleared

#### Scenario: Two tool calls are rejected
- **GIVEN** a response holding two `classify_item` calls
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: A call to another tool is rejected
- **GIVEN** a response holding one call to a tool named `other_tool`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: Another stop reason is rejected
- **GIVEN** two responses holding a valid `classify_item` call, with stop reasons `end_turn` and `refusal`
- **WHEN** the batch is applied
- **THEN** both items are `extracted` with one more attempt and `last_error` `invalid_response`

#### Scenario: An input that breaks the schema is rejected
- **GIVEN** a response whose `classify_item` input has no `title`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: A template that does not match the type is rejected
- **GIVEN** a response choosing the type `tip` with a `recipe` fiche
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: A generic fiche for a type with its own template is rejected
- **GIVEN** a response choosing the existing type `tip` with a `generic` fiche
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: A proposed type with its own template is rejected
- **GIVEN** a response proposing the new type `event` with a `tip` fiche
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt, and no type `event` exists

#### Scenario: An unknown type is rejected
- **GIVEN** a response choosing the type key `book`, which does not exist
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

#### Scenario: A category that is not the user's is rejected
- **GIVEN** three responses choosing a category id of another user, the id of a deleted category of the user, and an id that does not exist
- **WHEN** the batch is applied
- **THEN** the three items are `extracted` with one more attempt and nothing written

#### Scenario: A rejected response creates no proposal
- **GIVEN** a response proposing the category « Kubernetes », the type `event` and the new tags `alpha` and `beta`, whose fiche breaks the schema
- **WHEN** the batch is applied
- **THEN** no category, type or tag is created

#### Scenario: The third rejected response fails the item
- **GIVEN** a `submitted` item with `attempts` 2 and an invalid response
- **WHEN** the batch is applied
- **THEN** the item is `failed` with `attempts` 3 and `last_error` `invalid_response`, and its extracted content is kept

### Requirement: A cut response is retried once with a doubled cap
A response stopped by its output cap (`max_tokens`) SHALL write nothing and count no attempt: the first time, the item SHALL go back to `extracted` marked as retried, so that its next request carries the 8000 cap; when it was already retried, the item SHALL be `failed` with `last_error` `too_long`, its extracted content kept (RG-36).

#### Scenario: A first cut response is retried with a doubled cap
- **GIVEN** a `submitted` item with `attempts` 1 whose response stopped at `max_tokens`
- **WHEN** the batch is applied and the next nightly pass sends it
- **THEN** the item was back to `extracted` with `attempts` 1 and no fiche, and its next request carries an output cap of 8000

#### Scenario: A second cut response fails the item
- **GIVEN** a `submitted` item already retried once after a cut response, whose response stopped at `max_tokens` again
- **WHEN** the batch is applied
- **THEN** the item is `failed` with `last_error` `too_long`, its `attempts` unchanged, and its extracted content kept

### Requirement: An errored request counts an attempt
A request the AI service reports `errored` SHALL write nothing and count one attempt with `last_error` `request_errored`; at the third attempt the item SHALL be `failed`. Only the type of the service's error SHALL be logged, never its message (RG-35).

#### Scenario: An errored request counts an attempt
- **GIVEN** a `submitted` item with `attempts` 0 whose request is reported `errored` with the error type `invalid_request_error`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with `attempts` 1 and `last_error` `request_errored`

#### Scenario: The third errored request fails the item
- **GIVEN** a `submitted` item with `attempts` 2 whose request is reported `errored`
- **WHEN** the batch is applied
- **THEN** the item is `failed` with `attempts` 3 and `last_error` `request_errored`

### Requirement: A valid response the store cannot write counts an attempt
When the store refuses to write a valid response for another reason than the end of the read, the item SHALL be settled once with one attempt and `last_error` `storage_failed`, back to `extracted` without batch or `failed` at its third attempt, so that the batch can close; when that settling fails too, the read SHALL be cut and retried at the next follow-up (RG-35, decided with Ghislain on 2026-11-07).

#### Scenario: A valid response the store refuses counts an attempt
- **GIVEN** a `submitted` item with `attempts` 0 whose valid response the store refuses to write
- **WHEN** the batch is applied
- **THEN** the item is `extracted` without batch, with `attempts` 1 and `last_error` `storage_failed`, and the batch is ended with its cost

#### Scenario: The third refused write fails the item
- **GIVEN** a `submitted` item with `attempts` 2 whose valid response the store refuses to write
- **WHEN** the batch is applied
- **THEN** the item is `failed` with `attempts` 3 and `last_error` `storage_failed`

#### Scenario: A store that refuses every write cuts the read
- **GIVEN** a `submitted` item whose valid response the store refuses to write, and a store that also refuses to settle it
- **WHEN** the batch is applied
- **THEN** the item stays `submitted`, the batch stays without cost, and the next follow-up reads the batch again

### Requirement: Requests the service did not process go back to the queue
A request reported `expired` or `canceled`, an item of the batch without any result once every line was read, and every `succeeded` request of a batch recorded with an envelope version other than the backend's SHALL send the item back to `extracted` without batch, with its `attempts` and `last_error` unchanged (RG-35).

#### Scenario: An expired request goes back without attempt
- **GIVEN** a `submitted` item with `attempts` 1 whose request is reported `expired`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` without batch, with `attempts` 1, and the next nightly pass may send it

#### Scenario: A canceled request goes back without attempt
- **GIVEN** a `submitted` item with `attempts` 0 whose request is reported `canceled`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` without batch, with `attempts` 0

#### Scenario: An item without result goes back without attempt
- **GIVEN** an ended batch of two items whose results file holds a line for one item only
- **WHEN** the batch is followed
- **THEN** the other item is `extracted` without batch, with its `attempts` unchanged

#### Scenario: A batch sent with another envelope version goes back without attempt
- **GIVEN** an ended batch recorded with envelope version 2 while the backend's envelope version is 1, holding a valid response
- **WHEN** the batch is followed
- **THEN** the item is `extracted` without batch, with its `attempts` unchanged, no fiche, and the batch is ended with the tokens of that line

### Requirement: A batch whose results are no longer served is closed
When the AI service answers that the batch does not exist, or that its results are archived or not found, the batch SHALL be ended with the tokens of the lines already read (none when nothing was read) and their cost, and its items still `submitted` SHALL go back to `extracted` without attempt counted, so that the next night can send (RG-35, RG-80, RG-81).

#### Scenario: A batch unknown to the service is closed
- **GIVEN** a batch of the user in progress with two `submitted` items, for which the AI service answers « not found »
- **WHEN** the batch is followed
- **THEN** the batch is ended with cost 0, both items are `extracted` without batch with their `attempts` unchanged, and a log line gives the reason `results_unavailable`

#### Scenario: An archived batch is closed
- **GIVEN** a batch of the user in progress that the AI service reports ended and archived
- **WHEN** the batch is followed
- **THEN** the batch is ended with cost 0 and its items are `extracted` without attempt counted

#### Scenario: Results that disappear after a cut read keep the tokens read
- **GIVEN** an ended batch of two items whose first results download is cut after one valid line, and an AI service that answers « not found » at the next follow-up
- **WHEN** the batch is followed twice
- **THEN** the first item is `analyzed`, the second is `extracted` without attempt counted, and the batch is ended with the tokens and cost of the first line

#### Scenario: A closed batch no longer blocks the next night
- **GIVEN** a batch closed because its results were no longer served, and one `extracted` public item
- **WHEN** the nightly pass runs
- **THEN** a batch is sent

### Requirement: Private and deleted items keep or drop their result
An item checked Privé after its batch left SHALL get that batch's result like any other item. A result for an item deleted after its batch left SHALL write nothing on the item; its tokens SHALL still count (RG-12, RG-14, RG-79).

#### Scenario: An item made private after its batch left gets its result
- **GIVEN** a `submitted` item checked Privé after its batch was sent, and a valid response
- **WHEN** the batch is applied
- **THEN** the item is `analyzed` with its fiche and still private

#### Scenario: A cut response of an item made private is not sent again
- **GIVEN** a `submitted` item checked Privé after its batch was sent, whose response stopped at `max_tokens`
- **WHEN** the batch is applied and the next nightly pass runs
- **THEN** the item is `extracted`, private, and no request of the next batch carries its id

#### Scenario: A deleted item's result is dropped
- **GIVEN** a `submitted` item deleted after its batch was sent, and a valid response proposing the category « Kubernetes » and the new tag `alpha`
- **WHEN** the batch is applied
- **THEN** the item has no fiche, no category « Kubernetes » nor tag `alpha` is created, and the batch's tokens include that line's

### Requirement: Each batch records its tokens and its cost
Once its results are read, a batch SHALL record the sums, over every `succeeded` line, of input, output, cache-write and cache-read tokens, and its cost in US dollars: half of the input price times input tokens, plus 1.25 times it per 5-minute cache-write token, 2 times per 1-hour one, 0.1 times per cache-read token, plus the output price times output tokens (RG-80, RG-81, ADR-0021).

#### Scenario: The cost follows the batch price
- **GIVEN** prices of 1 and 5 US dollars per million input and output tokens, and an ended batch whose succeeded lines total 10 000 input, 2 000 output, 4 000 five-minute cache-write and 36 000 cache-read tokens
- **WHEN** the batch is applied
- **THEN** the batch records these tokens (cache writes 4 000) and a cost of 0.0143

#### Scenario: A one-hour cache write costs twice the input price
- **GIVEN** the same prices and an ended batch whose succeeded lines total 1 000 one-hour cache-write tokens and nothing else
- **WHEN** the batch is applied
- **THEN** the batch records 1 000 cache-write tokens and a cost of 0.001

#### Scenario: Every succeeded line counts in the tokens
- **GIVEN** an ended batch whose succeeded lines are a valid response, a rejected response, a cut response, the response of a deleted item and a result for an unknown item, each with 100 input tokens, plus an errored and an expired request
- **WHEN** the batch is applied
- **THEN** the batch records 500 input tokens

#### Scenario: A batch without any processed request costs nothing
- **GIVEN** an ended batch whose requests are all `expired`
- **WHEN** the batch is applied
- **THEN** the batch is ended with zero tokens and cost 0, and the next nightly pass may send

#### Scenario: The cost counts in the month the batch was sent
- **GIVEN** `MONTHLY_BUDGET_USD` 5, a batch created at 23:30 Paris time on 31 January 2027 and ended on 1 February with a cost of 5.00, and one `extracted` public item
- **WHEN** the pass runs on 2 February 2027 at 02:00 Paris time
- **THEN** the batch is sent

### Requirement: The token prices are configured
`ANALYSIS_INPUT_PRICE_USD_PER_MTOK` and `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK` SHALL give the standard prices of `ANALYSIS_MODEL`, 1 and 5 when absent; a value that is not a positive decimal number SHALL stop the start-up with an error naming the variable. The start-up SHALL log the model and both prices (RG-80, ADR-0021).

#### Scenario: The prices default to the Haiku 4.5 prices
- **GIVEN** neither price variable set
- **WHEN** the backend starts
- **THEN** a start-up log line gives the model `claude-haiku-4-5`, the input price 1 and the output price 5

#### Scenario: An invalid price stops the start-up
- **GIVEN** `ANALYSIS_INPUT_PRICE_USD_PER_MTOK` set to `0`, `-1` or `abc`
- **WHEN** the backend starts
- **THEN** it refuses to start with an error naming `ANALYSIS_INPUT_PRICE_USD_PER_MTOK`
