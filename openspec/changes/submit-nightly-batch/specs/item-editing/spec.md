# Spec Delta

## MODIFIED Requirements

### Requirement: Changing the privacy flag never changes the processing status
Checking or unchecking Privé SHALL leave the item's `status` and fiche as they are. Made public, an `extracted` item is back in the analysis queue, since the nightly batch takes the `extracted` items that are not private; an `analyzed` item keeps its fiche and is not reanalysed; a `failed` item stays `failed` until « Relancer ». Checked Privé, a `submitted` item stays `submitted` and keeps its batch: if that batch has not left yet, the nightly pass takes the item out before the send, back to `extracted` without fiche (item-analysis); otherwise its content has already left, and it keeps that batch's result (RG-12, RG-14, RG-35).

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

#### Scenario: A submitted item made private stays submitted
- **GIVEN** a public item of the user that the nightly batch has moved to `submitted`
- **WHEN** the user sends `{"is_private": true}` for this item
- **THEN** the answer is `200` with the item private and still `submitted`, and the stored item keeps its batch
