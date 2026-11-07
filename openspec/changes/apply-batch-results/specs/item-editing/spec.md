# Spec Delta

## ADDED Requirements

### Requirement: An answered item carries its classification and its fiche
Every `Item` the API answers SHALL carry the item's content type key, category id, tag ids in ascending order and fiche once analysed, and `null`, `[]` and no fiche before; `last_error` SHALL be `null` or one of the codes `invalid_response`, `request_errored`, `too_long` (RG-34, RG-35, RG-36, ADR-0017, ADR-0035).

#### Scenario: An analysed item is answered with its classification
- **GIVEN** an `analyzed` item of the user with the type `tip`, a category, the tags `go` and `testing`, and a `tip` fiche
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with `content_type` `tip`, its `category_id`, the two tag ids in ascending order, and the fiche with `template` `tip`

#### Scenario: An item not analysed yet has no classification
- **GIVEN** an `extracted` item of the user
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with `content_type` null, `category_id` null, `tag_ids` `[]` and no `fiche` member

#### Scenario: A deleted tag is not answered
- **GIVEN** an `analyzed` item of the user carrying the tags `go` and `golang`, the tag `golang` being deleted
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer's `tag_ids` holds only the id of `go`

#### Scenario: A failed item answers its error code
- **GIVEN** an item of the user `failed` after a second cut response
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with `status` `failed` and `last_error` `too_long`
