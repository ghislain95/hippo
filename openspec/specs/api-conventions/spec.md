# api-conventions Specification

## Purpose

Defines the rules that every route of the Hippo HTTP API follows, whatever its domain: authentication, error format, and the answers to a route that is declared but not yet implemented, to an unknown route and to a request that breaks the contract.

## Requirements

### Requirement: API routes require the bearer token
Every route under `/api/` SHALL require the header `Authorization: Bearer <token>` matching the configured API token, compared in constant time, and SHALL answer `401` with a problem body and a `WWW-Authenticate: Bearer` header otherwise, before any routing or validation (ADR-0016, RG-90).

#### Scenario: Request without token is rejected
- **WHEN** a client sends `GET /api/items` without an `Authorization` header
- **THEN** the API answers `401` with `Content-Type: application/problem+json`, a `WWW-Authenticate` header, and a body whose `status` is `401`

#### Scenario: Request with a wrong token is rejected
- **WHEN** a client sends `GET /api/items` with `Authorization: Bearer <another token>`
- **THEN** the API answers `401` with a problem body

#### Scenario: The bearer scheme is case-insensitive
- **WHEN** a client sends `GET /api/items` with `Authorization: bearer <valid token>`
- **THEN** the request is authenticated and reaches the route

#### Scenario: Unknown API paths are not revealed without a token
- **WHEN** a client sends `GET /api/does-not-exist` without a token
- **THEN** the API answers `401`, not `404`

### Requirement: The health check is public
`GET /healthz` SHALL answer without any token: `200` with `{"status": "ok", "version": <backend version>}` when the database answers, `503` with a problem body otherwise (ADR-0019).

#### Scenario: Health check without token
- **WHEN** a client sends `GET /healthz` without an `Authorization` header and the database answers
- **THEN** the API answers `200` with `status` equal to `ok` and the running version

#### Scenario: Health check with the database down
- **WHEN** a client sends `GET /healthz` and the database does not answer
- **THEN** the API answers `503` with a problem body

### Requirement: Errors are problem documents
Every error response of the API SHALL be `application/problem+json` (RFC 9457) with at least `type`, `title` and `status` equal to the HTTP status; a business error SHALL use a `type` of the form `urn:hippo:problem:<slug>` listed in the contract, and any other error SHALL use `about:blank` (ADR-0017).

#### Scenario: A generic error uses about:blank
- **WHEN** a client sends `GET /api/items` without a token
- **THEN** the problem body has `type` equal to `about:blank`, a non-empty `title` and `status` equal to `401`

#### Scenario: A business error uses its catalogue type
- **WHEN** an authenticated client calls a declared route that is not implemented yet
- **THEN** the problem body has `type` equal to `urn:hippo:problem:not-implemented`

### Requirement: Declared but not implemented routes answer 501
A route, request format or field that is declared in the contract but not implemented yet SHALL answer `501` with the problem type `urn:hippo:problem:not-implemented` to an authenticated request, until the story that owns it implements it.

#### Scenario: Declared route not implemented yet
- **WHEN** an authenticated client sends `GET /api/items`
- **THEN** the API answers `501` with the problem type `urn:hippo:problem:not-implemented`

#### Scenario: Every declared operation answers 501 until implemented
- **WHEN** an authenticated client calls any operation of the contract other than `GET /healthz` and the operations already implemented, with a request that respects the contract
- **THEN** the API answers `501` with the problem type `urn:hippo:problem:not-implemented`

### Requirement: Unknown routes answer 404
A request whose path or method matches no operation of the contract SHALL answer `404` with a problem body; under `/api/`, only after the token was accepted.

#### Scenario: Unknown path outside the API
- **WHEN** a client sends `GET /nope`
- **THEN** the API answers `404` with a problem body

#### Scenario: Unknown path under the API with a valid token
- **WHEN** an authenticated client sends `GET /api/does-not-exist`
- **THEN** the API answers `404` with a problem body

#### Scenario: Wrong method on a declared path
- **WHEN** an authenticated client sends `PUT /api/items`, which the contract does not declare
- **THEN** the API answers `404` with a problem body

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

### Requirement: JSON bodies are bounded
A request whose `application/json` body exceeds 1 MiB SHALL answer `413` with a problem body of type `about:blank`, without the route running (ADR-0019).

#### Scenario: Oversized JSON body
- **WHEN** an authenticated client sends `POST /api/items` with a JSON body of 2 MiB
- **THEN** the API answers `413` with a problem body of type `about:blank` and nothing is stored
