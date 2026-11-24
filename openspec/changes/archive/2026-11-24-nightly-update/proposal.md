# Proposal

Backlog story: **E6-05 — Mise à jour automatique** (new story, created by this change; decided with Ghislain on 2026-11-12: E6 goes on with `nightly-backup`, then `nightly-update`).

Rules: RG-30, RG-92, RG-98, **RG-99 (new, agreed with Ghislain on 2026-11-24)** · ADRs: ADR-0015, **ADR-0019 (amended by a new ADR-0040)**, ADR-0027, ADR-0032, ADR-0038, ADR-0039.

## Why

Since `deploy-nas` and `nightly-backup`, every merge on `main` reaches the NAS only when Ghislain follows section 7 of the runbook by hand over SSH, which UGOS keeps cutting. The NAS lags behind `main`, and each update is a session of copied commands. `nightly-backup` gave the missing safety net (a backup command that fails loudly), so the update can now run alone at night.

## What Changes

- **Decisions taken with Ghislain (2026-11-14 to 2026-11-24)**:
  1. **Mechanism: a root cron line on the NAS host** (`/etc/cron.d/hippo-update`) that runs an update script kept in the project folder. UGOS has no task scheduler (checked by Ghislain in the UGOS UI on 2026-11-24); the host is Debian 12 with `cron`, `systemd`, `curl` and Docker Compose v5.1.3 (checked on the NAS on 2026-11-24). Nothing runs inside a container with the Docker socket (Watchtower and the like): `infra/check-compose.sh` forbids it.
  2. **Every night at 04:00 Paris time**, after the batch (02:00) and the backup (03:00). When nothing is new, nothing happens: no backup, no restart, one log line.
  3. **A failed update is logged and left as is**: no automatic rollback (an older image would start on a migrated schema without error, ADR-0032). The log line names the old and new versions and the backup taken just before, which is all section 8 of the runbook needs.
  4. **Notification: the log only.** UGOS has no scheduler to notify on failure; an external push service is out (new service, data leaving the NAS, RG-92). A state of the update visible in the app goes to the backlog.
  5. **The NAS follows everything `main` publishes for it**: the Hippo image, the production compose (Tailscale image included), `serve.json` and `.env.example`. An image and its compose only work together (`BACKUP_HOST_DIR` came with `nightly-backup`).
  6. **`main` is trusted, and the trust is written down** (2026-11-24, after the `architect` pass; stop case 6): the update script replaces itself with the version of the same commit, only after a successful update. No `check-compose.sh` on the NAS (it needs `jq`, and a compose from `main` can already mount any host folder: it does not stop a compromised `main`); it stays in CI on every PR. On the NAS, `docker compose config --quiet` only checks that `.env` fits the new compose.
- **Decisions taken after the `architect` pass (no new behaviour, tighter guards)**:
  - **The deployment files come from the commit the image was built from**: the script pulls `latest`, reads its `org.opencontainers.image.revision` label and fetches `infra/docker-compose.yml`, `infra/tailscale/config/serve.json`, `infra/.env.example` and the script itself at that commit from GitHub. A compose merged on `main` never reaches the NAS before its image. `image.yml` also publishes when one of those files changes (a Tailscale bump by Dependabot publishes a new image).
  - **`image.yml` publishes from `main` only** (`workflow_dispatch` on another branch could publish a `latest` the NAS would install without a PR nor `ci-ok`).
- **Business rule RG-99 (agreed with Ghislain on 2026-11-24, below).**
- **The update script**, run by cron as root, with one run at a time (a lock), in this order; every step that fails stops the run with a log line and a non-zero exit, and nothing after it happens:
  1. **Hand brake**: if `HIPPO_IMAGE_TAG` in `.env` is not `latest` (a pinned `sha-…`), the run only logs `paused` and exits 0.
  2. Pull `latest` and fetch the deployment files of its commit into a staging folder, without touching the running stack. Same image and same files as what runs: log `up-to-date`, exit 0.
  3. Refuse the new files when `.env` lacks a variable that the new `.env.example` sets (uncommented), or when the new compose fails `docker compose config --quiet` with the real `.env`: nothing is replaced.
  4. Pull the other new images (Tailscale) while the old version still runs.
  5. Back up with the running version: `docker compose exec hippo /hippo backup` (never `run`, RG-98); a non-zero exit stops the run.
  6. Put the new deployment files in place, then `docker compose up -d`.
  7. Wait for `hippo` to be `healthy` (bounded); log `updated` with the old and new versions and the backup name, or `failed` with the same facts and a non-zero exit.
- **Log**: one JSON line per run in a log file of the project folder (rotated, bounded), never a secret, never a value of `.env`, never an item's content.
- **Infra and docs**: the script and its tests in `infra/`, its test suite and shellcheck in `make check-infra` (so in CI); whatever `main` must publish for the NAS to fetch; the runbook (install the cron line once, enable by setting `HIPPO_IMAGE_TAG=latest`, read the log, pause with a pinned tag, check after each UGOS firmware update that the cron line is still there); ADR-0040 amending ADR-0019 (« mise à jour manuelle et volontaire »), with the accepted risk below; `docs/metier.md` (RG-99, RG-98 cross-reference); glossary; `docs/spec-v1.md` « Déploiement »; backlog E6-05 and the « état de la mise à jour dans l'appli » idea.

## Capabilities

### New Capabilities
- `automatic-update`: the nightly update of Hippo on the NAS from what `main` publishes — hand brake, nothing-new check, refusal of incompatible files, backup before, health check after, log line. Exception to the spec rules of `openspec/config.yaml`, stated here as for `backup`: nothing of it goes through the API; its observable behaviour is the Docker commands it runs, the files it puts in place, its exit code and its log line. Each scenario has its test in a shell test suite with `docker` and the network replaced by fakes on `PATH` (Docker is not SQLite: the no-mock rule does not apply), plus a real run on the NAS during the QA.

### Modified Capabilities
- none (`backup` is used as it is: its exit code and output contract are what this change relies on).

## Non-goals

- Automatic rollback, of the image or of the data (decision 3).
- Any notification beyond the log (decision 4); the state of the update in the app (backlog).
- Catching up a night missed while the NAS was off (like the batch and the backup, RG-30, RG-98).
- Deploying from GitHub to the NAS (a push needs secrets and tailnet access from GitHub); Watchtower or any container with the Docker socket.
- Updating UGOS, Docker or anything outside the Hippo project folder.
- Updating more often than nightly, or on a schedule other than 04:00.

## Decision taken with Ghislain (2026-11-24, stop cases 1 and 2: business rule)

**RG-99 — accepted as worded.** Wording added to `docs/metier.md`: « Chaque nuit à 4 h, le NAS installe la dernière version publiée de Hippo, après une sauvegarde (RG-98) ; s'il n'y a rien de nouveau, rien ne se passe. Si la mise à jour échoue, Hippo reste tel quel et l'échec est noté dans le journal du NAS. Fixer une version précise suspend la mise à jour automatique. »

## Accepted risks (to record in ADR-0040)

- **What reaches `main` runs on the NAS at night, as root, without Ghislain reading it** (decision 6). Guards: `main` moves only by a merged PR with `ci-ok` green, which runs `check-compose.sh` (ADR-0029); `image.yml` publishes from `main` only; the hand brake. They catch mistakes, not a compromise: a compromised GitHub account is root on the NAS by the next night. This risk existed before, but went through Ghislain at update time.
- **A UGOS firmware update may silently drop the cron line.** The runbook adds a check after each UGOS update (a log line from the night before); the QA checks the line survives a reboot.
- **A failure after `up -d` leaves Hippo down until Ghislain reads the log** or notices captures failing. A capture made in the app waits on the phone and is sent later (RG-06); one made from the share extension fails with a clear message and the link to copy (RG-06). Reading and search keep working offline on synced items (RG-97).

## Impact

- **AI cost**: none.
- **Private items**: none; nothing leaves the NAS but image pulls and fetches of public files (RG-92).
- **Persisted data**: none in the database (migrations run at start-up as today, ADR-0032). One extra backup on each night with an update (RG-98 retention applies). A log file in the project folder.
- **API contract**: none.
- **Prompt and fiche schemas**: none.
- **Dependencies**: none in the backend or the app. On the NAS host: `bash`, `cron`, `curl`, `docker compose` (checked on 2026-11-24) and `flock` (util-linux, Debian base; checked during the QA); no `jq` nor `python3` is assumed. Anything more is a stop case.
- **User QA: yes** — Ghislain installs the cron line, then checks on the NAS: a night with nothing new, a night with a real update, the hand brake, and the cron line still there after a reboot.
