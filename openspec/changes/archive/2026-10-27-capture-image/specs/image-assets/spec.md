# Spec Delta

## Purpose

Serves the images captured by a user through `GET /api/assets/{asset_id}`: the stored file, unchanged, to its owner only.

## ADDED Requirements

### Requirement: An asset is served to its owner as captured
`GET /api/assets/{asset_id}` SHALL answer `200` with the stored file, byte for byte as captured, `Content-Type` its recognised type, `X-Content-Type-Options: nosniff` and `Cache-Control: private, max-age=31536000, immutable` (an asset never changes), to the user who owns it; range requests and validators are not supported (RG-90, ADR-0004, ADR-0018).

#### Scenario: Download a captured image
- **GIVEN** the user captured a PNG image
- **WHEN** they request its asset
- **THEN** the answer is `200` with `Content-Type: image/png`, `X-Content-Type-Options: nosniff`, `Cache-Control: private, max-age=31536000, immutable`, and a body equal to the file sent

#### Scenario: Private item's image is served to its owner
- **GIVEN** the user captured an image marked private
- **WHEN** they request its asset
- **THEN** the answer is `200` with the file

### Requirement: Another user's or an unknown asset is not found
`GET /api/assets/{asset_id}` SHALL answer `404` with a problem body for an asset that does not exist or belongs to another user, and `401` without a valid token (RG-90, ADR-0016).

#### Scenario: Another user's asset
- **GIVEN** user A captured an image
- **WHEN** user B requests its asset
- **THEN** the answer is `404` with a problem body

#### Scenario: Unknown asset
- **WHEN** the user requests an asset id that was never created
- **THEN** the answer is `404` with a problem body

#### Scenario: Asset without token
- **WHEN** a client requests an existing asset without `Authorization` header
- **THEN** the answer is `401`
