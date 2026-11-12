# Design

## Context

See proposal.md for the motivation, the two decisions taken with Ghislain on 2026-11-12 (backups stay on the NAS; folder `Sauvegardes/hippo`) and RG-98. This design settles how the backup is built; the specs (`specs/backup/spec.md`) give its observable behaviour.

Current state that shapes the approach:

- `store.Open` (`backend/internal/store/store.go`) opens one pool with WAL, `foreign_keys`, `busy_timeout(5000)`, `synchronous(NORMAL)` and `_txlock=immediate`, then applies the goose migrations and fills the normalised category names: it writes. The backup path must not go through it.
- `cmd/hippo` sweeps `.tmp-*` from `DATA_DIR/assets` (`openAssets`) and every regular file from `DATA_DIR/tmp` (`openTempDir`) at start-up, and opens the store. `hippo backup` runs beside a live service: it must do none of this.
- `assetfs` already names its temporary files `.tmp-` and its doc says backups skip them; `DATA_DIR/tmp` holds unnamed files only, which no directory listing ever sees.
- `config.Config.BackupSchedule` is an unvalidated string (`backend/internal/config/config.go:39`); `infra/.env.example` says it is « read but not used yet ». `schedule.ParseDaily`, `schedule.Run` and `capture.LoadParis` already serve `BATCH_SCHEDULE` (RG-30).
- `internal/extraction` measures free space (`freespace_{linux,darwin,other}.go`, `diskFree`) with a 64 MiB margin (`freeSpaceMargin`, `tempfile.go`). `graphify affected` finds no caller outside `extraction` (its `tempfile.go` and `export_test.go`).
- Both compose files mount `./data` only; `infra/check-compose.sh` checks the variables of `.env.example` against the `hippo` environment, with `COMPOSE_ONLY_VARS` for those Compose reads itself.
- No backup exists: the runbook (§ 7) copies `data/` by hand, Hippo stopped, before each update.

## Goals / Non-Goals

**Goals:**

- One code path for the nightly backup and `hippo backup`, safe beside the running service, that never writes the database nor cleans the data folder.
- A backup that is complete under its final name or absent, never half written; a failure that leaves nothing behind.
- Images stored once across backups (hard links between backups), never shared with the live data.
- A contract `nightly-update` can call: `docker compose exec hippo /hippo backup`, exit code zero only when a complete backup exists.

**Non-Goals:**

- Everything in the proposal's Non-goals (copy outside the NAS, restore from Hippo, `tailscale/state` and `.env`, automatic update, catching up a missed night, alerting).
- Verifying a backup by reading it back after writing it (an integrity check of the copy): the QA restore checks the procedure once; a nightly self-check is not asked for.
- Compressing or encrypting backups (they stay on the NAS, RG-92 rewritten).

## Vocabulary

Terms this change introduces or makes precise; `doc-keeper` adds them to `docs/glossary.md` (section « Coûts et exploitation ») in task 1.4.

| Term | Code | Meaning |
| --- | --- | --- |
| **Sauvegarde** | `backup`, package `internal/backup` | One dated folder of the backup folder holding a copy of the database, the images and a manifest, made each night or by `hippo backup` (RG-98). Not to be confused with « sauvegardé », used by the V2 digest for an article the user kept (`digest_trace`): a backup is never something the user saves. |
| **Dossier des sauvegardes** | `BACKUP_DIR` (backend, `/backups` in the image), `BACKUP_HOST_DIR` (Compose, the host folder mounted there: `Sauvegardes/hippo` on the NAS, `./backups` locally) | The folder that holds the backups, the lock file `.lock` and, during a backup, its temporary folder. « Dossier » means a file-system folder here; the « Mots à éviter » rule (never « dossier » for a catégorie) is about organising items, not about this. |
| **Nom d'une sauvegarde** | backup format `YYYY-MM-DDTHHMMSS±hhmm` | The Paris time at which the backup started, with its offset (`2026-11-13T030000+0100`). Only folders with such a name are ever removed by the retention. |
| **Dossier partiel** | `.partial-<name>` | The folder a backup is written in before its rename; one found at the start of a backup was left by a crash and is removed. |
| **Manifeste** | `manifest.json` | The small JSON file of a backup: versions, counts, sizes (D8). |
| **Version de migration** | `migration_version` | The last goose migration applied to a database. The proposal's « schema version » of the manifest; named so because « Version de schéma » (`schema_version`) already means the version of a fiche's JSON schema (RG-64). |
| **Déclencheur** | `trigger`: `nightly`, `command` | What started a backup: the schedule of the service, or `hippo backup`. |
| **Restauration** | — | The manual procedure of `docs/runbook-nas.md` that puts a backup back as the data folder. Hippo has no restore command (proposal, Non-goals). |

## Packages, contracts and artefacts touched

| Where | Change |
| --- | --- |
| `internal/backup` (new) | the backup: lock, free-space check, partial folder, database snapshot (through `store`), images walk with links and copies, manifest, sync and rename, retention, log line; `CheckDir` for the start-up. A new top-level package, justified by D1 |
| `internal/freespace` (new) | `Available(path)` and `Margin` (64 MiB), moved out of `internal/extraction` (D10) |
| `internal/extraction` | uses `freespace`; its `freespace_*.go` and `freeSpaceMargin` go; no behaviour change |
| `internal/store` | new `snapshot.go`: `Snapshot(ctx, dbPath, outPath) (migrationVersion int64, err error)` on its own read-only connection (D2, D3); `CreateAtVersion(ctx, path, version)`, exported for `storetest` only (a database behind the binary for the tests); `storetest` gains `IntegrityCheck`, `MigrationVersion`, `CreateAtVersion` and `HoldItemInsert` (an uncommitted item insert by SQL: no store function leaves a write open) |
| `internal/config` | `BackupSchedule` becomes a `schedule.Daily` parsed by `schedule.ParseDaily`; new `BackupDir` (`BACKUP_DIR`, default `/backups`); `BACKUP_DIR` equal to, inside or around `DATA_DIR` refused (D15) |
| `cmd/hippo` | sub-command `backup` (D13); `startBackup` beside `startAnalysis` (D14); start-up check of the backup folder (D15) |
| `backend/Dockerfile` | `ENV BACKUP_DIR=/backups`, no `VOLUME` for it (D15) |
| `backend/.golangci.yml` | depguard rule `backup-stays-on-the-nas` (D18) |
| `Makefile` | `make dev` sets `BACKUP_DIR=../data/dev-backups` and creates it; `make up` creates `infra/backups` |
| `infra/` | both compose files mount the backup folder; `check-compose.sh`, `.env.example`, `CLAUDE.md`; `.gitignore` `infra/backups/` (D16) |
| docs | `docs/metier.md` (RG-98, RG-92, RG-79), new ADR-0039 and ADR-0020's status, glossary, `docs/spec-v1.md`, `docs/backlog.md`, `docs/runbook-nas.md`, `docs/dev/conventions-backend.md` |

- **API contract** (`api/openapi.yaml`): no operation nor schema changes. Nothing of the backup goes through HTTP.
- **Migration**: none. The database is never written by the backup; no table is added.
- **Prompt and fiche schemas**: none; the eval set is not concerned.
- **Dependencies**: none. `syscall.Flock`, `syscall.Statfs`, `os.Root`, the existing `modernc.org/sqlite` and `goose`.

## Decisions

### D1. One package, `internal/backup`, called by the schedule and by `hippo backup`

`backup.Run(ctx, backup.Options) (backup.Result, error)` does a whole backup, under the lock. `Options` carries the database path (`DATA_DIR/hippo.db`), the images folder (`DATA_DIR/assets`), `BACKUP_DIR`, the Paris location, a `Now` function, the backend version, the trigger, the lock wait (zero: try once) and the logger. The paths are given one by one, never `DATA_DIR`: the package cannot reach `DATA_DIR/tmp`. SQLite stays in `store` (`store.Snapshot`), so `backup` imports neither `database/sql` nor the driver (conventions-backend, « Dépendances entre couches »).

Alternatives: the backup in `store` (it would mix file copies and retention into the database package); a sidecar container with `sqlite3` and `rsync` (an extra image to pin and watch, a second process touching the database, and the distroless image has neither tool); a shell script on the NAS (outside the repository's tests, and UGOS has no guaranteed `sqlite3`).

### D2. The database is copied by `VACUUM INTO` on its own read-only connection

`store.Snapshot` opens a new `*sql.DB` on `file:<path>?mode=ro&_pragma=busy_timeout(5000)`: no `_txlock`, no `journal_mode`, no migrations, no category fill, one connection (`SetMaxOpenConns(1)`). It runs `VACUUM INTO ?` with the target path (the partial folder's `hippo.db`; SQLite's message on a failed open names that file, « unable to open database: <path> », so the paths are cut out of its errors), closes, then opens the copy, `fsync`s it and closes it (the VACUUM's own syncs follow the source's pager flags, we do not rely on them).

Source, at `modernc.org/sqlite v1.61.0` (SQLite 3.54.1, `lib/sqlite.go:4564`):

- `VACUUM INTO` opens only a **read** transaction on the source: `sqlite3BtreeBeginTrans(pMain, pOut==0 ? 2 : 0)`, `lib/sqlite_g_0000000000000480.go:17849` (linux/amd64, also loong64), `lib/sqlite_g_0000000000003a00.go:9735` (linux/arm64), `lib/sqlite_g_0000000000000003.go:78151` (darwin). It refuses a target that exists and is not empty: `lib/sqlite_g_0000000000000480.go:17812`.
- Checked on 2026-11-12 with a scratch program on that version (darwin/arm64): on a WAL database with committed rows in the WAL and an **uncommitted write transaction open on another connection**, `VACUUM INTO` over `mode=ro` succeeds; the copy holds the committed rows only, passes `PRAGMA integrity_check`, its FTS5 table answers; the source's WAL is byte-identical after; any write on that connection fails (`attempt to write a readonly database`). A second `VACUUM INTO` to the same file fails (`output file already exists`).
- **`PRAGMA query_only(1)` is not an option**: on the same version, `VACUUM INTO` on such a connection fails with `attempt to write a readonly database`. `mode=ro` it is.
- The copy is in rollback-journal mode (header bytes 18 and 19 are 1): after a restore, the service's `journal_mode(WAL)` pragma converts it at start (spec scenario « A restored backup serves the items and images as before »).
- When no `-wal` exists (service stopped), the read-only connection creates `hippo.db-wal` and `hippo.db-shm` and leaves them, empty, owned by its process user. Harmless for uid 65532; a `docker compose exec -u root` would leave root-owned files the service cannot open. The runbook says never `-u root`.

Alternatives: the online backup API (`modernc.org/sqlite` `backup.go`, through `sql.Conn.Raw`): more code, and it restarts on every write of the source; reusing the store's pool (it carries `_txlock=immediate` and the write path; one bug from writing); copying the files with the service stopped (downtime, and `hippo backup` runs beside the service).

### D3. The migration version is read from the copy, by goose

After the copy, `store.Snapshot` opens the copy read-only and asks goose `Provider.GetDBVersion` (`pressly/goose/v3 v3.29.0`, `provider.go:185`), which only reads: `initialize` takes a lock only when one is configured (`provider_run.go:277`, none here), then `GetLatestVersion` selects from `goose_db_version`. Read from the copy, it is the version of exactly what was copied. It goes to the manifest and the log line as `migration_version`.

Alternative: `PRAGMA user_version` (goose does not write it); a query on the source (another read transaction, not the snapshot's).

### D4. Images: one walk of the images folder, database first

Order: database, then images (backlog E6-03, inherited from E2-04): an image captured after the snapshot is an orphan in the backup, harmless; a row without its file only happens when an image is removed in between (E4-05), counted as gone.

The walk goes through an `os.Root` on the images folder, `Lstat` only: regular files are backed up, at their relative path; folders are recreated (`0750`); `.tmp-*` files are skipped (any level); symbolic links and other types are skipped, never followed (`os.Root` refuses an escape anyway). A file listed then missing when opened (`fs.ErrNotExist`), or no longer a regular file once opened, is counted in `images_gone`, not an error. The whole folder is listed first, then copied: the test seam « after the database » runs between the listing and the first copy, which is where an image can be gone. A missing images folder fails the backup (a wrong `DATA_DIR`), as does a missing database (`mode=ro` never creates it). No query on the `assets` table: the folder is the source, so no business table is read (RG-90 below).

Alternative: list the image paths from the copied database (a query on a business table across users, and the copy is open read-only anyway); copy the whole folder blindly (would take `.tmp-*` and links).

### D5. Hard links from the previous backup only; byte copies otherwise

The previous backup is the most recent folder in the backup format, by its name (D7). For each image, when `prev/assets/<rel>` exists, is a regular file, and has the same size and the same modification time (to the nanosecond), the new file is `Root.Link(prev/assets/<rel>, .partial-<name>/assets/<rel>)` (`os/root.go:235`, Go 1.27.3). Any link error (`EXDEV`, `EMLINK`, `EPERM`, …) falls back to a copy, counted as copied.

A copy is written through a plain buffer (256 KiB) into a new file `O_EXCL`, mode `0640`, then its modification time is set to the source's (`Root.Chtimes`), so the next backup can link it, then it is `fsync`ed. The buffer is not an accident: `io.Copy` between two `*os.File` goes to `copy_file_range` on Linux (`os/zero_copy_linux.go:114`), which on Btrfs, the usual UGOS file system, may clone extents instead of copying them, so the backup would share its blocks with the live image. Images are never linked from the images folder: an in-place write to a live file never reaches a backup.

Accepted (ADR-0039, decision 1): each image exists once for all the backups that hold it; damage to that file reaches them all.

Alternatives: a content hash per image each night (reads every image every night); `rsync --link-dest` (not in the image); reflinks on purpose (shared with the live data, and not portable to the Mac's file systems in tests).

### D6. Written in `.partial-<name>`, synced, renamed

1. Under the lock, remove every `.partial-*` folder whose rest is in the backup format (leftovers of a crash: `leftovers_removed`). Nothing else is touched.
2. If `<name>` exists (`Lstat`), fail with `name_taken` before writing; any other error of that `Lstat` is `failed`. The check runs after the free-space check of D10, still before anything is written.
3. `Mkdir .partial-<name>` (`0750`), snapshot, images, manifest.
4. `fsync` each folder of the partial tree after its entries (bottom-up), then the partial folder.
5. `Lstat <name>` again, then `Root.Rename(.partial-<name>, <name>)`, then `fsync` the backup folder: the model of `assetfs.(*Upload).Commit`.

The second `Lstat` matters: `os.Rename` refuses to replace a directory (`os/file_unix.go:26-44`), but `Root.Rename` calls `renameat` directly (`os/root_unix.go:248`), which replaces an **empty** directory. Under the lock no other backup can create `<name>` in between.

Any error after step 3, or a cancelled context (`stopped`), removes the partial folder (`Root.RemoveAll`, which removes links without following them) before returning.

### D7. Name: Paris time with its offset, without colons

`now.In(paris).Format("2006-01-02T150405-0700")`. The offset keeps names unique across the autumn overlap (02:30 happens twice) and makes each name an exact instant, so retention parses it back with the same layout. No colon: the folder is browsed over SMB from the Mac, where a colon is not a valid name character. The Paris location comes from `capture.LoadParis` (embedded tz database; `TZ` only sets the log timestamps, conventions-backend « Configuration »).

Alternatives: RFC 3339 (colons); UTC names (Ghislain reads Paris time on the NAS); a sequence number (meaningless when browsing).

### D8. Manifest

`manifest.json`, mode `0640`, written last before the syncs:

```json
{
  "format": 1,
  "name": "2026-11-13T030000+0100",
  "started_at": "2026-11-13T03:00:00+01:00",
  "trigger": "nightly",
  "backend_version": "<git revision>",
  "migration_version": 7,
  "database_bytes": 1048576,
  "images": 120,
  "images_copied": 2,
  "images_linked": 118,
  "images_gone": 0,
  "image_bytes_written": 5242880
}
```

No file name, no item content. The restore reads `backend_version` to pin the image tag (`sha-` and its first 7 characters, runbook) and `migration_version` to know whether a migration lies between the backup and the running version.

### D9. Retention

After a successful backup only: parse every folder name in the backup format (`Lstat`: directories only, never a file nor a link with such a name); sort by instant; keep the 3 most recent and the one just made; remove (`Root.RemoveAll`) the others whose instant is more than 14 × 24 h before the new backup's start. A removal error is logged at `error` on its own line (`backup retention`, no path) and does not turn the backup into a failure: the backup exists, which is what `nightly-update` needs.

With a nightly backup this keeps 14 or 15 nightly folders plus every `hippo backup` of the last 14 days. A clock jumping years ahead would leave the 3 most recent and the new one: accepted, and the log line shows `backups_removed`.

### D10. Free space checked before writing; `freespace` shared

`diskFree` and `freeSpaceMargin` move to `internal/freespace` (`Available(path) (free uint64, known bool, err error)`, `Margin = 64 << 20`, one file per system as today; same `syscall.Statfs` with `Bavail * Bsize`). `extraction` imports it: no behaviour change, its tests stay green unchanged except the exported margin in `export_test.go`. Only one package calls it today, so no add / migrate / remove split.

Before writing, `backup` makes a plan: the size of `hippo.db` plus `hippo.db-wal` (an upper bound of the copy), plus the sizes of the images it would copy (walk of D4, compared with the previous backup as in D5), plus `freespace.Margin`. `Available(BACKUP_DIR)` below that: `no_room`, nothing written. The plan's listing is an estimate; the real walk happens after the snapshot (D4). An image added in between that does not fit fails its write, and D6 removes the partial folder. Unknown free space (other systems): the check passes, the write fails if it must.

### D11. Lock: `flock` on `.lock`, across processes

`Root.OpenFile(".lock", O_RDWR|O_CREATE, 0640)`, then `syscall.Flock(fd, LOCK_EX|LOCK_NB)`; `EWOULDBLOCK` means taken. The nightly backup tries once (`skipped`, `warn`). `hippo backup` retries every second up to 10 minutes (`locked`, exit non-zero), and writes one line `waiting for the backup lock` (`info`) when it starts waiting, so that the operator sees why nothing happens; the name and the start time of the backup are read once the lock is taken. The file is never removed and never holds data; `CheckDir` creates it at start-up.

- A `flock` lock belongs to the open file description and goes with its last descriptor, process death included; two `open` of the same file, even in one process, conflict (Linux `flock(2)`, https://man7.org/linux/man-pages/man2/flock.2.html; macOS `flock(2)` has the same semantics). So the in-process tests are real, and one test kills a child process that holds the lock (spec « The lock of a killed process is released »).
- Go opens every file `O_CLOEXEC` (`os/root_unix.go:85`): the children `extract-page` and `copy-image` never inherit the lock.
- `lock_unix.go` (`//go:build linux || darwin`), `lock_other.go` returns an error: the service runs on Linux, tests run on Linux and macOS.
- The backup folder must be a local file system of the NAS (a shared folder, not a network mount): `flock` over SMB or NFS is not reliable. Runbook.

Ten minutes: far above a nightly backup of V1 (seconds: a few MB of database, a handful of new images), and above the first full copy of the images expected in V1 (hundreds of MB on the NAS's disks); the QA measures the first full backup and records it here. If that first full backup takes more than 5 minutes, the wait is raised in this design and in the spec's requirement in the same move, before merging. Alternatives: a lock inside SQLite (the backup must not write the database); a PID file (stale after a crash, needs liveness checks); no lock (two backups in the same second race on the name, two writers on the same partial folder).

### D12. One log line; outcomes

One JSON line `backup` per backup, written by `Run` before it returns, on the service's logger (nightly) or the command's (D13): `outcome`, `trigger`, `backup` (the name), `backend_version`, `migration_version`, `database_bytes`, `images`, `images_copied`, `images_linked`, `images_gone`, `image_bytes_written`, `duration_ms`, `backups_removed`, `leftovers_removed`, and `error` when it failed. Outcomes: `ok` (`info`), `skipped` (`warn`), `locked`, `no_room`, `name_taken`, `failed`, `stopped` (`error`). Errors are made path-free like `assetfs.pathless` (the cause only); no image path, no item content, ever (conventions-backend « Logs »). The service's line is written at `info` (`ok`), `warn` (`skipped`) or `error`: a `LOG_LEVEL` above `info` (`warn`, `error`) hides the nightly `ok` line, and one above `warn` hides `skipped` too; `.env.example` and the runbook say so. `backup` is empty when the backup failed before taking the lock (backup folder missing, lock taken). A lock file that fails to close after the backup is logged on its own line (`backup lock`, `error`) and does not fail a complete backup. Should the final sync of the backup folder fail after the rename, the backup is reported `failed` though its folder exists: the conservative side for `nightly-update`.

### D13. `hippo backup`

`run` dispatches `backup` before the service path: `config.Load(getenv)` (the container's environment is the service's: same `DATA_DIR`, `BACKUP_DIR`, `LOG_LEVEL`), JSON logger on standard output, `capture.LoadParis`, `debug.SetMemoryLimit(32 MiB)` (a soft limit of the Go heap: the command is a fourth Go process in the 384 MB of ADR-0038; set in `main` when the first argument is `backup`, so that the tests, which call `run` in-process, keep the test binary's limit), then `backup.Run` with trigger `command` and a 10-minute lock wait. It opens neither the store nor the images folder through `assetfs`, nor `DATA_DIR/tmp`: no migration, no sweep. `SIGINT`/`SIGTERM` cancel the context (`stopped`). Its handler logs at `min(LOG_LEVEL, info)`: the line of the backup is the command's report and is always printed, whatever `LOG_LEVEL` says (the nightly line, on the service's logger, follows `LOG_LEVEL`, D12). Success returns nil (exit 0); any failure returns an error (exit 1, `hippo: <error>` on standard error, no path). The log line on standard output is what the operator and `nightly-update` read: an `exec` does not reach the Docker logs.

Run with `docker compose exec hippo /hippo backup`, never `run`: `run` starts a new container of the current image tag, which would be the new version during an update, and the backup would not be the old version's. The command itself never migrates, but the runbook says `exec` only.

SQLite's page cache is allocated by `modernc.org/libc` outside the Go heap, so `SetMemoryLimit` does not bound it; the default `cache_size` (-2000, about 2 MiB) does. **Unverified** in the container: the QA measures the first full backup with `docker stats` (task 11.2).

### D14. Nightly backup in the service

`startBackup(ctx, backupOptions)` in `cmd/hippo/backup.go`, beside `startAnalysis`: one goroutine `schedule.Run(ctx, cfg.BackupSchedule, paris, clock, logger.With("schedule", "backup"), task)`, whose task calls `backup.Run` with trigger `nightly` and no lock wait. The `With` makes the existing `scheduled run missed` line of `schedule.Run` tell the backup from the batch. A start-up line `backup scheduled` gives `backup_schedule`, `time_zone` and `next_run` (no path). `stop` cancels and waits, deferred like the analysis. The batch (02:00) and the backup (03:00) are independent: the backup only reads; on the spring clock change both run at 03:00, which is fine.

### D15. Configuration and start-up check

- `BACKUP_SCHEDULE`: `schedule.ParseDaily` in `config.Load`, error `BACKUP_SCHEDULE: …` like `BATCH_SCHEDULE` (conventions-backend « Configuration »); default `0 3 * * *`.
- `BACKUP_DIR`: default `/backups`. Like `DATA_DIR`, fixed by the image (`ENV BACKUP_DIR=/backups`) and by `make dev` (`../data/dev-backups`, created by the target), never set in `.env`. `config.Load` refuses it equal to, inside or around `DATA_DIR` (lexical, after `filepath.Clean` and `filepath.Abs`): a backup inside the data would back itself up.
- Start-up: `backup.CheckDir(BACKUP_DIR)` after the data folder: the folder must exist and be a directory, a probe file `.probe-<random>` must be created (`O_EXCL`) and removed (as `extraction.OpenTempDir` does: a folder whose `.lock` exists but where nothing can be created is refused), and `.lock` must open for writing. It never creates `BACKUP_DIR`: in the container, a missing mount leaves no `/backups` (the image declares no `VOLUME` for it, which would hide a missing mount behind an anonymous volume), and the service refuses to start instead of running without backups. Same rule as the assets and temporary folders, which fail at start rather than at first use (conventions-backend « Dossier temporaire »).
- Existing tests of `cmd/hippo` set `BACKUP_DIR` to a `t.TempDir()`.

### D16. Compose and the static check

- Production: `- ${BACKUP_HOST_DIR:?set BACKUP_HOST_DIR in infra/.env}:/backups` on `hippo`. An `.env` without it stops `docker compose up`, rather than writing into the project folder.
- Local: `- ${BACKUP_HOST_DIR:-./backups}:/backups`; `infra/backups/` in `.gitignore` (public repository, ADR-0027); `make up` creates it.
- `.env.example`: `BACKUP_HOST_DIR=` uncommented and empty (NAS: `/<volume>/Sauvegardes/hippo`), `BACKUP_SCHEDULE` comment rewritten (used now, daily form only), `BACKUP_DIR=/backups` in the commented « fixed by the image » block.
- `check-compose.sh`: `BACKUP_HOST_DIR` joins `COMPOSE_ONLY_VARS`; new production assertion: `hippo` has a volume whose target is `/backups` and whose source reads `${BACKUP_HOST_DIR:?`; new local assertion: a volume whose target is `/backups`. **Unverified**: how `config --no-interpolate --format json` renders a variable source (Compose may prefix it with the project folder, as it does for `./data`); the assertion matches « contains `${BACKUP_HOST_DIR:?` » and task 8.1 checks the rendering first.
- **Deviations found while implementing (8.1, 8.2):** without interpolation, Compose splits the short form `${BACKUP_HOST_DIR:?…}:/backups` on its colons (source `${BACKUP_HOST_DIR`, no `/backups` target), so both compose files use the long syntax (`type: bind`, `source`, `target`); the rendered source is prefixed with the project folder and still contains `${BACKUP_HOST_DIR:?`. Both volumes also set `bind.create_host_path: false` (asserted in production): without it Docker creates a missing host folder as root even in the long syntax (seen on Docker Desktop), and `up` now stops on « bind source path does not exist » instead (Linux Docker of the NAS: checked during the QA). A new common assertion refuses any `COMPOSE_ONLY_VARS` variable in the `environment` of `hippo`. `make check-infra` passes `BACKUP_HOST_DIR=/check` (an absolute path; a bare word would be read as a named volume).

### D17. Permissions

Files `0640`, folders `0750`, like `assetfs` (conventions-backend « Fichiers des images »); the process umask (022 in the container) does not widen them. The backup folder on the NAS belongs to uid 65532, mode `0750`, without UGOS ACL (`+`), checked by the runbook like `data/`.

### D18. How the guarantees hold

- **RG-12 and RG-92 (rewritten)**: the backup path reads local files and writes local files. depguard rule `backup-stays-on-the-nas` forbids `internal/backup` to import `net`, `net/http`, `internal/analysis` and its sub-packages, `internal/extraction`, `internal/httpapi`, and `database/sql` (SQLite stays in `store`): no path from a backup to the network or the AI exists, and `make lint-backend` proves it on every commit. `store.Snapshot` sends nothing either (`store-knows-no-transport`). Private items are in the backups, on the NAS only (RG-98); the runbook checks the backup folder is out of every UGOS sync.
- **RG-90**: not applicable by RG-98's own terms. A backup is a copy of the installation, every user included; it runs no query on a business table (`VACUUM INTO` copies pages; goose reads `goose_db_version`; images come from the folder, D4).
- **RG-81**: no AI call, nothing to check.
- **RG-79**: an item deleted stays in the backups until their retention (RG-98, cross-reference added to RG-79).

## Risks / Trade-offs

- [Memory of `hippo backup` beside the service and a batch's image child, 384 MB in all] → soft limit of 32 MiB on the Go heap, a 256 KiB copy buffer, SQLite's default cache; the QA runs the first full backup under `docker stats` and records the peak here. Above 64 MiB of resident memory, reconsider before merging.
- [UGOS ACL on `Sauvegardes` overriding the `chown`] → the service refuses to start (D15) instead of running without backups; the runbook checks `ls -ldn` and the absence of `+`.
- [UGOS sync or cloud backup app picking up `Sauvegardes`] → RG-92 broken outside Hippo's reach; the runbook checks every sync and backup task of UGOS excludes the folder.
- [Backups on the same volume as the data fill it] → the free-space check with a 64 MiB margin, shared with the extraction; 14 days; images stored once.
- [Damage to one image file reaches every backup that links it] → accepted with ADR-0039; RAID 1 and Btrfs checksums cover the disk; never linked to the live data (D5).
- [Theft, fire, surge, ransomware reach the NAS and its backups together] → accepted with ADR-0039 (decision 1).
- [A root-owned `-shm` left by `exec -u root` on a stopped service] → runbook: `docker compose exec hippo /hippo backup`, never `-u root`, never `run`.
- [The update that installs this change] → the running image has no `backup` command yet: that update keeps the manual copy of § 7, and the backup folder and `BACKUP_HOST_DIR` must exist before `up -d` (Compose refuses otherwise, then the service refuses a missing folder). The runbook says so.
- [A long `VACUUM INTO` holds a read transaction, so checkpoints cannot pass it] → the WAL grows for the duration (seconds in V1) and is checkpointed after.
- [A wrong NAS clock] → names and retention follow it; the 3 most recent are always kept.

## Migration Plan

1. Merge; the image is published by the CI.
2. On the NAS (runbook, QA): create the shared folder `Sauvegardes` and `hippo` in it, owned by 65532, mode 750, out of any sync; add `BACKUP_HOST_DIR` to `.env`; update with the manual copy one last time; check `hippo backup`, then the next night's line `backup`; restore once on a `data/` set aside.
3. Rollback: the previous image ignores `BACKUP_*` (an unknown variable is harmless, `/backups` unused); the compose of the previous SHA has no backup volume. Backups made stay in `Sauvegardes/hippo` and remain restorable by hand.
