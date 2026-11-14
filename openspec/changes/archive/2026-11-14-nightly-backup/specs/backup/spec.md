# Spec Delta

## Purpose

Keeps a dated copy of Hippo's database and images on the NAS, every night and on demand with `hippo backup`, so that a migration, a bug or a mistake can be undone by restoring it. Nothing of it goes through the API: its observable behaviour is the content of the backup folder, the database left unchanged, the exit code and output of `hippo backup`, and its log line.

Terms used below: the **backup folder** is the folder named by `BACKUP_DIR` (`/backups` in the container, `Sauvegardes/hippo` on the NAS); a **backup** is one folder inside it, named after the Paris time at which it started, in the form `YYYY-MM-DDTHHMMSS±hhmm` (the **backup format**, for example `2026-11-13T030000+0100`); the **data folder** is `DATA_DIR`, the **images folder** is `DATA_DIR/assets`. Every scenario runs on a real SQLite database with every migration applied, in a temporary folder.

## ADDED Requirements

### Requirement: A backup holds the whole database and the images
A successful backup MUST be a folder of the backup folder, named in the backup format after the Paris time at which it started, holding `hippo.db` (a consistent copy of the database, every user's items, private ones included), `assets/` (each committed image file at its relative path, byte for byte) and `manifest.json`. Files MUST be mode `0640`, folders `0750` (RG-98, RG-12, RG-92, ADR-0020, ADR-0039).

#### Scenario: A backup holds the database and the images
- **GIVEN** a data folder with two items, one of them an image
- **WHEN** a backup runs
- **THEN** the backup folder holds one new backup with `hippo.db`, `assets/` and `manifest.json`; its `hippo.db` opens, passes an integrity check and holds both items; each image file of `assets/` is byte for byte the file of the images folder at the same relative path

#### Scenario: A private item is backed up like the others
- **GIVEN** an item marked private, with an image
- **WHEN** a backup runs
- **THEN** the backup's `hippo.db` holds the private item and its `assets/` holds its image

#### Scenario: The backup name is the Paris time with its offset
- **GIVEN** `TZ` set to `UTC`
- **WHEN** one backup starts at 01:00:00 UTC on 1 July 2026 and another at 02:00:00 UTC on 13 November 2026
- **THEN** they are named `2026-07-01T030000+0200` and `2026-11-13T030000+0100`

#### Scenario: Backup files are not readable by others
- **WHEN** a backup runs
- **THEN** every file of the backup has mode `0640` and every folder, the backup itself included, mode `0750`

#### Scenario: A write in progress is not in the backup
- **GIVEN** the service holds an uncommitted write transaction that adds an item
- **WHEN** a backup runs, then the transaction is rolled back
- **THEN** the backup's `hippo.db` passes an integrity check and holds the committed items only

### Requirement: A backup never writes the database nor cleans the data folder
A backup MUST NOT write the database, apply a migration, nor remove or change any file of the data folder: not the `.tmp-*` files of the images folder, not the files left in `DATA_DIR/tmp`. The `.tmp-*` files MUST NOT be copied (RG-98, ADR-0020, ADR-0032).

#### Scenario: The database is left unchanged
- **GIVEN** the service has the database open, with committed writes still in its write-ahead log
- **WHEN** a backup runs
- **THEN** the database file and its write-ahead log are byte for byte what they were before the backup

#### Scenario: A database behind the binary is not migrated
- **GIVEN** a database whose last applied migration is older than the last one the binary knows
- **WHEN** a backup runs
- **THEN** the database still has the same last applied migration, and the backup's manifest names that migration

#### Scenario: Temporary files stay in place and out of the backup
- **GIVEN** a `.tmp-` file in the images folder and a file left in `DATA_DIR/tmp`
- **WHEN** a backup runs
- **THEN** both files are still there, unchanged, and the backup holds no `.tmp-` file and nothing of `DATA_DIR/tmp`

#### Scenario: A symbolic link in the images folder is not followed
- **GIVEN** a symbolic link in the images folder that points to a file outside it
- **WHEN** a backup runs
- **THEN** the backup succeeds and holds neither the link nor the content of its target

### Requirement: An unchanged image is linked from the previous backup, never from the data folder
An image whose relative path, size and modification time equal those of its copy in the most recent backup MUST be a hard link to that copy; otherwise, or when the link fails, it MUST be copied, keeping its modification time. A copy MUST be a separate file, never a link nor a shared extent of the images folder (RG-98, ADR-0039).

#### Scenario: An unchanged image is linked from the previous backup
- **GIVEN** a backup already holds an image, unchanged since
- **WHEN** a new backup runs
- **THEN** the image in the new backup is the same file as in the previous one (same device and inode), and the manifest counts it as linked

#### Scenario: A changed image is copied
- **GIVEN** a backup already holds an image, and the image's modification time has changed since
- **WHEN** a new backup runs
- **THEN** the image in the new backup is a separate file with the new content and modification time, and the manifest counts it as copied

#### Scenario: A backup image is never the data folder's file
- **WHEN** a backup runs, then an image of the images folder is rewritten in place
- **THEN** the backup's copy is a separate file from the images folder's (another inode) and still holds the original bytes

#### Scenario: A failed link falls back to a copy
- **GIVEN** a backup already holds an unchanged image, and creating a hard link fails
- **WHEN** a new backup runs
- **THEN** the backup succeeds, its copy of the image is a separate file with the same bytes, and the manifest counts it as copied

### Requirement: An image gone during the backup is counted, not an error
An image file that disappears between the copy of the database and its own copy MUST be left out of the backup and counted as gone; the backup MUST still succeed (RG-98, RG-79).

#### Scenario: An image removed during the backup is counted as gone
- **GIVEN** two images, one of which is removed after the database is copied and before the images are
- **WHEN** the backup completes
- **THEN** it succeeds, holds the other image, and its manifest and log line count one image gone

### Requirement: A backup appears under its final name only when complete
A backup MUST be written in a temporary folder `.partial-<name>` of the backup folder, synced (files and folders), then renamed to its final name. A name already taken MUST fail the backup. Any failure or stop MUST remove its temporary folder; a temporary folder left by a crash MUST be removed by the next backup (RG-98, ADR-0039).

#### Scenario: A backup in progress is not visible under its final name
- **WHEN** a backup is between the copy of its database and the copy of its images
- **THEN** the backup folder holds `.partial-<name>` and no folder `<name>`

#### Scenario: A temporary folder left by a crash is removed by the next backup
- **GIVEN** the backup folder holds a `.partial-2026-11-12T030000+0100` folder, a file `notes.txt` and a folder `old-copy`
- **WHEN** a backup runs
- **THEN** the `.partial-` folder is gone, the new backup is complete, `notes.txt` and `old-copy` are unchanged, and the log line counts one leftover removed

#### Scenario: A name already taken fails the backup
- **GIVEN** the backup folder already holds a folder with the name the backup would take
- **WHEN** the backup runs
- **THEN** it fails with outcome `name_taken`, the existing folder is unchanged and no `.partial-` folder is left

#### Scenario: A failed write removes the temporary folder
- **GIVEN** the previous backups, and writes to the backup folder that start failing after the database is copied
- **WHEN** the backup runs
- **THEN** it fails with outcome `failed`, no new backup and no `.partial-` folder is left, and every previous backup is still there

#### Scenario: A stop during the backup removes the temporary folder
- **WHEN** the service is asked to stop while a backup copies its images
- **THEN** the backup ends with outcome `stopped`, no new backup and no `.partial-` folder is left

### Requirement: A backup that cannot fit does not start
Before writing anything, a backup MUST check that the file system of the backup folder has room for the database, the images it will copy (not those it will link) and a margin of 64 MiB; otherwise it MUST fail with outcome `no_room` and write nothing (RG-98, ADR-0039).

#### Scenario: A backup that cannot fit does not start
- **GIVEN** free space below the database size plus the images to copy plus 64 MiB
- **WHEN** a backup runs
- **THEN** it fails with outcome `no_room`, and the backup folder holds no new backup and no `.partial-` folder

#### Scenario: Linked images do not count toward the space needed
- **GIVEN** a previous backup holding every image unchanged, and free space for the database plus 64 MiB but not for the images
- **WHEN** a backup runs
- **THEN** it succeeds and every image is linked

### Requirement: One backup runs at a time, across processes
A backup MUST hold an exclusive lock on the file `.lock` of the backup folder for its whole run, released when its process ends, crash included. The nightly backup that finds the lock taken MUST be skipped; `hippo backup` MUST wait for the lock up to 10 minutes, then fail (RG-98, ADR-0039).

#### Scenario: The nightly backup is skipped when another backup holds the lock
- **GIVEN** another process holds the lock of the backup folder
- **WHEN** the nightly backup time comes
- **THEN** no backup is written, the log line has outcome `skipped`, and the next night's backup runs as usual

#### Scenario: The backup command waits for the lock, then fails
- **GIVEN** another process holds the lock for longer than the command's wait
- **WHEN** `hippo backup` runs
- **THEN** it exits non-zero after the wait, with outcome `locked` on its output, and no backup is written

#### Scenario: The backup command proceeds once the lock is released
- **GIVEN** another process holds the lock, and releases it during the command's wait
- **WHEN** `hippo backup` runs
- **THEN** it makes its backup and exits zero

#### Scenario: The lock of a killed process is released
- **GIVEN** a process that took the lock is killed
- **WHEN** a backup runs
- **THEN** it takes the lock at once and succeeds

### Requirement: Backups older than 14 days are removed, the 3 most recent kept
After a successful backup, and only then, every backup older than 14 days, by its name, MUST be removed, except the 3 most recent and the one just made. Only folders named in the backup format MUST ever be removed (RG-98, ADR-0020, ADR-0039).

#### Scenario: Backups older than 14 days are removed after a successful backup
- **GIVEN** backups made 20, 15, 13, 2 and 1 days before
- **WHEN** a backup succeeds
- **THEN** the backups of 20 and 15 days are removed, the others and the new one are kept, and the log line counts 2 removed

#### Scenario: The 3 most recent backups are kept whatever their age
- **GIVEN** only backups made 33, 32, 31 and 30 days before (the NAS was off for a month)
- **WHEN** a backup succeeds
- **THEN** the backups of 31 and 30 days and the new one are kept, and the two others are removed

#### Scenario: Only folders in the backup format are touched
- **GIVEN** in the backup folder, all dated more than 14 days back: a file `notes.txt`, a folder `old-copy`, a folder `2026-01-01`, a file named in the backup format
- **WHEN** a backup succeeds
- **THEN** all four are unchanged

### Requirement: The manifest describes the backup
`manifest.json` MUST hold the format version of the manifest, the backup name, its start time, its trigger (`nightly` or `command`), the backend version, the last database migration applied, the size of the database copy, the number of images and of images copied, linked and gone, and the bytes of images written. It MUST NOT hold a file name nor any item content (RG-98, ADR-0039).

#### Scenario: The manifest describes the backup
- **GIVEN** backend version `abc1234`, two images of which one is unchanged since the previous backup
- **WHEN** a backup runs from `hippo backup`
- **THEN** its manifest has trigger `command`, backend version `abc1234`, the last migration of the database, the database copy's size, 2 images, 1 copied, 1 linked, 0 gone, and the copied image's bytes; it holds no image path and no item title

### Requirement: Each backup writes one log line without file names nor content
Each backup, successful or not, MUST write one JSON log line `backup` with its outcome, trigger, name, backend version, last migration, database size, images copied, linked and gone, bytes written, duration, backups and leftovers removed. It MUST NOT hold an image path nor any item content (RG-98, ADR-0039).

#### Scenario: One log line per backup, without file names nor content
- **GIVEN** an item with a title and an image
- **WHEN** a backup runs
- **THEN** exactly one log line `backup` is written, with outcome `ok` and every listed field, and no log line holds the image's path, its file name nor the item's title

### Requirement: `hippo backup` makes one backup in the running service and reports it
`hippo backup`, run in the running container, MUST make one backup with the configuration of the service, print its log line on its standard output, and exit zero only when a complete backup exists; any failure MUST exit non-zero (RG-98, ADR-0039).

#### Scenario: The command makes a backup while the service runs
- **GIVEN** the service is running on its data folder
- **WHEN** `hippo backup` runs
- **THEN** it exits zero, prints one line with outcome `ok` and the backup's name, and that backup is complete

#### Scenario: The command exits non-zero when the backup fails
- **GIVEN** the backup folder has disappeared since the service started
- **WHEN** `hippo backup` runs
- **THEN** it exits non-zero, prints a line with outcome `failed`, writes nothing, and its error message names no file

### Requirement: The nightly backup runs at BACKUP_SCHEDULE in Paris time
The service MUST run one backup per Paris date at `BACKUP_SCHEDULE` (default `0 3 * * *`), read as Paris wall-clock time whatever `TZ` says, with the clock-change rules of the nightly batch; a night missed while the service was stopped or asleep MUST NOT be caught up. Only the daily form `M H * * *` MUST be accepted: any other form refuses the start-up (RG-30, RG-98, ADR-0039).

#### Scenario: The nightly backup runs at the scheduled Paris time
- **GIVEN** `BACKUP_SCHEDULE` `0 3 * * *` and `TZ` set to `UTC`
- **WHEN** the clock reaches 03:00 in Paris on 13 November 2026 (02:00 UTC)
- **THEN** one backup named `2026-11-13T030000+0100` with trigger `nightly` is written, and none before that instant

#### Scenario: A missed night is not caught up
- **GIVEN** `BACKUP_SCHEDULE` `0 3 * * *`
- **WHEN** the service wakes at 09:00 Paris time after sleeping through 03:00
- **THEN** no backup is written before 03:00 the next night

#### Scenario: A schedule that is not daily refuses the start-up
- **GIVEN** `BACKUP_SCHEDULE` `0 3 * * 1`
- **WHEN** the service starts
- **THEN** it refuses to start with an error that names `BACKUP_SCHEDULE`

### Requirement: The service starts only with a usable backup folder
The service MUST refuse to start when the backup folder does not exist, is not a folder, cannot be written, or is the data folder, inside it or around it (RG-98, ADR-0039).

#### Scenario: The service refuses to start without a writable backup folder
- **GIVEN** `BACKUP_DIR` names a folder that does not exist
- **WHEN** the service starts
- **THEN** it refuses to start with an error about the backup folder

#### Scenario: A backup folder inside the data folder is refused
- **GIVEN** `BACKUP_DIR` names a folder inside `DATA_DIR`
- **WHEN** the service starts
- **THEN** it refuses to start with an error that names `BACKUP_DIR`

### Requirement: A backup restores to a working Hippo
Copying a backup's `hippo.db` and `assets/` into an empty data folder MUST give, at the next start of the same backend version, the items, private ones included, and the images of the backup (RG-98, ADR-0020).

#### Scenario: A restored backup serves the items and images as before
- **GIVEN** a backup of a data folder with a public item, a private item and an image
- **WHEN** its `hippo.db` and `assets/` are copied into an empty data folder and the service starts on it
- **THEN** the service starts, and both items and the image are served as they were when the backup was made
