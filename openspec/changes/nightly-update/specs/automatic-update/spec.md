# Spec Delta

## Purpose
Keeps Hippo on the NAS in line with what `main` publishes, every night at 04:00, without Ghislain: it backs up before changing anything, leaves a failed update as it is, and writes one log line per run. Nothing of it goes through the API: its observable behaviour is the Docker commands it runs, the files it puts in place, its exit code and its log line.

Terms used below: the **project folder** is the NAS folder that holds `docker-compose.yml`, `.env`, `.env.example`, `tailscale/config/serve.json` and the **update script**; the **deployment files** are those three files plus the update script; the **published image** is `ghcr.io/ghislain95/hippo:latest` on GHCR; its **revision** is the commit it was built from, read from its `org.opencontainers.image.revision` label; a **run** is one execution of the update script; the **update log** is the log file of the update script in the project folder. Every scenario runs the update script in a temporary project folder, with `docker`, `curl` and `flock` replaced by fakes that record their calls.

## ADDED Requirements

### Requirement: A pinned image tag pauses the automatic update
A run MUST read `HIPPO_IMAGE_TAG` from `.env` without executing `.env`, and MUST do nothing but log `paused` and exit 0 when it is anything other than `latest`. A run MUST fail when `.env` or its `HIPPO_IMAGE_TAG` line is missing (RG-99).

#### Scenario: A pinned tag pauses the update
- **GIVEN** `.env` holds `HIPPO_IMAGE_TAG=sha-3fb9955`
- **WHEN** a run starts
- **THEN** it logs `paused` and exits 0, and runs no `docker` nor `curl` command

#### Scenario: A quoted latest tag is followed
- **GIVEN** `.env` holds `HIPPO_IMAGE_TAG="latest"` with a trailing space
- **WHEN** a run starts with a new published image
- **THEN** the run goes on and pulls the published image

#### Scenario: A missing image tag fails the run
- **GIVEN** `.env` has no `HIPPO_IMAGE_TAG` line
- **WHEN** a run starts
- **THEN** it logs `failed` with the reason `no_image_tag`, exits non-zero and runs no `docker` command

#### Scenario: The environment file is never executed
- **GIVEN** `.env` holds a line that would run a command if the file were executed by the shell
- **WHEN** a run starts
- **THEN** the command is never run

### Requirement: One run at a time
A run MUST hold an exclusive lock for its whole duration; a run that finds the lock taken MUST log `locked`, exit non-zero and change nothing (RG-99).

#### Scenario: A second run while one holds the lock
- **GIVEN** the lock is held
- **WHEN** a run starts
- **THEN** it logs `locked`, exits non-zero and runs no `docker` command

### Requirement: The deployment files come from the commit of the published image
A run MUST pull the published image, read its revision, and fetch the deployment files of that revision from the public repository into a staging folder, without touching the running stack. A revision that is not 40 lowercase hexadecimal characters, or a fetch that fails, MUST fail the run with nothing replaced (RG-99, ADR-0040).

#### Scenario: The files are fetched at the image's revision
- **GIVEN** the published image has the revision `aaaa…` (40 characters)
- **WHEN** a run starts
- **THEN** each deployment file is fetched from the repository at `aaaa…`, and none from a branch name

#### Scenario: A malformed revision fails the run
- **GIVEN** the published image has no revision label, or a label that is not 40 hexadecimal characters
- **WHEN** a run starts
- **THEN** it logs `failed` with the reason `bad_revision`, exits non-zero, fetches nothing and replaces no file

#### Scenario: A failed fetch fails the run
- **GIVEN** the repository does not answer for one of the deployment files
- **WHEN** a run starts
- **THEN** it logs `failed` with the reason `fetch_failed`, exits non-zero and replaces no file

#### Scenario: A failed pull fails the run
- **GIVEN** GHCR does not answer
- **WHEN** a run starts
- **THEN** it logs `failed` with the reason `pull_failed`, exits non-zero, and runs no backup and no `up`

### Requirement: Nothing new means nothing happens
When the running `hippo` container uses the same image as the published image, and the fetched deployment files are byte for byte the installed ones, a run MUST log `up-to-date` and exit 0 without a backup, a pull of another image, a file change nor a restart (RG-99).

#### Scenario: Same image and same files
- **GIVEN** the running container uses the published image and the installed files equal the fetched ones
- **WHEN** a run starts
- **THEN** it logs `up-to-date`, exits 0, runs no backup and no `up`, and changes no file

#### Scenario: A changed deployment file alone is an update
- **GIVEN** the running container uses the published image, but the fetched `docker-compose.yml` differs from the installed one
- **WHEN** a run starts
- **THEN** the run goes on to the backup and the update

### Requirement: Files that do not fit the environment are refused
Before any backup, a run MUST refuse the new files, with nothing replaced, when `.env` lacks a variable that the new `.env.example` sets on an uncommented line (reason `missing_variables`, the missing names logged), or when the new compose fails `docker compose config --quiet` with the real `.env` (reason `compose_invalid`). The output of `docker compose config` MUST never be logged (RG-99).

#### Scenario: A new required variable is missing from the environment file
- **GIVEN** the new `.env.example` sets `NEW_SETTING=` and `.env` does not
- **WHEN** a run starts with a new published image
- **THEN** it logs `failed` with the reason `missing_variables` and the name `NEW_SETTING`, exits non-zero, runs no backup and replaces no file

#### Scenario: A commented variable is not required
- **GIVEN** the new `.env.example` holds `# DATA_DIR=/data` and `.env` has no `DATA_DIR`
- **WHEN** a run starts with a new published image
- **THEN** the run goes on to the backup

#### Scenario: A compose that does not render is refused
- **GIVEN** `docker compose config --quiet` fails on the new compose
- **WHEN** a run starts with a new published image
- **THEN** it logs `failed` with the reason `compose_invalid`, exits non-zero, runs no backup, replaces no file, and its log line holds nothing printed by `docker compose`

### Requirement: The running version is backed up before anything changes
After the new images are pulled and before any file is replaced, a run MUST back up with the running version, `docker compose exec -T hippo /hippo backup` on the installed compose, and never with `docker compose run`. A backup that exits non-zero, or a `hippo` service that is not running, MUST fail the run with nothing replaced (RG-98, RG-99).

#### Scenario: The backup runs in the running container before the files change
- **GIVEN** a new published image
- **WHEN** a run updates Hippo
- **THEN** `docker compose exec -T hippo /hippo backup` runs once, after the pull of the other images and before any deployment file is replaced, and `docker compose run` never runs

#### Scenario: A failed backup stops the update
- **GIVEN** a new published image and a backup that exits non-zero
- **WHEN** a run starts
- **THEN** it logs `failed` with the reason `backup_failed`, exits non-zero, replaces no file and runs no `up`

#### Scenario: Hippo not running stops the update
- **GIVEN** no running `hippo` container
- **WHEN** a run starts with `HIPPO_IMAGE_TAG=latest`
- **THEN** it logs `failed` with the reason `hippo_not_running`, exits non-zero, runs no backup and replaces no file

### Requirement: A run stopped before the update leaves the local latest tag as it was
When a run stops after pulling the published image and before replacing any file, the local `latest` tag MUST point again to the image it pointed to before the run, or be removed when there was none, so that a later `docker compose up` never installs the new version without its backup (RG-98, RG-99).

#### Scenario: The previous latest tag is put back after a failed backup
- **GIVEN** the local `latest` tag points to the running image, and a new published image whose backup fails
- **WHEN** the run stops
- **THEN** the local `latest` tag points to the running image again

### Requirement: An update replaces the files, restarts and checks the health
After a successful backup, a run MUST keep a copy of the installed deployment files, put the new ones in place (each by a rename, never written in place), run `docker compose up -d`, then wait up to 3 minutes for the `hippo` container to be `healthy` on the new image. It MUST then log `updated` with the old and new revisions and the backup name, and exit 0; otherwise it MUST log `failed` with the reason `up_failed` or `unhealthy`, the same facts, leave everything as it is, and exit non-zero (RG-99).

#### Scenario: A successful update
- **GIVEN** a new published image and a healthy start
- **WHEN** a run updates Hippo
- **THEN** the installed deployment files are the fetched ones, the previous ones are kept apart, `docker compose up -d` ran once, and the run logs `updated` with the old revision, the new revision and the backup name, and exits 0

#### Scenario: An unhealthy start is logged and left as is
- **GIVEN** a new published image whose container never becomes `healthy`
- **WHEN** a run updates Hippo
- **THEN** after at most 3 minutes it logs `failed` with the reason `unhealthy`, the old and new revisions and the backup name, exits non-zero, runs no other `up` and leaves the new files in place

#### Scenario: A failed start is logged and left as is
- **GIVEN** a new published image and a `docker compose up -d` that exits non-zero
- **WHEN** a run updates Hippo
- **THEN** it logs `failed` with the reason `up_failed` and the backup name, exits non-zero and leaves the new files in place

### Requirement: The update script replaces itself only after a successful update
A run MUST put the fetched update script in place of the running one, by a rename, only after it logged `updated`; never after any other outcome (RG-99, ADR-0040).

#### Scenario: The script is replaced after an update
- **GIVEN** a new published image whose update script differs from the installed one
- **WHEN** a run logs `updated`
- **THEN** the installed update script is the fetched one, executable by its owner only

#### Scenario: The script is kept after a failed update
- **GIVEN** a new published image whose container never becomes `healthy`
- **WHEN** the run logs `failed`
- **THEN** the installed update script is unchanged

### Requirement: An interrupted run is reported
A run that finds the marker left by a run interrupted while installing MUST log `interrupted` with that run's new revision, remove the marker, then go on as a normal run (RG-99).

#### Scenario: A marker left by a crash
- **GIVEN** the marker of a run interrupted while installing revision `bbbb…`
- **WHEN** a run starts
- **THEN** it first logs `interrupted` with the revision `bbbb…`, then goes on, and leaves no marker when it ends

### Requirement: Each run writes one bounded log line, never a secret
Each run MUST append to the update log, and print on its output, JSON lines with the time, the outcome and only these facts: the reason, the old and new revisions, the backup name, the missing variable names and the duration; every value MUST be checked against its expected form before being written. The update log MUST be readable by its owner only, and MUST be rotated once it exceeds 1 MiB, keeping one previous file. No value of `.env`, no output of `docker compose config` and no item content is ever written (RG-99, RG-92).

#### Scenario: Secrets of the environment file never reach the log
- **GIVEN** `.env` holds `API_TOKEN=s3cr3t-token-value` and a new published image whose start is healthy
- **WHEN** a run updates Hippo
- **THEN** neither the update log nor the run's output holds `s3cr3t-token-value`

#### Scenario: The log is readable by its owner only
- **WHEN** a run ends
- **THEN** the update log has mode `0600` and holds one more line per outcome logged, each a JSON object

#### Scenario: A full log is rotated
- **GIVEN** an update log larger than 1 MiB
- **WHEN** a run ends
- **THEN** the previous content is in the rotated file and the update log holds only the new lines
