# Design

## Context

See proposal.md — Why. Current state, read on 2026-11-07:

- `infra/docker-compose.yml` runs two services: `tailscale` (`tailscale/tailscale:latest`, kernel mode, `NET_ADMIN`, `/dev/net/tun`, `TS_SERVE_CONFIG=/config/serve.json`) and `hippo` (`ghcr.io/ghislain95/hippo:${HIPPO_IMAGE_TAG:-latest}`, `network_mode: service:tailscale`, `HTTP_ADDR=127.0.0.1:8080`, `mem_limit: 384m`, healthcheck `/hippo healthcheck`). No log rotation, `TS_AUTHKEY` required.
- `infra/tailscale/config/serve.json` proxies `https://${TS_CERT_DOMAIN}:443/` to `http://127.0.0.1:8080`, Funnel off.
- `.github/workflows/image.yml` publishes `latest` and `sha-<short sha>` for `linux/amd64` and `linux/arm64` on every push to `main` touching `backend/**`. `latest` was published on 2026-11-07 (run of PR ghislain95/hippo#21), amd64 present (`docker manifest inspect`).
- `make check-infra` only runs `docker compose config --quiet` on both files. `protect-files.sh` blocks any `docker compose config` without `--quiet`, since it would print `infra/.env`.
- The backend logs `hippo started` with the full commit SHA (`backend/cmd/hippo/main.go:283`) and `analysis prices` at start-up; without `ANTHROPIC_API_KEY`, it starts and logs a warning, and sends nothing.
- No backend, contract, migration, prompt or schema change in this change.

## Goals / Non-Goals

**Goals:**
- A runbook Ghislain can follow from a bare NAS to Hippo answering in HTTPS on the tailnet, then update and roll back.
- The security invariants of the production compose enforced in CI instead of remembered.

**Non-Goals:**
- Any change of behaviour of the backend; backups (`nightly-backup`); a healthcheck of the tailnet link.

## Decisions

### D1 — Sidecar stays in kernel mode

`TS_USERSPACE=false`, `cap_add: [NET_ADMIN]`, `devices: [/dev/net/tun]` stay. In userspace mode, tailscaled sets `ProcessLocalIPs` for netstack and forwards a peer's TCP connection to the node's IP, on a port without a `serve` handler, to `127.0.0.1:<port>` (tailscale v1.104.5 source: `cmd/tailscaled/netstack.go:41`, `wgengine/netstack/netstack.go:1799-1800`, read by the adversarial pass): `:8080` would be reachable in clear from the tailnet, around `serve`. In kernel mode the node's tailnet IP is a real interface of the container's namespace, and the backend bound to `127.0.0.1` is not reachable on it. `NET_ADMIN` applies to the container's own network namespace, not to the NAS. Alternative rejected: userspace + ACL restricting ports (a tailnet setting, out of scope) or a Unix socket between `serve` and the backend (backend change).

### D2 — Enrol once, then forget the auth key

`TS_AUTH_ONCE=true` and `TS_AUTHKEY=${TS_AUTHKEY:-}`. containerboot logs in with the key only when the persisted state has no node (`TS_AUTH_ONCE`, containerboot `settings.go`, read at v1.104.5 by the adversarial pass; exact behaviour with an empty key once enrolled: unverified, checked during the QA by a restart after the key is erased). The runbook has Ghislain erase the consumed key from `.env` after enrolment. The key is one-off, pre-approved, non-ephemeral and untagged: no ACL change; key expiry is then disabled on the node (device setting).

### D3 — Hippo follows its sidecar

`depends_on: tailscale: {condition: service_started, restart: true}` (Compose ≥ 2.17; local Compose is v2.38.2; the NAS version is checked by the runbook's prerequisites). When Compose restarts the sidecar, it restarts `hippo`, whose network namespace is the sidecar's. A restart from the UGOS interface may bypass Compose: the runbook says to restart the project, never `tailscale` alone. The QA covers a project restart and a NAS reboot.

### D4 — Versions pinned

- `HIPPO_IMAGE_TAG` required (`${HIPPO_IMAGE_TAG:?…}`): a missing line stops `docker compose up` instead of pulling `latest`. `.env.example` keeps `HIPPO_IMAGE_TAG=latest` as a working value for the Mac, with the runbook pinning `sha-<short sha>` on the NAS. The tag exists only for merges touching `backend/**`: the runbook reads it on the GHCR package page and checks it with `docker manifest inspect` before writing it; after start-up, the `version` field of `hippo started` (full SHA) starts with the tag's short SHA.
- Tailscale image `tailscale/tailscale:v1.104.4`, the latest tag published on Docker Hub on 2026-11-07 (`v1.104.5` is a GitHub release without image yet). Dependabot gets a `docker-compose` entry on `/infra` (weekly, Monday, like the other ecosystems, ADR-0030). How Dependabot treats `ghcr.io/ghislain95/hippo:${HIPPO_IMAGE_TAG:?…}`: unverified; an unparsable reference is expected to be skipped.

### D5 — Bounded logs

`logging: {driver: json-file, options: {max-size: "10m", max-file: "3"}}` on both services (YAML anchor). `json-file` is the default driver, so the UGOS Docker interface reads it as before; the `local` driver is not chosen, its reading by UGOS is unverified.

### D6 — Static check of the compose files

New script `infra/check-compose.sh` (bash, `jq`, `shellcheck -S warning`), called by `make check-infra` after the two `config --quiet` runs, so by the CI `infra` job too. It renders each file with `docker compose --env-file /dev/null -f <file> config --no-interpolate --format json`: `--env-file` replaces the default `infra/.env` (never loaded), `--no-interpolate` keeps `${…}` as written (no shell value enters the output). It asserts, one message per failure:

- production: no `ports` on any service; `hippo` environment holds `HTTP_ADDR=127.0.0.1:8080` and `network_mode` is `service:tailscale`; `tailscale` has `cap_add == ["NET_ADMIN"]`, only `/dev/net/tun` in `devices`, `TS_USERSPACE=false`, no `privileged`, image not `:latest`; `hippo` image tag is `${HIPPO_IMAGE_TAG:?…}`; every service has `logging`; `hippo` has `mem_limit`;
- both files: the set of variable names of the `hippo` environment equals the set of uncommented `^[A-Z_][A-Z0-9_]*=` names of `infra/.env.example`, minus an explicit exclusion list (`TS_AUTHKEY`, `HIPPO_IMAGE_TAG`: compose-level, not backend settings), plus an explicit list of names set by the compose only (`HTTP_ADDR` in production).

`jq` is already required by `protect-files.sh` and present on `ubuntu-latest`: no new dependency. The `--format json --no-interpolate` rendering cannot be run by an agent outside `make` (the hook blocks `docker compose config`): unverified until the first `make check-infra`.

`protect-files.sh` is not changed: running the script through `make check-infra` is the path the hook itself recommends, and the script never reads `infra/.env`.

### D7 — The runbook

`docs/runbook-nas.md`, French, numbered sections like `docs/runbook-ios.md`, commands in fenced blocks with placeholders (`<volume>`, `<tailnet>`, `<sha>`), nothing personal (ADR-0027). Order: 0 prerequisites and checks · 1 folder, permissions, files at a SHA · 2 `.env` · 3 Tailscale key · 4 phase 1 start and checks · 5 phase 2 API key and the paid end-to-end test (agreed 2026-11-07) · 6 day-to-day in the UGOS interface · 7 update · 8 rollback · 9 no backup yet (link to E6-03) · 10 troubleshooting. Placement: an admin-only shared folder, excluded from UGOS sync and cloud backups (RG-92, ADR-0020); `.env` `600`; `tailscale/state` `700`; `data/` owned by 65532 before the first `up` (a missing bind-mount source is created by Docker as root and SQLite fails).

Rollback: goose (v3.29.0) ignores applied versions unknown to the binary (`internal/gooseutil/resolve.go:45-63`, read by the adversarial pass), so an old image starts on a newer schema without error. The runbook checks, before each update, whether `backend/internal/store/migrations/` changed between the two SHAs (GitHub compare URL); copies `data/` with Hippo stopped (WAL: `-wal`, `-shm` go with it) to a sibling folder; and rolls back past a migration only by restoring that copy.

### D8 — Docs alignment

`docs/spec-v1.md` « Déploiement »: GHCR public (ADR-0027), compose sketch aligned (pinned images, tun, loopback listener, log rotation). `infra/CLAUDE.md`: the invariants and their check. `infra/.env.example` NAS section. `docs/CLAUDE.md`, `README.md`: link to the runbook. `docs/dev/setup-report.md` « À faire » 6 and 8. Backlog E6-01, E6-02, E6-04 done after the QA. ADR-0019's « GHCR privé » is already amended by ADR-0027; no new ADR (no decision contradicts an accepted one).

### Packages, contract, migration, prompt, rules

- Go packages: none. App: none. `api/openapi.yaml`: none. Migration: none. Prompt and fiche schemas: none.
- `user_id` filtering, private items (RG-12), budget (RG-81): paths untouched. Phase 1 without API key makes the checks unable to send anything.

## Risks / Trade-offs

- [UGOS ships an old Compose without `depends_on.restart`] → prerequisites check `docker compose version`; fallback documented: drop the key `restart: true` locally is not allowed (versioned file), so the runbook asks to update the Docker app first.
- [UGOS shared-folder ACLs override the `chown`] → `ls -ln` check; fallback: a folder outside shared folders created with `sudo` (unverified on UGOS).
- [Healthcheck green while the node is offline (key expiry, sidecar down)] → key expiry disabled; troubleshooting row; monitoring is a non-goal.
- [Dependabot bumps Tailscale in the repo, the NAS keeps the old one] → runbook § update: fetch the compose again at the new SHA.
- [Empty `TS_AUTHKEY` with `TS_AUTH_ONCE` after enrolment behaves unexpectedly] → QA restart after erasing the key; if it fails, keep the consumed key in `.env` (harmless once consumed) and record it here.
- [No backup until `nightly-backup`] → warning, copy of `data/` before each update, nothing precious before E6-03.

## Migration Plan

No data migration. Deployment = the runbook itself. Rollback of this change: the previous compose is in git history; the NAS keeps whatever SHA it fetched.

## Open Questions

- UGOS version, Compose version and path of the Docker shared folder on Ghislain's NAS: read by the runbook's first section during the QA; they do not change the approach.
