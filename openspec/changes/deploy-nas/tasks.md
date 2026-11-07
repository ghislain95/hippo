# Tasks

Before starting: `graphify check-update .`, read ADR-0013, 0015, 0019, 0020, 0021, 0027, 0030, 0032, 0038, `infra/CLAUDE.md`, `docs/runbook-ios.md`. No backend, contract, migration, prompt or schema change (`skip_specs: true`): the « tests » of this change are the assertions of `infra/check-compose.sh`, each seen failing once, and the runbook's checks during the QA. Never run `docker compose config` without `--quiet` outside `make check-infra`; never read `infra/.env`.

## 1. Static check first

- [ ] 1.1 Write `infra/check-compose.sh` (design D6): render each compose file with `--env-file /dev/null config --no-interpolate --format json`, then assert every invariant of D6 with one message per failure and a non-zero exit; shellcheck it at `-S warning`; wire it into `make check-infra` after the two `config --quiet` runs. Verify `make check-infra` fails on the current production compose for the invariants it does not meet yet (log rotation, pinned Tailscale image, required `HIPPO_IMAGE_TAG`), and only those.
- [ ] 1.2 Prove each assertion bites: on a scratch copy of the compose (outside the repository), break one invariant at a time (published port, `HTTP_ADDR=:8080`, extra capability, `TS_USERSPACE=true`, missing `logging`, an `.env.example` variable missing from `hippo`, a `hippo` variable missing from `.env.example`) and run the script on it through a `COMPOSE_DIR` override; record the result in this task's note. Verify each break gives its own message.

## 2. Compose and configuration

- [ ] 2.1 `infra/docker-compose.yml` (design D1 to D5): Tailscale image `v1.104.4`, `TS_AUTH_ONCE=true`, `TS_AUTHKEY=${TS_AUTHKEY:-}`, `TS_USERSPACE=false` kept with `NET_ADMIN` and `/dev/net/tun`, `depends_on` with `restart: true`, `HIPPO_IMAGE_TAG` required, log rotation on both services; header comments updated. Verify `make check-infra` green.
- [ ] 2.2 `infra/docker-compose.local.yml`: log rotation (same anchor values); no other change. Verify `make check-infra` green.
- [ ] 2.3 `infra/.env.example`: NAS section rewritten (one-off pre-approved untagged auth key, erased after enrolment; the NAS gets its own `API_TOKEN`; `HIPPO_IMAGE_TAG=sha-<short sha>` on the NAS, `latest` acceptable on the Mac). Verify gitleaks (`make check-secrets`) and `make check-infra`.
- [ ] 2.4 `.github/dependabot.yml`: `docker-compose` ecosystem on `/infra`, weekly Monday, prefix `build`. Verify the YAML parses (`yq`-free: `ruby -ryaml` or `python3 -c`), and note in design D4 that the effect on the GHCR reference stays unverified until Dependabot's first run.

## 3. Runbook and docs

- [ ] 3.1 Write `docs/runbook-nas.md` (French) following design D7, sections 0 to 10, placeholders only, every command in its own fenced block. Verify `make check-docs`, and `git grep` finds no tailnet name, IP, token or key in it.
- [ ] 3.2 Docs (doc-keeper): `infra/CLAUDE.md` (invariants and the check, log rotation, `TS_AUTH_ONCE`, pinned images); `docs/spec-v1.md` « Déploiement » (GHCR public, compose sketch aligned); `docs/CLAUDE.md` and `README.md` link the runbook; `docs/dev/setup-report.md` « À faire » items 6 and 8. Verify `make check-docs`.

## 4. Checks and review

- [ ] 4.1 `make check` green; `openspec validate deploy-nas`.
- [ ] 4.2 `code-reviewer` on the uncommitted diff; fix every 🛑 and 🔴, then review again.
- [ ] 4.3 `/opsx:verify deploy-nas`.

## 5. Pull request and QA

- [ ] 5.1 Commit `feat(deploy-nas)`, push, open the PR (French description, « QA Ghislain » section), enable Auto-fix; `ci-ok` green.
- [ ] 5.2 QA by Ghislain on the NAS, phase 1: sections 0 to 4 and 6 of the runbook, including a project restart, a NAS reboot and a restart after the auth key is erased. Every deviation fixes the runbook (or the compose) in the same PR, with a new review.
- [ ] 5.3 QA phase 2: API key added; paid end-to-end test (one fictitious public link, one fictitious image) left for the night; the next morning, logs, analysed items and image copy time on amd64 recorded in design.md (no personal data), then both items deleted.
- [ ] 5.4 Post-archive: backlog E6-01, E6-02, E6-04 done; `/opsx:archive`; `graphify update .`; commit `chore(deploy-nas): archive change, post-archive`; `/dod-check`; merge (`gh pr merge --merge --delete-branch`).
