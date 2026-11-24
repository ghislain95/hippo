# Design

## Context

- Hippo runs on the NAS (Ugreen DXP4800 Plus, UGOS Pro = Debian 12) as the production compose `infra/docker-compose.yml`: `tailscale` sidecar and `hippo`, in a project folder (`/volume1/docker/hippo` today) with `.env` (mode 600, never read by agents), `.env.example`, `tailscale/config/serve.json`, `data/`, `tailscale/state/`. Backups go to `Sauvegardes/hippo` (ADR-0039).
- Facts checked on the NAS on 2026-11-24: Debian 12, `cron` with `/etc/cron.d` in use (`e2scrub_all`, `sysstat`), empty root crontab, `systemd`, `curl`, Docker Compose v5.1.3; no UGOS task scheduler. Not checked yet (QA, task 7): `flock`, `jq` (not assumed), the host time zone, and that `docker inspect -f '{{.Image}}'` of a container equals `docker image inspect -f '{{.Id}}'` of its image on the NAS image store.
- `image.yml` publishes `latest` and `sha-<short>` on a push to `main` that touches `backend/**`, multi-arch with QEMU (several minutes); `docker/metadata-action` sets `org.opencontainers.image.revision` to the commit SHA (unverified in the GHCR manifest; checked during the QA with `docker image inspect`).
- `hippo backup` (capability `backup`) exits non-zero on every failure and prints one JSON line with `"outcome"` and `"backup":"<name>"`; it must run in the running container (`exec`), never `run`.
- Today the NAS runs `HIPPO_IMAGE_TAG=sha-3fb9955`: the automatic update starts paused, and is enabled by setting `latest` (runbook).

No Go package, app folder, API operation, migration, prompt nor fiche schema is touched. `user_id` filtering, the private-item guarantee (RG-12) and the AI budget (RG-81) are not on any path of this change: the script never reads the database nor calls the AI.

## Goals / Non-Goals

**Goals:** the nightly update of the proposal, decisions 1 to 6; a script that is boring, testable on the Mac and in CI, and safe to stop at any step before the point of no return.

**Non-Goals:** those of the proposal; pruning old images (each is ~25 MB and updates follow merges, not nights; the runbook gives `docker image prune` for Ghislain).

## Decisions

### D1. Files on the NAS

| Path (project folder) | Owner, mode | Role |
| --- | --- | --- |
| `hippo-update.sh` | root, `0700` | the update script, a copy of `infra/hippo-update.sh` |
| `update/` | root, `0700` | state of the script, created by it |
| `update/lock` | root, `0600` | `flock` lock |
| `update/update.log`, `update/update.log.1` | root, `0600` | update log and its rotated file |
| `update/staging/` | root, `0700` | fetched files of the current run, emptied at the start of each run |
| `update/previous/` | root, `0700` | the deployment files installed before the last update (for section 8 of the runbook) |
| `update/installing` | root, `0600` | marker: revision being installed (D7) |
| `/etc/cron.d/hippo-update` | root, `0644` | the cron line (D9) |

The script finds the project folder from its own path (`HIPPO_DIR`, overridable for the tests). Only `HIPPO_DIR`, `HIPPO_UPDATE_HEALTH_TIMEOUT` and `HIPPO_UPDATE_REPO_URL` are read from the environment, for the tests; cron sets none of them.

### D2. Origin of the files: the commit of the published image

`docker pull ghcr.io/ghislain95/hippo:latest`, then `docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}'`, checked against `^[0-9a-f]{40}$`, then `curl -fsS --max-time 60 --retry 2` of `https://raw.githubusercontent.com/ghislain95/hippo/<revision>/infra/<file>` for `docker-compose.yml`, `tailscale/config/serve.json`, `.env.example` and `hippo-update.sh`.

- The image and its files come from one commit by construction: no race with a merge at 04:00, and a broken `image.yml` leaves the NAS on an old but consistent pair.
- `image.yml` also runs when one of those four files changes (`paths`), so a Tailscale bump by Dependabot or a script fix publishes a new image (its `VERSION` changes: `hippo` restarts, accepted).
- `image.yml` publishes from `main` only: `if: github.ref == 'refs/heads/main'` on the job (a `workflow_dispatch` on another branch would publish a `latest` that the NAS installs).
- Alternatives: fetch the files from `raw/main` (race with the image build, and two files may come from two commits); ship the files inside the image (`docker cp` out of a created container: same consistency, but a second build context in the Dockerfile, `make image`, `ci.yml` and the local compose).

### D3. Order of a run and point of no return

1. `umask 077`; create `update/`; take `flock -n` on `update/lock`, else log `locked`, exit 1.
2. Marker left (D7): log `interrupted`, remove it.
3. Read `HIPPO_IMAGE_TAG` (D5). Missing: `failed`/`no_image_tag`. Not `latest`: `paused`, exit 0.
4. Running container: `docker compose ps -q hippo` then `docker inspect -f '{{.Image}}'`; none: `failed`/`hippo_not_running`. Old revision: its label (or `unknown`).
5. Note the current local `latest` (`docker image inspect -f '{{.Id}}' …:latest`, may be empty).
6. `docker pull -q …:latest`: fails → `pull_failed`. Read the new image id and revision (D2): bad → `bad_revision`.
7. Empty `update/staging/`, fetch the four files: fails → `fetch_failed`.
8. Same image id as the running container and the three deployment files equal (`cmp`) to the installed ones → `up-to-date`, exit 0. The script alone is not compared: it changes only with a new image (D2).
9. Variables (D5): missing → `missing_variables`. `docker compose --project-directory "$HIPPO_DIR" --env-file "$HIPPO_DIR/.env" -f update/staging/docker-compose.yml config --quiet`, output discarded: fails → `compose_invalid`. `--project-directory` keeps `./data` and `./tailscale/…` resolved in the project folder.
10. Pull the other services of the new compose (`config --services` minus `hippo`, so `latest` is not pulled twice and cannot move between step 6 and here): fails → `pull_failed`.
11. `docker compose exec -T hippo /hippo backup` on the installed compose (`-T`: no TTY under cron); its output is kept in memory, the backup name read with `sed` and checked against the backup format; exit non-zero → `backup_failed`.
12. **Point of no return.** Write the marker, copy the installed deployment files to `update/previous/`, then install each staged file by `cp` to a temporary name in the target folder and `mv` over the target (same file system, atomic rename).
13. `docker compose up -d`: fails → `failed`/`up_failed`.
14. Health (D8): `healthy` on the new image → `updated`; else `failed`/`unhealthy`.
15. After `updated` only: install the staged script (D6). Remove the marker, log, exit.

On every stop between steps 6 and 11, the local `latest` tag is put back (`docker tag <old id> …:latest`) or removed (`docker rmi …:latest`, the image stays if a container or another tag uses it) when there was none: a manual `docker compose up -d` after a stopped run never installs the new version without its backup. After the point of no return, nothing is undone (proposal decision 3).

The script never writes `.env`, never runs `docker compose down`, never `run`, never `--force-recreate`.

### D4. Nothing new

Image ids compared as strings (`sha256:…`); files by `cmp -s`. The semantics of `{{.Image}}` versus `{{.Id}}` on the NAS image store is unverified: if they differ there, the run would see an update every night (a harmless backup and `up -d` that recreates nothing). The QA checks the first night logs `up-to-date`.

### D5. Reading `.env` and its variables

- `HIPPO_IMAGE_TAG`: the last line matching `^[[:space:]]*HIPPO_IMAGE_TAG[[:space:]]*=`, value trimmed of spaces, `\r` and one pair of surrounding quotes, with `sed`. `.env` is never sourced nor evaluated, so a line like `X=$(cmd)` is inert.
- Variable names: `sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'` on both files (uncommented lines only). Every name of the new `.env.example` must be present in `.env`, whatever its value (an empty `TS_AUTHKEY=` counts). A new optional variable with a default therefore also blocks the update until Ghislain adds it: accepted, the log names it.

### D6. The script replaces itself

After `updated`, if the staged script differs: `cp` to `hippo-update.sh.new`, `chmod 0700`, `mv` over `hippo-update.sh`. The rename gives a new inode; the running bash keeps reading the old one. The body is functions and the last line is `main "$@"; exit $?`, so bash has parsed everything before the file is replaced. Never after another outcome: a script that cannot update Hippo is not trusted to replace itself.

### D7. Interrupted run

The marker holds the revision being installed. A NAS reboot, an out-of-memory kill or a `kill` between steps 12 and 15 leaves it; the next run logs `interrupted` with that revision and goes on as usual (its backup and `up -d` repair a half-installed state). Runs that fail night after night each take a backup: RG-98's retention (14 days, 3 most recent kept) bounds them.

### D8. Health

Poll every 5 s, up to `HIPPO_UPDATE_HEALTH_TIMEOUT` (default 180 s: the healthcheck interval is 30 s with retries, and migrations run at start-up): `docker compose ps -q hippo`, then `docker inspect -f '{{.State.Health.Status}} {{.Image}}'`; `healthy` with the new image id is success.

### D9. Cron line

```
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
0 4 * * * root /volume1/docker/hippo/hippo-update.sh >/dev/null 2>&1
```

Written by Ghislain with a ready command of the runbook (path of his project folder). Debian cron runs at the host's local time: the runbook and the QA check `timedatectl` shows `Europe/Paris` (`CRON_TZ` is not relied on, unverified on Debian's cron). Output goes nowhere: the script logs to its own file; a crash before the log exists is seen as a missing line the next morning.

### D10. Log

One JSON object per line, built by the script from fixed keys: `time` (`date +%Y-%m-%dT%H:%M:%S%z`), `msg` (`"update"`), `outcome` (`paused`, `locked`, `interrupted`, `up-to-date`, `updated`, `failed`), and only when they apply `reason` (fixed set of D3), `old_revision`, `new_revision` (40 hex or `unknown`), `backup` (backup format), `missing` (names matching `[A-Za-z_][A-Za-z0-9_]*`, comma-separated), `duration_s`. A value that does not match its form is replaced by `invalid`. Appended to `update/update.log` (`0600`) and printed on standard output. Rotation by the script at the start of a run: over 1 MiB, `mv update.log update.log.1` (no `logrotate` assumed). Nothing printed by `docker` or `curl` is ever written to the log (their output goes to `/dev/null` or stays in a variable that is parsed, never copied).

### D11. Tests

`infra/hippo-update.test.sh`, plain bash (no `bats`: a new dependency), in the style of `.claude/hooks/*.test.sh`: each test builds a temporary project folder and a `bin/` of fakes put first on `PATH`:

- `docker`: answers from files of a scenario folder (running image id, published image id and labels, exit codes of `pull`, `config`, `exec … backup`, `up`, health sequence) and appends each call to `calls.log`;
- `curl`: copies files from a fake repository folder keyed by revision, or fails on demand; the URL is checked to hold the revision;
- `flock`: succeeds, or fails when the scenario says the lock is held (the Mac has no `flock`; the real one is checked in the QA).

Each scenario of the delta is one test named after it. The script stays portable to the Mac's bash 3.2 and BSD tools (no associative arrays, `mapfile`, `sed -i`, `stat`, GNU-only flags). `make check-infra` gains `shellcheck -S warning infra/hippo-update.sh infra/hippo-update.test.sh` and `bash infra/hippo-update.test.sh`, so the CI `infra` job runs them. `HIPPO_UPDATE_HEALTH_TIMEOUT` keeps the unhealthy test short.

Only the QA can check: the real 04:00 trigger and time zone, `flock`, `docker compose exec -T`, image ids and the revision label on the NAS, GitHub raw and GHCR for real, a reboot keeping the cron line.

### D12. Docs aligned in this change

ADR-0040 (amends ADR-0019; records decision 6 and that the guards catch mistakes, not a compromise); ADR-0019 status; `docs/metier.md` (RG-99, RG-98 cross-reference); glossary (« Mise à jour automatique », « Frein à main », « Journal de mise à jour »); `docs/spec-v1.md` « Déploiement »; runbook sections 2 (`HIPPO_IMAGE_TAG`), 7 (manual update stays possible, pause first), 8 (rollback after an automatic update: `update/previous/` and the backup named in the log), 9 or a new section (install, enable, read the log, pause, check after a UGOS update), 10; comments of `infra/.env.example` (`HIPPO_IMAGE_TAG`), `infra/docker-compose.yml` header, `image.yml` header; `infra/CLAUDE.md`; backlog E6-05 and the idea « état de la mise à jour dans l'appli ».

### D13. QA in two stages, around the merge

The real path needs a published image whose commit holds the script, and `image.yml` publishes from `main` only (D2). So the QA runs in two stages: before the merge, the facts, the hand brake and the early path on the real NAS (a run against `3fb9955` must stop at `fetch_failed` with nothing touched); after the merge, the first real nightly update, a night with nothing new and a reboot. Merging before the second stage is Ghislain's call at QA time (merge condition « User QA »); a defect found after the merge is fixed by a follow-up PR.

## Risks / Trade-offs

- **`main` is root on the NAS** (proposal decision 6, ADR-0040). Guards: protected `main`, `ci-ok` (with `check-compose.sh`), `image.yml` on `main` only, hand brake.
- **Cron line dropped by a UGOS firmware update**: silent; the runbook checks the log after each UGOS update; the QA checks a reboot.
- **Image id semantics on the NAS** (D4): worst case a useless nightly backup and `up -d`; caught by the QA.
- **GitHub raw down at 04:00**: `fetch_failed`, nothing changes, next night retries.
- **An analysis pass still running at 04:00** is cut by the restart; it is picked up at start-up, and nothing missed is caught up (RG-30). Not expected with V1 volumes.
- **First switch from `sha-3fb9955` to `latest`**: Compose may recreate the container although the image is the same (unverified); harmless, and done by Ghislain at enable time, not by the script.

## QA results

To fill during the QA.
