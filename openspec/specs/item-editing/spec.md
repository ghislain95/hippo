# item-editing Specification

## Purpose
Describes how the user changes an existing item through `PATCH /api/items/{item_id}`: today the privacy flag, which never changes the processing status (RG-14); the other members answer 501 until the corrections are implemented (E4-03).

## Requirements

### Requirement: The privacy flag of an item can be changed
`PATCH /api/items/{item_id}` with `{"is_private": true}` or `{"is_private": false}` SHALL set the privacy flag of the authenticated user's item and answer `200` with the complete item, as the capture answers it. A change of the flag SHALL advance the item's `updated_at` and `change_seq`; a value equal to the current one SHALL write nothing (RG-10, RG-14, ADR-0035).

#### Scenario: An item is made private
- **GIVEN** a public item of the user
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with the item, `is_private` true, and the stored item has a later `updated_at` and a greater `change_seq`

#### Scenario: A private item is made public
- **GIVEN** a private item of the user
- **WHEN** the user sends `{"is_private": false}` for this item
- **THEN** the answer is `200` with the item and `is_private` false

#### Scenario: The same value writes nothing
- **GIVEN** a private item of the user
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with `is_private` true, and the item's `updated_at` and `change_seq` are unchanged

### Requirement: Changing the privacy flag never changes the processing status
Checking or unchecking Privé SHALL leave the item's `status` and fiche as they are. Made public, an `extracted` item is back in the analysis queue, since the nightly batch takes the `extracted` items that are not private; an `analyzed` item keeps its fiche and is not reanalysed; a `failed` item stays `failed` until « Relancer » (RG-12, RG-14, RG-35).

#### Scenario: A private extracted item made public is back in the analysis queue
- **GIVEN** a private item of the user with `status` `extracted`
- **WHEN** the user sends `{"is_private": false}` for this item
- **THEN** the item is `extracted` and not private

#### Scenario: An analysed item made private keeps its status
- **GIVEN** a public item of the user with `status` `analyzed` and an `analyzed_at`
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the item is private, still `analyzed`, with the same `analyzed_at`

#### Scenario: An analysed item made public is not reanalysed
- **GIVEN** a private item of the user with `status` `analyzed` and an `analyzed_at`
- **WHEN** the user sends `{"is_private": false}` for this item
- **THEN** the item is public, still `analyzed`, with the same `analyzed_at` and `attempts`

#### Scenario: A failed item made public stays failed
- **GIVEN** a private item of the user with `status` `failed`
- **WHEN** the user sends `{"is_private": false}` for this item
- **THEN** the item is public and still `failed`

### Requirement: Only the owner can change an item
An unknown item, a deleted item or another user's item SHALL answer `404` and change nothing (RG-90).

#### Scenario: Another user's item
- **GIVEN** an item of another user
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `404` and the other user's item is still public

#### Scenario: Unknown item
- **WHEN** the user sends `{"is_private": true}` for an id that matches no item
- **THEN** the answer is `404`

### Requirement: The edit body follows the contract strictly
The body of `PATCH /api/items/{item_id}` SHALL be a JSON object with at least one member of `ItemPatch`. An undeclared member, data after the value, a member of the wrong type, an empty object or `is_private` null SHALL answer `400` and change nothing (ADR-0019).

#### Scenario: Undeclared member
- **WHEN** the user sends `{"is_private": true, "favorite": true}` for one of their public items
- **THEN** the answer is `400` and the item is still public

#### Scenario: Empty object
- **WHEN** the user sends `{}` for one of their items
- **THEN** the answer is `400`

#### Scenario: Wrong type or null
- **WHEN** the user sends `{"is_private": "yes"}`, then `{"is_private": null}`, for one of their public items
- **THEN** both answers are `400` and the item is still public

### Requirement: Members other than the privacy flag answer 501 until implemented
A request whose body carries `content_type`, `category_id`, `tags`, `is_pinned`, `annotations` or `user_note` SHALL answer `501` with the problem type `urn:hippo:problem:not-implemented` and apply none of its members, `is_private` included, until the story that owns them implements them (E4-03).

#### Scenario: Pin not implemented yet
- **WHEN** the user sends `{"is_pinned": true}` for one of their items
- **THEN** the answer is `501` with the problem type `urn:hippo:problem:not-implemented`

#### Scenario: A mixed request applies nothing
- **GIVEN** a public item of the user
- **WHEN** the user sends `{"is_private": true, "tags": ["travel"]}` for this item
- **THEN** the answer is `501` and the item is still public, with the same `updated_at`
