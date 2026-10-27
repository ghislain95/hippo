# Spec Delta

## MODIFIED Requirements

### Requirement: Declared but not implemented routes answer 501
A route, request format or field that is declared in the contract but not implemented yet SHALL answer `501` with the problem type `urn:hippo:problem:not-implemented` to an authenticated request, until the story that owns it implements it.

#### Scenario: Declared route not implemented yet
- **WHEN** an authenticated client sends `GET /api/items`
- **THEN** the API answers `501` with the problem type `urn:hippo:problem:not-implemented`

#### Scenario: Every declared operation answers 501 until implemented
- **WHEN** an authenticated client calls any operation of the contract other than `GET /healthz` and the operations already implemented, with a request that respects the contract
- **THEN** the API answers `501` with the problem type `urn:hippo:problem:not-implemented`

### Requirement: Requests that break the contract answer 400
A request whose parameters or body do not match the types of the contract SHALL answer `400` with a problem body of type `about:blank`, before the route runs. For an implemented operation, a JSON body that holds a member the contract does not declare, or data after the JSON value, SHALL also answer `400` with type `about:blank` (ADR-0017).

#### Scenario: Query parameter of the wrong type
- **WHEN** an authenticated client sends `GET /api/items?limit=abc`
- **THEN** the API answers `400` with a problem body of type `about:blank`

#### Scenario: Malformed JSON body
- **WHEN** an authenticated client sends `POST /api/items` with `Content-Type: application/json` and the body `{not json`
- **THEN** the API answers `400` with a problem body of type `about:blank`

#### Scenario: Undeclared member in the body of an implemented operation
- **WHEN** an authenticated client sends `POST /api/items` with `{"url": "https://example.com/a", "unknown": 1}`
- **THEN** the API answers `400` with a problem body of type `about:blank`

## ADDED Requirements

### Requirement: JSON bodies are bounded
A request whose `application/json` body exceeds 1 MiB SHALL answer `413` with a problem body of type `about:blank`, without the route running (ADR-0019).

#### Scenario: Oversized JSON body
- **WHEN** an authenticated client sends `POST /api/items` with a JSON body of 2 MiB
- **THEN** the API answers `413` with a problem body of type `about:blank` and nothing is stored
