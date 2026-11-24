# Design

## Context

- Hippo runs on the NAS (Ugreen DXP4800 Plus, UGOS Pro = Debian 12) as the production compose `infra/docker-compose.yml`: `tailscale` sidecar and `hippo`, in a project folder (`/<volume>/docker/hippo`) with `.env` (mode 600, never read by agents), `.env.example`, `tailscale/config/serve.json`, `data/`, `tailscale/state/`. Backups go to `Sauvegardes/hippo` (ADR-0039).
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

| Path (project folder, unless absolute) | Owner, mode | Role |
| --- | --- | --- |
| `/usr/local/sbin/hippo-update` | root, `0700` | the update script, a copy of `infra/hippo-update.sh`, outside the project folder (D15) |
| the project folder, `tailscale/`, `tailscale/config/` | root, no write for group nor others, no ACL | checked at each run (D15) |
| `docker-compose.yml`, `.env`, `.env.example`, `tailscale/config/serve.json` | root, no write for group nor others, no ACL (`.env` stays `0600`) | checked at each run (D15) |
| `/var/lib/hippo-update/` | root, `0700` | state folder of the script, outside the share, created by it (D16) |
| `/var/lib/hippo-update/lock` | root, `0600` | `flock` lock |
| `/var/lib/hippo-update/update.log`, `update.log.1` | root, `0600` | update log and its rotated file |
| `/var/lib/hippo-update/run/` | root, `0700`, files `0600` | copies of the project folder's `docker-compose.yml` and `.env` given to Compose during a run, emptied at its start and removed at its end, whatever the outcome (D17) |
| `/var/lib/hippo-update/staging/` | root, `0700` | fetched files of the current run, emptied at the start of each run |
| `/var/lib/hippo-update/previous/` | root, `0700` | the deployment files installed before the last update (for section 8 of the runbook); not the script (D15) |
| `/var/lib/hippo-update/installing` | root, `0600` | marker: revision being installed (D7) |
| `/etc/cron.d/hippo-update` | root, `0644` | the cron line (D9) |

The script takes the project folder as its only argument, an absolute path (D15). Nothing is written to the project folder but the deployment files installed by an update (D16); Compose is never given a file of the project folder (D17). Only `HIPPO_UPDATE_OWNER_UID` (D15), `HIPPO_UPDATE_STATE_DIR` (D16), `HIPPO_UPDATE_HEALTH_TIMEOUT`, `HIPPO_UPDATE_HEALTH_INTERVAL` (D8; added during the implementation, so that the health tests poll without waiting 5 s) and `HIPPO_UPDATE_REPO_URL` are read from the environment, for the tests; cron sets none of them.

### D2. Origin of the files: the commit of the published image

`docker pull ghcr.io/ghislain95/hippo:latest`, then `docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}'`, checked against `^[0-9a-f]{40}$`, then `curl -fsS --max-time 60 --retry 2` of `https://raw.githubusercontent.com/ghislain95/hippo/<revision>/infra/<file>` for `docker-compose.yml`, `tailscale/config/serve.json`, `.env.example` and `hippo-update.sh`.

- The image and its files come from one commit by construction: no race with a merge at 04:00, and a broken `image.yml` leaves the NAS on an old but consistent pair.
- `image.yml` also runs when one of those four files changes (`paths`), so a Tailscale bump by Dependabot or a script fix publishes a new image (its `VERSION` changes: `hippo` restarts, accepted).
- `image.yml` publishes from `main` only: `if: github.ref == 'refs/heads/main'` on the job (a `workflow_dispatch` on another branch would publish a `latest` that the NAS installs).
- Alternatives: fetch the files from `raw/main` (race with the image build, and two files may come from two commits); ship the files inside the image (`docker cp` out of a created container: same consistency, but a second build context in the Dockerfile, `make image`, `ci.yml` and the local compose).

### D3. Order of a run and point of no return

1. `umask 077`; read the project folder from the argument, resolve the script's own file, `cd -P` into the project folder (then relative paths only, D16) and check the ownership of the paths (D15): refused → message on standard error, exit 1, no log; `flock` missing → the same (D14); create the state folder; take `flock -n` on its `lock`, else log `locked`, exit 1; copy `./docker-compose.yml` and `./.env` into `<state>/run/` (D17; fails → message on standard error, exit 1, no log). `HIPPO_IMAGE_TAG` and the variable names are then read from the copy of `.env`.
2. Marker left (D7): log `interrupted`, remove it.
3. Read `HIPPO_IMAGE_TAG` (D5). Missing (no line, or an empty value, which Compose refuses too; a missing `.env` is refused at step 1, D15): `failed`/`no_image_tag`. Not `latest`: `paused`, exit 0.
4. Running container: `docker compose ps -q hippo` then `docker inspect -f '{{.Image}}'`; none, or an image id not of the form `sha256:<64 hex>`: `failed`/`hippo_not_running`. Old revision: its label (or `unknown`).
5. Note the current local `latest` (`docker image inspect -f '{{.Id}}' …:latest`, may be empty).
6. `docker pull -q …:latest`: fails, or no image id behind the tag afterwards → `pull_failed`. Read the new image id and revision (D2): bad → `bad_revision` (logged with `new_revision` `unknown`).
7. Empty `<state>/staging/`, fetch the four files: fails, or a fetched file is empty → `fetch_failed`.
8. Same image id as the running container and the three deployment files equal (`cmp`) to the installed ones → `up-to-date`, exit 0. The script alone is not compared: it changes only with a new image (D2).
9. Variables (D5): missing → `missing_variables`. `docker compose --project-directory . --env-file <state>/run/.env -f <state>/staging/docker-compose.yml config --quiet`, run in the project folder, output discarded: fails → `compose_invalid`. `--project-directory` keeps `./data` and `./tailscale/…` resolved in the project folder. Until the point of no return, the installed compose is given to Compose as its copy, `-f <state>/run/docker-compose.yml` (D17).
10. Pull the other services of the new compose (`config --services` minus `hippo`, so `latest` is not pulled twice and cannot move between step 6 and here): fails → `pull_failed`; the services cannot be listed, or a name is malformed → `compose_invalid`.
11. `docker compose exec -T hippo /hippo backup` on the installed compose (`-T`: no TTY under cron); its standard output is kept in memory, the backup name read with `sed` and checked against the backup format; exit non-zero, or exit zero without a valid backup name (the `backup` output contract broken) → `backup_failed`.
12. **Point of no return.** Write the marker, copy the installed deployment files to `<state>/previous/`, then install each staged file (to `./<file>`, D16) by `cp` to a temporary name in the target folder and `mv` over the target (same file system, atomic rename). An installed file keeps its owner and mode (the temporary copy is made with `cp -p` from it, then given the new content); a file new to the folder gets `0644` (public content). Any of these writes fails → `failed`/`install_failed` (added during the implementation: no reason of the list fitted), nothing undone.
13. `docker compose up -d` with `-f <state>/staging/docker-compose.yml` (the bytes just installed, never the file of the share; also for the health polls, D17): fails → `failed`/`up_failed`.
14. Health (D8): `healthy` on the new image → `updated`; else `failed`/`unhealthy`.
15. After `updated` only: install the staged script (D6). Remove the marker, log, exit.

On every stop between steps 6 and 11, the local `latest` tag is put back (`docker tag <old id> …:latest`) or removed (`docker rmi …:latest`, the image stays if a container or another tag uses it) when there was none: a manual `docker compose up -d` after a stopped run never installs the new version without its backup. After the point of no return, nothing is undone (proposal decision 3).

The script never writes `.env`, never runs `docker compose down`, never `run`, never `--force-recreate`.

### D4. Nothing new

Image ids compared as strings (`sha256:…`); files by `cmp -s`. The semantics of `{{.Image}}` (container) versus `{{.Id}}` (image) on the NAS image store is unverified. If they differ there (a containerd image store reporting another digest), the run is **not** harmless: it would see an update every night, back up, run `up -d`, and `wait_healthy` (which compares the container's `{{.Image}}` to the new `{{.Id}}`) would never succeed, so every night would end `failed`/`unhealthy`. QA 7.1 is therefore a go/no-go: `docker inspect -f '{{.Image}}' hippo-hippo-1` must equal `docker image inspect -f '{{.Id}}' ghcr.io/ghislain95/hippo:<running tag>` before the cron line is installed. (Corrected after the `code-reviewer` pass, 2026-11-24.)

### D5. Reading `.env` and its variables

- `HIPPO_IMAGE_TAG`: the last line matching `^[[:space:]]*HIPPO_IMAGE_TAG[[:space:]]*=`, value trimmed of spaces, `\r` and one pair of surrounding quotes, with `sed`. `.env` is never sourced nor evaluated, so a line like `X=$(cmd)` is inert.
- Variable names: `sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'` on both files (uncommented lines only). Every name of the new `.env.example` must be present in `.env`, whatever its value (an empty `TS_AUTHKEY=` counts). A new optional variable with a default therefore also blocks the update until Ghislain adds it: accepted, the log names it.

### D6. The script replaces itself

After `updated`, if the staged script differs from the running script's own file (absolute path of `$0`, its folder resolved by `cd` and `pwd -P`, no `readlink -f`; `/usr/local/sbin/hippo-update` on the NAS, D15): `cp` to `<that file>.new`, `chmod 0700`, `mv` over it, in its own folder. The rename gives a new inode; the running bash keeps reading the old one. The body is functions and the last line is `main "$@"; exit $?`, so bash has parsed everything before the file is replaced. Never after another outcome: a script that cannot update Hippo is not trusted to replace itself.

### D7. Interrupted run

The marker holds the revision being installed. A NAS reboot, an out-of-memory kill or a `kill` between steps 12 and 15 leaves it; the next run logs `interrupted` with that revision and goes on as usual (its backup and `up -d` repair a half-installed state). Runs that fail night after night each take a backup: RG-98's retention (14 days, 3 most recent kept) bounds them.

### D8. Health

Poll every 5 s (`HIPPO_UPDATE_HEALTH_INTERVAL`, for the tests), up to `HIPPO_UPDATE_HEALTH_TIMEOUT` (default 180 s: the healthcheck interval is 30 s with retries, and migrations run at start-up; the last pause is cut to the time left): `docker compose ps -q hippo`, then `docker inspect -f '{{.State.Health.Status}} {{.Image}}'`; `healthy` with the new image id is success.

### D9. Cron line

```
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
0 4 * * * root /usr/local/sbin/hippo-update /<volume>/docker/hippo >/dev/null 2>&1
```

Written by Ghislain with a ready command of the runbook (path of his project folder, the only argument, D15). Debian cron runs at the host's local time: the runbook and the QA check `timedatectl` shows `Europe/Paris` (`CRON_TZ` is not relied on, unverified on Debian's cron). Output goes nowhere: the script logs to its own file; a crash before the log exists is seen as a missing line the next morning.

### D10. Log

One JSON object per line, built by the script from fixed keys: `time` (`date +%Y-%m-%dT%H:%M:%S%z`), `msg` (`"update"`), `outcome` (`paused`, `locked`, `interrupted`, `up-to-date`, `updated`, `failed`), and only when they apply `reason` (fixed set of D3: `no_image_tag`, `hippo_not_running`, `pull_failed`, `bad_revision`, `fetch_failed`, `missing_variables`, `compose_invalid`, `backup_failed`, `install_failed`, `up_failed`, `unhealthy`), `old_revision`, `new_revision` (40 hex or `unknown`), `backup` (backup format), `missing` (names matching `[A-Za-z_][A-Za-z0-9_]*`, comma-separated), `duration_s` (a number; on every outcome but `locked` and `interrupted`). `up-to-date` carries both revisions. A value that does not match its form is replaced by `invalid`. Appended to `/var/lib/hippo-update/update.log` (`0600`) and printed on standard output. Rotation by the script at the start of a run: over 1 MiB, `mv update.log update.log.1` (no `logrotate` assumed). Nothing printed by `docker` or `curl` is ever written to the log (their output goes to `/dev/null` or stays in a variable that is parsed, never copied).

### D11. Tests

`infra/hippo-update.test.sh`, plain bash (no `bats`: a new dependency), in the style of `.claude/hooks/*.test.sh`: each test builds a temporary project folder and a `bin/` of fakes put first on `PATH`:

- `docker`: answers from files of a scenario folder (running image id, published image id and labels, exit codes of `pull`, `config`, `exec … backup`, `up`, health sequence) and appends each call to `calls.log`;
- `curl`: copies files from a fake repository folder keyed by revision, or fails on demand; the URL is checked to hold the revision;
- `flock`: succeeds, or fails when the scenario says the lock is held (the Mac has no `flock`; the real one is checked in the QA);
- `ls`: the real one (`/bin/ls`), except that `ls -ldn <path>` reports another uid for the path the scenario names: a file of another owner without being root (D15).

The script runs from a copy outside the project folder (`<test folder>/sbin/hippo-update`) with the project folder as argument, `HIPPO_UPDATE_STATE_DIR` set to a temporary state folder (D16), and `HIPPO_UPDATE_OWNER_UID` set to the uid of the user running the tests, who stands for root; the suite sets `umask 022`. The fake `docker` runs scenario hooks on `pull` and `exec` (a project folder swapped during the run, a write during the backup). The fake `docker compose` behaves as Compose does (strace, Compose v5.7.1, third review): it takes its working directory from `$PWD` (Go `os.Getwd`; bash, like Go, keeps an inherited `$PWD` only while it names the working directory), runs a race hook (two renames) and only then opens `-f` and `--env-file`, a relative one joined to that directory; it records every file it reads, and the compose and `.env` read by `up -d`.

Each scenario of the delta is one test named after it. The script stays portable to the Mac's bash 3.2 and BSD tools (no associative arrays, `mapfile`, `sed -i`, `stat`, GNU-only flags). `make check-infra` gains `shellcheck -S warning infra/hippo-update.sh infra/hippo-update.test.sh` and `bash infra/hippo-update.test.sh`, so the CI `infra` job runs them. `HIPPO_UPDATE_HEALTH_TIMEOUT` and `HIPPO_UPDATE_HEALTH_INTERVAL` keep the health tests short (the whole suite runs in a few seconds).

Only the QA can check: the real 04:00 trigger and time zone, `flock`, `docker compose exec -T`, image ids and the revision label on the NAS, GitHub raw and GHCR for real, a reboot keeping the cron line.

### D12. Docs aligned in this change

ADR-0040 (amends ADR-0019; records decision 6 and that the guards catch mistakes, not a compromise); ADR-0019 status; `docs/metier.md` (RG-99, RG-98 cross-reference); glossary (« Mise à jour automatique », « Frein à main », « Journal de mise à jour »); `docs/spec-v1.md` « Déploiement »; runbook sections 2 (`HIPPO_IMAGE_TAG`), 7 (manual update stays possible, pause first), 8 (rollback after an automatic update: `/var/lib/hippo-update/previous/` and the backup named in the log), 9 or a new section (install, enable, read the log, pause, check after a UGOS update), 10; comments of `infra/.env.example` (`HIPPO_IMAGE_TAG`), `infra/docker-compose.yml` header, `image.yml` header; `infra/CLAUDE.md`; backlog E6-05 and the idea « état de la mise à jour dans l'appli ».

### D13. QA in two stages, around the merge

The real path needs a published image whose commit holds the script, and `image.yml` publishes from `main` only (D2). So the QA runs in two stages: before the merge, the facts, the hand brake and the early path on the real NAS (a run against `3fb9955` must stop at `fetch_failed` with nothing touched); after the merge, the first real nightly update, a night with nothing new and a reboot. Merging before the second stage is Ghislain's call at QA time (merge condition « User QA »); a defect found after the merge is fixed by a follow-up PR.

### D14. Hardening after the `code-reviewer` pass (2026-11-24)

- The script refuses to run, with a message on standard error and exit 1, when its state folder is a symbolic link (root would otherwise create and write through it; now part of the check of D15, the state folder having moved by D16) or when `flock` is missing (it would otherwise log `locked` every night, a false lead). Under cron, standard error goes nowhere: both show as a missing log line in the morning, and the runbook says to run the script by hand to read the message.
- `updated` is logged first, then the script replaces itself (the spec's order). A failed replacement is not an error of the update: the old script stays and the next update retries.
- `restore_latest` is best effort on purpose (comment in the script): it only runs on a path that already logs `failed`.
- `HIPPO_IMAGE_TAG=latest # comment` reads `latest`, as Compose reads it.

### D15. The script outside the project folder, and the project folder root's alone (2026-11-24, Ghislain, stop case: security)

Risk reduced, not closed (what remains: below and D16): the project folder sits in a UGOS share that the admin account (and whoever holds its password or a share session) can write; root ran at 04:00 a script, a compose and a `.env` taken from there, so a write to the share was root on the NAS the next night.

- **Location**: the script is installed as `/usr/local/sbin/hippo-update` (root, `0700`), outside every share; the file of the repository stays `infra/hippo-update.sh`, fetched from the commit of the image as before (D2). It replaces its own file (D6). `previous/` no longer holds a script.
- **Argument**: the project folder is the only argument, mandatory and absolute (trailing slashes removed); missing, relative or not a folder → message on standard error, exit 1, no log. Cron line: `0 4 * * * root /usr/local/sbin/hippo-update /<volume>/docker/hippo >/dev/null 2>&1` (D9).
- **Check, before any write and any `docker`** (in the project folder after `cd -P`, before the state folder is created and before the lock): the project folder, `docker-compose.yml` and `.env` must exist; the project folder (`.`, and its argument must not be a symbolic link), `tailscale/`, `tailscale/config/`, `docker-compose.yml`, `.env`, `.env.example`, `tailscale/config/serve.json`, the state folder (D16) and the script's own file, those that exist, must each not be a symbolic link (`[ -L ]`), belong to uid 0 (`HIPPO_UPDATE_OWNER_UID`, tests only), have no write bit for group (6th character of the mode) nor others (9th), and no ACL (`+` as 11th character; the `@` of macOS and the `.` of SELinux are accepted). Read with `ls -ldn` (BSD and GNU, no `stat`). Refused → one message on standard error naming the path and the cause, exit 1, no log line (the log folder is not trusted yet), no `docker`, nothing created. The script's own file is checked too (added during the implementation: a script left group-writable would reopen the same hole).
- **Not covered**:
  - not atomic (TOCTOU): a file can change between the check and its use only through a folder the check found root's alone; the remaining case, the project folder renamed by its writable parent (`/<volume>/docker`) and another put at its path during the run, was shown by the second review to make root install an attacker's compose and script; D16 and D17 close that, except the mounts resolved by Compose and the daemon (D16, D17);
  - the parent folders of the project folder (`/<volume>`, `/<volume>/docker`) and the folder of the script (`/usr/local/sbin`, root `0755` on Debian 12, checked in a `debian:12` container) are not checked;
  - the ACL refusal is not tested by the suite (no portable way to set an ACL on the Mac and in CI without a new dependency); the QA checks `ls -ldn` and `getfacl` on the NAS;
  - a UGOS firmware update may erase `/usr/local/sbin/hippo-update`, like the cron line: the runbook's check after each UGOS update covers both (a missing log line).

### D16. The state outside the share, and the project folder reached through its inode (2026-11-24, second review, 🛑)

The second `code-reviewer` pass showed the check of D15 done once, then the project folder read again by its absolute path: whoever can write `/<volume>/docker` (the UGOS admin account) renames the project folder after the check and puts its own at the same path; `up -d` then reads its compose, and its `update/staging/hippo-update.sh`, written during the backup, becomes `/usr/local/sbin/hippo-update` (root for good), with a log line `updated`. Fixed (option b, decided with the coordinator; option a, checking every ancestor and so moving the project out of the UGOS share, is left aside and goes to Ghislain):

- **State outside the share**: `/var/lib/hippo-update` (root, `0700`, created by the script; `HIPPO_UPDATE_STATE_DIR`, absolute, tests only) holds `lock`, `update.log` and `update.log.1`, `staging/`, `previous/` and the marker `installing`. Nothing is written to the project folder but the deployment files installed by an update. The state folder is checked when it exists (D15); its parent `/var/lib` is not.
- **`cd -P` then relative paths**: right after the argument is validated (and the script's own path resolved, `$0` may be relative), the script enters the project folder with `cd -P --`, then checks `.` and relative paths, and only uses relative paths to the project: `cmp`, `cp -p`, the copies of D17 and the install to `./<file>`; Compose gets `--project-directory .` and files of the state folder only (corrected by D17: a relative `-f ./docker-compose.yml` is opened by Compose through an absolute path). The working directory holds the inode of the checked folder: a rename of the folder by its parent does not move it. `HIPPO_DIR` only serves the messages after the `cd`.
- **Test**: « A project folder swapped during the run is not followed » (the fake `docker pull` renames the folder and puts an attacker's one at its path, with a compose, a `.env` and a staging script; the fake `exec` rewrites the script during the backup): the backup and `up -d` read the checked folder's compose, the new files go to the checked folder, nothing is written in the other, the script installed is the repository's. The reviewer's `race.sh` and `race2.sh`, replayed on the new suite, fail to attack.
- **Remaining risk** (corrected by the third review, see D17): Compose turns `--project-directory .` into an absolute path (Go `os.Getwd`) and opens every file it is given by absolute path; with D17 it is only given files of the state folder. The relative mounts of the compose, `./data`, `./tailscale/config` and `./tailscale/state`, still become absolute paths under the project folder's path, resolved by the Docker daemon when the containers are created. Whoever can write `/<volume>/docker` and wins the rename race at that moment has its own folders mounted: `data/` (other data read and written by `hippo`, uid 65532, distroless), `tailscale/config` (a `serve.json` with Funnel: Hippo exposed on the Internet if the tailnet allows Funnel), `tailscale/state` (the node's identity). No code runs as root by it. Only moving the project folder out of a parent writable by another account closes it (option a, Ghislain's decision pending).
- **Out of reach of the script**: the admin account can still write the share's other content, and whoever is root or in the `docker` group is root on the NAS anyway.

### D17. Compose gets copies, never a file of the share (2026-11-24, third review, 🛑)

The third `code-reviewer` pass showed with `strace` (Compose v5.7.1, `docker:cli` image) that after `cd -P`, `docker compose --project-directory . --env-file ./.env -f ./docker-compose.yml …` calls Getwd (`$PWD`) and then `openat("<absolute>/.env")` and `openat("<absolute>/docker-compose.yml")`: Compose opens its files by absolute path, on every call (`ps` of the health polls, `exec`, `up`, `config`, `pull`). An admin account watching the folder (inotify) and doing two renames around a call makes root run its compose. Fixed:

- **Copies**: once the lock is held, and before any `docker`, `./docker-compose.yml` and `./.env`, read by relative path (through the checked folder), are copied into `<state>/run/` (root, `0700`, files `0600`; emptied at the start of each run). Every Compose call on the installed compose gets `--project-directory . --env-file <state>/run/.env -f <state>/run/docker-compose.yml`; on the fetched compose, `-f <state>/staging/docker-compose.yml` with the same `--env-file`. After the install (point of no return), `up -d` and the health polls get the fetched compose, `<state>/staging/docker-compose.yml`, the same bytes as the file just installed, never the file of the share. `HIPPO_IMAGE_TAG` and the variable names are read from the copy of `.env` too.
- **`.env` on the system disk**: the copy of `.env` (secrets included) sits on the system disk, `0600` in a root `0700` folder, for the time of a run; `run/` is removed at the end of every run that took the lock, whatever the outcome (`trap … EXIT`). A run killed (`kill -9`, power loss) leaves it until the next run empties `run/`. Never in the log, never in the project folder.
- **Compose labels**: the containers record the files they were created from: `com.docker.compose.project.config_files` names `staging/docker-compose.yml` of the state folder (kept until the next run), and the environment file label, probably, `run/.env` (removed after the run; unverified, the hook forbids `docker inspect` to agents). Whether the UGOS Docker app still shows the project and can restart it is QA 7.5. A manual `docker compose` from the project folder finds the project by its name (`name: hippo`) as before.
- **Copy fails** (system disk full): message on standard error, exit 1, no log line (seen as a missing line the next morning, like D14 and D15).
- **Test**: the fake `docker compose` takes `$PWD`, then runs the race, then opens its files (D11). « A project folder swapped during the run is not followed » adds the race around every `docker compose` to the permanent swap of D16, and checks that no Compose call reads a file of either other folder, that `up -d` gets a compose outside the project folder and the `.env` of the checked folder, and that no copy of `.env` is left. It fails with the script of D16 (Compose reads the attacker's compose and `.env`) and passes with D17.
- **What remains**: the mounts of the compose, resolved by the daemon under the project folder's path (D16, « Remaining risk »).

## Risks / Trade-offs

- **The share holding the project folder was root on the NAS**: reduced by D15 (script outside it, ownership checked at each run), D16 (state outside it, project folder reached through its inode) and D17 (Compose gets copies in the state folder); what remains is listed in D16: the mounts `./data`, `./tailscale/config` and `./tailscale/state`, resolved by the daemon under the folder's path, can be swapped by whoever wins a rename race at container creation (other data in `hippo`, a `serve.json` with Funnel, another node identity); no code as root. Moving the project folder out of the UGOS share (and checking its ancestors) would close it: left to Ghislain.
- **`main` is root on the NAS** (proposal decision 6, ADR-0040). Guards: protected `main`, `ci-ok` (with `check-compose.sh`), `image.yml` on `main` only, hand brake.
- **Cron line dropped by a UGOS firmware update**: silent; the runbook checks the log after each UGOS update; the QA checks a reboot.
- **Image id semantics on the NAS** (D4): if container and image ids differ there, every night ends `unhealthy`; QA 7.1 checks it before the cron line is installed.
- **GitHub raw down at 04:00**: `fetch_failed`, nothing changes, next night retries.
- **An analysis pass still running at 04:00** is cut by the restart; it is picked up at start-up, and nothing missed is caught up (RG-30). Not expected with V1 volumes.
- **First switch from `sha-3fb9955` to `latest`**: Compose may recreate the container although the image is the same (unverified); harmless, and done by Ghislain at enable time, not by the script.

## QA results

### Before the merge (Ghislain on the NAS, 2026-11-24)

- **7.1 Facts.** `flock`, `getfacl`, `getfattr`, `setpriv` present; `cron` active; time zone: a CET/CEST zone other than `Europe/Paris`, with the same clock and the same DST changes, so 04:00 is 04:00 Paris time; `docker inspect -f '{{.Image}}' hippo-hippo-1` equals `docker image inspect -f '{{.Id}}'` of `sha-3fb9955` (`sha256:797b6556…`), so D4 holds; revision label `3fb995522651675e05468c31eaaf7f01cd3cdda8`.
- **Ownership (D15).** Every checked path belonged to root, but carried a `+`: UGOS applies its own ACL (`system.ugacl_self`, or inherited from the `docker` share), which `getfacl` does not show. Without `sudo`, the admin account `<admin>` could create files in the project folder and in `tailscale/config` (which was also `drwxrwxrwx`). Fixed by Ghislain: in the UGOS file manager, `hippo` › Properties › Permissions, account the admin account `<admin>` set to « Accès interdit », applied with « Remplacer »; then `chmod` of the seven paths (`700` folders, `755` `tailscale/config`, `600` compose and both env files, `644` `serve.json`), which on UGOS removes its ACL: no `+` left on them, `touch` without `sudo` refused, `docker compose restart` healthy. `setpriv … test -w` does **not** see the UGOS ACL (it answered writable where `touch` was refused): never rely on it. Side effect: the admin account can no longer enter the project folder; every runbook command now runs without `cd`, through `sudo` and absolute paths. A new UGOS admin account would get access by default: the runbook says to deny it too.
- **7.2.** Script installed in `/usr/local/sbin/hippo-update` from the branch commit `4187df3` (sha256 matched the repository's), first run by hand with the pinned tag: `paused`, exit 0; `/etc/cron.d/hippo-update` written (`0644`, root).
- **7.3.** With `HIPPO_IMAGE_TAG=latest`, a run by hand logged `failed`/`fetch_failed` with old and new revision `3fb9955…` (no script at that commit), exit 1, in 2 s; the three deployment files kept their checksums; the local `latest` tag was removed again (there was none before; the image stays under `sha-3fb9955`); `hippo` stayed healthy; `/var/lib/hippo-update` held `lock`, `staging/` and `update.log`, no `run/`. Hand brake set back to `sha-3fb9955`.

### After the merge

To fill (7.4 to 7.7).
