# Spec Delta

## Purpose

Gives the iPhone app its foundation before the library exists: the build-time configuration shared with the share extension, the theme (light and dark, text sizes following iOS), and the home screen that shows the server's state. Every scenario runs in jest; what only a device can show is in the QA checklist of the change's design.

## ADDED Requirements

### Requirement: The build refuses a configuration without a token or with an insecure URL
At `prebuild`, the build SHALL fail, naming the variable at fault and never printing the token, when `HIPPO_API_TOKEN` is missing, shorter than 32 characters or holds a character outside letters, digits and `-._~+/=`, and when `HIPPO_API_URL` is neither an `https` URL nor an `http` URL on `127.0.0.1` or `localhost` (ADR-0014, ADR-0015, ADR-0016, ADR-0027).

#### Scenario: Missing token
- **WHEN** the build configuration is checked with `HIPPO_API_URL=https://hippo.example.ts.net` and no `HIPPO_API_TOKEN`
- **THEN** it fails with a message naming `HIPPO_API_TOKEN`

#### Scenario: Token too short, not echoed
- **WHEN** the build configuration is checked with the token `abc"def`
- **THEN** it fails with a message naming `HIPPO_API_TOKEN` that does not contain `abc"def`

#### Scenario: Plain HTTP to a tailnet address
- **WHEN** the build configuration is checked with `HIPPO_API_URL=http://hippo.example.ts.net` and a valid token
- **THEN** it fails with a message naming `HIPPO_API_URL`

#### Scenario: Local backend on the simulator
- **WHEN** the build configuration is checked with `HIPPO_API_URL=http://127.0.0.1:8080` and a valid token
- **THEN** it passes

#### Scenario: Default URL
- **WHEN** the build configuration is checked with a valid token and no `HIPPO_API_URL`
- **THEN** it passes with the URL `http://127.0.0.1:8080`

### Requirement: The extension receives the same URL and token as the app
At `prebuild`, the build SHALL write, for the share extension, the server URL without a trailing slash and the token, from the same variables as the app's configuration, into a git-ignored generated source file (ADR-0014, ADR-0027).

#### Scenario: Generated configuration of the extension
- **WHEN** the configuration is generated with `HIPPO_API_URL=https://hippo.example.ts.net/` and a valid token
- **THEN** the generated source holds the URL `https://hippo.example.ts.net` and the token, as string literals, with a header saying it is generated and never committed

#### Scenario: The generated file is ignored by git
- **WHEN** git is asked whether the generated configuration file of the extension is ignored
- **THEN** it is

### Requirement: Every call of the app carries the build-time token
Every request of the app to `/api` SHALL carry `Authorization: Bearer <token>` with the token injected at build time, and go to the server URL injected at build time (ADR-0014, ADR-0016).

#### Scenario: Health check sent to the configured server
- **WHEN** the home screen checks the server configured as `http://hippo.test`
- **THEN** the request goes to `http://hippo.test/healthz` with the configured bearer token

### Requirement: The home screen explains how to capture and shows the server's state
The home screen SHALL show the title « Hippo », the server state « Serveur joignable » when `GET /healthz` answers `200` and « Serveur injoignable » otherwise, the text « Partage un lien, un texte ou une image depuis n'importe quelle appli. Hippo le range dans la nuit. » and the muted text « La bibliothèque arrive avec la prochaine version. » (RG-01).

#### Scenario: Server reachable
- **WHEN** the home screen is shown and `GET /healthz` answers `200`
- **THEN** « Hippo », « Serveur joignable », the capture explanation and « La bibliothèque arrive avec la prochaine version. » are visible

#### Scenario: Server unreachable
- **WHEN** the home screen is shown and `GET /healthz` answers `503`
- **THEN** « Serveur injoignable » is visible, announced as an alert, and the capture explanation stays visible

### Requirement: The theme follows the iOS appearance
The app SHALL use the light theme when iOS is in light appearance or reports none, and the dark theme when iOS is in dark appearance, for its screens and its navigation bar; every colour SHALL come from the theme tokens.

#### Scenario: Dark appearance
- **WHEN** iOS reports the dark appearance and the home screen is shown
- **THEN** its background and text use the dark tokens

#### Scenario: Light appearance
- **WHEN** iOS reports the light appearance and the home screen is shown
- **THEN** its background and text use the light tokens

#### Scenario: No appearance reported
- **WHEN** iOS reports no appearance and the home screen is shown
- **THEN** it uses the light tokens

### Requirement: Text sizes follow the iOS text size setting
Every text of the app SHALL take its size from the theme's text sizes and SHALL keep font scaling enabled, so that the iOS text size setting (Dynamic Type) enlarges or reduces it.

#### Scenario: Texts of the home screen scale
- **WHEN** the home screen is shown
- **THEN** every text has a font size from the theme's text sizes and none disables font scaling

### Requirement: The extension's colours come from the app's theme tokens
The colours of the share extension SHALL be generated from the same token file as the app's theme, with a light and a dark value for each (ADR-0036).

#### Scenario: Same tokens in the app and the extension
- **WHEN** the extension's target configuration is evaluated
- **THEN** each of its colours has the light and dark values of the token of the same name in the app's theme
