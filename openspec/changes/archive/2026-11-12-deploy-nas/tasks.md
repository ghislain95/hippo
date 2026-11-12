# Tasks

Before starting: `graphify check-update .`, read ADR-0013, 0015, 0019, 0020, 0021, 0027, 0030, 0032, 0038, `infra/CLAUDE.md`, `docs/runbook-ios.md`. No backend, contract, migration, prompt or schema change (`skip_specs: true`): the « tests » of this change are the assertions of `infra/check-compose.sh`, each seen failing once, and the runbook's checks during the QA. Never run `docker compose config` without `--quiet` outside `make check-infra`; never read `infra/.env`.

## 1. Static check first

- [x] 1.1 Write `infra/check-compose.sh` (design D6): render each compose file with `--env-file /dev/null config --no-interpolate --format json`, then assert every invariant of D6 with one message per failure and a non-zero exit; shellcheck it at `-S warning`; wire it into `make check-infra` after the two `config --quiet` runs. Verify `make check-infra` fails on the current production compose for the invariants it does not meet yet (log rotation, pinned Tailscale image, required `HIPPO_IMAGE_TAG`), and only those.
- [x] 1.2 Prove each assertion bites: on a scratch copy of the compose (outside the repository), break one invariant at a time (published port, `HTTP_ADDR=:8080`, extra capability, `TS_USERSPACE=true`, missing `logging`, an `.env.example` variable missing from `hippo`, a `hippo` variable missing from `.env.example`) and run the script on it through a `COMPOSE_DIR` override; record the result in this task's note. Verify each break gives its own message.
  - Result (2026-11-07, 18 breaks, one scratch copy each, `COMPOSE_DIR` override, also through `make check-infra COMPOSE_DIR=…`): the untouched copy passes; every break exits 1 with exactly one message of its own (the `.env.example`-only variable gives one per compose file, as expected): published port → `service tailscale: publishes a port…`; `HTTP_ADDR=:8080` → `hippo: HTTP_ADDR must be 127.0.0.1:8080`; extra `SYS_ADMIN` → `tailscale: cap_add must be exactly [NET_ADMIN]`; `TS_USERSPACE=true` → `tailscale: TS_USERSPACE must be false…`; missing `logging` (prod and local) → `service <s>: no bounded log rotation…`; `.env.example` variable missing from `hippo` → `hippo: LOG_LEVEL of .env.example does not reach the container…`; `hippo` variable missing from `.env.example` → `hippo: EXTRA_SETTING is not documented in .env.example…`; hard-coded value → `hippo: LOG_LEVEL is set without reading ${LOG_LEVEL}…`; extra device → `tailscale: devices must be exactly /dev/net/tun`; `privileged` → `…privileged is forbidden`; host network → `…the host network is forbidden`; Tailscale `latest` → `tailscale: image must be pinned…`; `${HIPPO_IMAGE_TAG:-latest}` → `hippo: image must be …${HIPPO_IMAGE_TAG:?...}…`; no `network_mode` → `hippo: network_mode must be service:tailscale`; no `mem_limit` → `hippo: no mem_limit (ADR-0038)`; local `8080:8080` → `service hippo: publishes a port outside 127.0.0.1`. An unparsable file and a missing file fail too. Before 2.1 and 2.2, `make check-infra` failed on the current files with exactly five messages: log rotation (`hippo`, `tailscale`, local `hippo`), Tailscale image not pinned, `HIPPO_IMAGE_TAG` not required.

## 2. Compose and configuration

- [x] 2.1 `infra/docker-compose.yml` (design D1 to D5): Tailscale image `v1.104.4`, `TS_AUTH_ONCE=true`, `TS_AUTHKEY=${TS_AUTHKEY:-}`, `TS_USERSPACE=false` kept with `NET_ADMIN` and `/dev/net/tun`, `depends_on` with `restart: true`, `HIPPO_IMAGE_TAG` required, log rotation on both services; header comments updated. Verify `make check-infra` green.
- [x] 2.2 `infra/docker-compose.local.yml`: log rotation (same anchor values); no other change. Verify `make check-infra` green.
- [x] 2.3 `infra/.env.example`: NAS section rewritten (one-off pre-approved untagged auth key, erased after enrolment; the NAS gets its own `API_TOKEN`; `HIPPO_IMAGE_TAG=sha-<short sha>` on the NAS, `latest` acceptable on the Mac). Verify gitleaks (`make check-secrets`) and `make check-infra`.
- [x] 2.4 `.github/dependabot.yml`: `docker-compose` ecosystem on `/infra`, weekly Monday, prefix `build`. Verify the YAML parses (`yq`-free: `ruby -ryaml` or `python3 -c`), and note in design D4 that the effect on the GHCR reference stays unverified until Dependabot's first run.

## 3. Runbook and docs

- [x] 3.1 Write `docs/runbook-nas.md` (French) following design D7, sections 0 to 10, placeholders only, every command in its own fenced block. Verify `make check-docs`, and `git grep` finds no tailnet name, IP, token or key in it.
- [x] 3.2 Docs (doc-keeper): `infra/CLAUDE.md` (invariants and the check, log rotation, `TS_AUTH_ONCE`, pinned images); `docs/spec-v1.md` « Déploiement » (GHCR public, compose sketch aligned); `docs/CLAUDE.md` and `README.md` link the runbook; `docs/dev/setup-report.md` « À faire » items 6 and 8. Verify `make check-docs`.

## 4. Checks and review

- [x] 4.1 `make check` green; `openspec validate deploy-nas`.
- [x] 4.2 `code-reviewer` on the uncommitted diff; fix every 🛑 and 🔴, then review again.
- [x] 4.3 `/opsx:verify deploy-nas`.

## 5. Pull request and QA

- [x] 5.1 Commit `feat(deploy-nas)`, push, open the PR (French description, « QA Ghislain » section), enable Auto-fix; `ci-ok` green.
- [x] 5.2 QA by Ghislain on the NAS, phase 1: sections 0 to 4 and 6 of the runbook, including a project restart, a NAS reboot and a restart after the auth key is erased. Every deviation fixes the runbook (or the compose) in the same PR, with a new review.
- [x] 5.3 QA phase 2: API key added; paid end-to-end test (one fictitious public link, one fictitious image) left for the night; the next morning, items read back by replaying their captures with the same `client_id`, logs and the duration of the pass that copied the image recorded in design.md (no personal data); then Hippo restarts from an empty `data/` (no delete route before E4).
- [x] 5.4 Post-archive: backlog E6-01, E6-02, E6-04 done; `/opsx:archive`; `graphify update .`; commit `chore(deploy-nas): archive change, post-archive`; `/dod-check`; merge (`gh pr merge --merge --delete-branch`).
