# Spec Delta

## ADDED Requirements

### Requirement: Every response forbids content sniffing
Every response of the backend, `/healthz` and errors included, SHALL carry `X-Content-Type-Options: nosniff`, so that a client never interprets a body as another type than the one declared (ADR-0017).

#### Scenario: Every response carries nosniff
- **WHEN** a client calls `GET /healthz`, an API route without token, an unknown route and a capture
- **THEN** every answer carries the header `X-Content-Type-Options: nosniff`
