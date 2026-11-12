# Proposal

Backlog story: **E6-03 — Sauvegardes** (decided with Ghislain on 2026-11-12: E6 goes on with `nightly-backup`, then `nightly-update`, the automatic update of the NAS, which will take a backup before each update).

Rules: RG-12, RG-30, RG-79, RG-90, **RG-92 (rewritten)**, **RG-98 (new, agreed with Ghislain on 2026-11-12)** · ADRs: ADR-0004, ADR-0019, **ADR-0020 (replaced in part by a new ADR)**, ADR-0027, ADR-0032, ADR-0038.

## Why

Hippo runs on the NAS since `deploy-nas` (2026-11-12), with no backup: the RAID protects from a disk failure, not from a migration or a bug that damages the database, nor from a mistake. Ghislain wants nothing precious in Hippo before a backup exists, and the automatic update planned next (`nightly-update`) is only safe with a backup taken just before it.

## What Changes

- **Decisions taken with Ghislain (2026-11-12)**:
  1. **Backups stay on the NAS, no copy outside it.** ADR-0020 asked for a nightly encrypted copy to an external storage; Ghislain does not want a cloud service nor its cost. A new ADR replaces that part of ADR-0020 and records the accepted risks: theft, fire, a power surge or ransomware reaching the NAS takes Hippo and its backups together; each image exists once on disk for all the backups that hold it (hard links), so damage to that file reaches them all. RG-92 is rewritten: no data leaves the NAS for a backup.
  2. **Location: the shared folder `Sauvegardes`, sub-folder `hippo`**, apart from the project folder `docker/hippo`: deleting or reinstalling the project leaves the backups intact.
- **Business rule RG-98 (agreed with Ghislain on 2026-11-12, below)**: what a backup keeps of a deleted or private item, and what a restore brings back.
- **One backup code path, nightly and on demand**:
  - nightly at `BACKUP_SCHEDULE` (default 03:00 Paris time, daily form only, parsed like `BATCH_SCHEDULE`: another form stops the start-up), and on demand with `hippo backup`, run in the running container (`docker compose exec hippo /hippo backup`, never `run`: a new image would migrate the database before backing it up);
  - the path never writes to the database and cleans nothing: its own connection without migrations nor start-up fixes, no removal of temporary uploads or files (they belong to the running service);
  - one backup at a time, across processes: an advisory lock on a file of the backup folder (`flock`, released if the process dies). The nightly backup that finds it taken is skipped with a log line; the command waits up to a bound, then fails; any failure exits non-zero (`nightly-update` relies on it);
  - a consistent copy of the database while the service runs (`VACUUM INTO`), then the images of `DATA_DIR/assets`, temporary files `.tmp-*` left out. An image gone between the two steps (possible once E4-05 removes images) is counted, not an error;
  - an image already in the previous complete backup, same relative path, size and modification time, is hard-linked from it; otherwise, or when the link fails (another file system, too many links, permission), it is copied;
  - each backup is written in a temporary folder, synced (files and folders), then renamed to its final name: `YYYY-MM-DDTHHMMSS±hhmm` in Paris time; a name already taken is a failure. A temporary folder left by a crash is removed under the lock. The folder holds a small manifest: backend version, schema version, counts, sizes;
  - free-space check before writing: the database size, the images to copy and a margin; a backup that cannot fit does not start; a write that fails midway removes its temporary folder;
  - retention after a successful backup: complete backups older than 14 days, by their name, are removed, except the 3 most recent and the one just made; only folders named in the backup format are ever touched;
  - log line per backup: outcome, folder name, backend and schema versions, database size, images copied, linked and gone, bytes, duration, backups removed; never an image file name, never an item's content. The command prints the same line on its output (an `exec` does not reach the Docker logs).
- **Configuration**: `BACKUP_DIR`, the backup folder seen by the backend (default `/backups`; fixed by the image and `make dev`, never set in `.env`, like `DATA_DIR`); `BACKUP_HOST_DIR`, the host folder mounted there by Compose, **required** in the production compose (`${BACKUP_HOST_DIR:?…}`: an `.env` without it stops `docker compose up` instead of writing into the project folder), defaulted to `./backups` in the local compose only; `infra/backups/` ignored by git (the repository is public, ADR-0027).
- **Restore**, documented in `docs/runbook-nas.md` and **tested once on the NAS during the QA** (ADR-0020), on a `data/` set aside, never over real data: stop Hippo; set `data/` aside; copy (never move nor link) a backup's database and images into a new `data/`; leave no `-wal` nor `-shm` file; give it to uid 65532; pin the image tag of the manifest's version; start. Section 7 (update) uses `hippo backup` instead of the manual copy; section 8 (rollback) restores a backup.
- **Infra and docs**: second volume in both compose files; `infra/check-compose.sh` checks the production `/backups` mount reads `${BACKUP_HOST_DIR:?`; `infra/.env.example`, `infra/CLAUDE.md`; the new ADR and ADR-0020's status; `docs/metier.md` (RG-92, RG-98); `docs/glossary.md` (« Sauvegarde », distinct from « sauvegardé » of the V2 digest); `docs/spec-v1.md` « Déploiement »; the runbook: folder `Sauvegardes/hippo` created and owned by 65532, checked out of any UGOS sync (the content must not leave the NAS, RG-92), sections 7 to 10; backlog E6-03.

## Capabilities

### New Capabilities
- `backup`: the nightly and on-demand backup of the database and images on the NAS, its lock, retention, manifest and restore. Exception to the spec rules of `openspec/config.yaml`, stated here: nothing of it goes through the API; the observable behaviour is the content of the backup folder, the database left unchanged, the exit code and output of `hippo backup`, and the log line. Each scenario has its Go integration test on `t.TempDir()`, a real SQLite with real migrations. `nightly-update` will rely on this contract.

### Modified Capabilities
- none

## Non-goals

- Any copy outside the NAS (decision 1); Btrfs snapshots of UGOS (a possible complement, outside Hippo).
- Restoring from Hippo itself (an API route or a command): restore stays a documented manual procedure.
- Backing up `tailscale/state` or `infra/.env`: the node is re-enrolled with a new key, `.env` lives in the password manager.
- The automatic update (`nightly-update`, next change, with its own ADR: ADR-0019 says updates are manual).
- Catching up a night missed while the NAS was off (not caught up, like the batch).
- Alerting on a failed backup beyond the log line and the exit code.

## Decision taken with Ghislain (2026-11-12, stop cases 1 and 2: business rule)

**RG-98 — what a backup keeps: accepted as worded.** A backup is a copy of the whole database and images on the NAS. So: an item deleted on Monday stays readable in the backups until the 14th day (RG-79 says the deletion is final); a private item is in the backups like the others, on the NAS only; restoring a backup brings back the items deleted since, and loses the captures made since. Wording added to `docs/metier.md`: « Chaque nuit à 3 h, et avant chaque mise à jour, Hippo copie sa base et ses images dans un dossier de sauvegarde du NAS ; les sauvegardes de plus de 14 jours sont effacées. Un élément supprimé reste dans les sauvegardes jusqu'à leur effacement ; restaurer une sauvegarde rend les éléments supprimés depuis et perd les captures faites depuis. Les sauvegardes ne quittent jamais le NAS. » RG-79 gets a cross-reference to it.

## Impact

- **AI cost**: none.
- **Private items**: backed up like the others, on the NAS only; nothing leaves the NAS (RG-92 rewritten); the runbook checks the backup folder is out of any UGOS sync.
- **Persisted data**: no migration, the database is never written by the backup. New files in the backup folder: the images once, plus 14 to 28 copies of the database (a few MB each in V1).
- **API contract**: none.
- **Prompt and fiche schemas**: none.
- **Memory**: `hippo backup` is a fourth Go process in the container's 384 MB (ADR-0038); it sets a soft memory limit, and the first full backup is measured with `docker stats` during the QA.
- **Dependencies**: none (`syscall.Flock`, `syscall.Statfs`, the existing SQLite driver).
- **User QA: yes** — Ghislain creates the backup folder, checks a nightly backup and a `hippo backup`, and restores once on a `data/` set aside, following the runbook.
