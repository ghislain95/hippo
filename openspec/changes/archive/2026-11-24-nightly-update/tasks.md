# Tasks

## 1. Publication of the image and its files

- [x] 1.1 `.github/workflows/image.yml`: add `infra/docker-compose.yml`, `infra/tailscale/config/serve.json`, `infra/.env.example` and `infra/hippo-update.sh` to `paths`; restrict the job to `refs/heads/main` (D2); update its header comment (the NAS pulls by itself, RG-99)
- [x] 1.2 `actionlint` green (`make check-tooling`)

## 2. Test harness and failing tests

- [x] 2.1 `infra/hippo-update.test.sh`: temporary project folder, fakes of `docker`, `curl` and `flock` on `PATH`, call log, helpers (D11)
- [x] 2.2 Tests of « A pinned image tag pauses the automatic update » (4 scenarios)
- [x] 2.3 Test of « One run at a time »
- [x] 2.4 Tests of « The deployment files come from the commit of the published image » (4 scenarios)
- [x] 2.5 Tests of « Nothing new means nothing happens » (2 scenarios)
- [x] 2.6 Tests of « Files that do not fit the environment are refused » (3 scenarios)
- [x] 2.7 Tests of « The running version is backed up before anything changes » (3 scenarios)
- [x] 2.8 Test of « A run stopped before the update leaves the local latest tag as it was »
- [x] 2.9 Tests of « An update replaces the files, restarts and checks the health » (3 scenarios)
- [x] 2.10 Tests of « The update script replaces itself only after a successful update » (2 scenarios)
- [x] 2.11 Test of « An interrupted run is reported »
- [x] 2.12 Tests of « Each run writes one bounded log line, never a secret » (3 scenarios)

## 3. Update script

- [x] 3.1 `infra/hippo-update.sh`: lock, marker, `.env` reading, hand brake, log and rotation (D1, D3 steps 1 to 3, D5, D7, D10)
- [x] 3.2 Pull, revision, fetch at the revision, nothing-new check, restore of the local `latest` tag (D2, D3 steps 4 to 8, D4)
- [x] 3.3 Variables and `config --quiet`, pull of the other services, backup by `exec -T` (D3 steps 9 to 11)
- [x] 3.4 Install by rename with `previous/` (in the state folder since D16), `up -d`, health, self-replacement (D3 steps 12 to 15, D6, D8)
- [x] 3.5 Every test of group 2 green; script portable to the Mac's bash 3.2 (D11)
- [x] 3.6 `Makefile` `check-infra`: shellcheck of both files and the test suite; `infra/CLAUDE.md` (files, commands, the script's rules)
- [x] 3.7 Script outside the project folder and ownership check (D15): `/usr/local/sbin/hippo-update <project folder>`, argument mandatory and absolute, every checked path root's alone (no symbolic link, owner root, no group nor other write, no ACL) before any write and any `docker`; self-replacement of its own file; tests of « The project folder and the deployment files belong to root alone » (4 scenarios) and the existing ones moved to the argument; `infra/CLAUDE.md`
- [x] 3.8 State outside the share and project folder reached through its inode (D16, second review): state in `/var/lib/hippo-update` (`HIPPO_UPDATE_STATE_DIR` for the tests), `cd -P` into the project folder then relative paths only; tests « A state folder writable by its group is refused » and « A project folder swapped during the run is not followed », no update script anywhere under the project folder; the reviewer's `race.sh` and `race2.sh` replayed and failing to attack; `infra/CLAUDE.md`
- [x] 3.9 Compose gets copies, never a file of the share (D17, third review): `./docker-compose.yml` and `./.env` copied into `<state>/run/` once the lock is held, every Compose call on those copies or on the fetched compose (`up -d` and the health polls after the install), tag and variable names read from the copy, `run/` removed at the end of every run; the fake `docker compose` resolves its files through `$PWD` after a race hook; « A project folder swapped during the run is not followed » fails with the D16 script and passes; runner order aligned on the spec; `infra/CLAUDE.md`

## 4. Decision and business rule

- [x] 4.1 ADR-0040 « Mise à jour automatique de nuit sur le NAS » (`/new-adr`): amends ADR-0019, records proposal decisions 1 to 6 and the accepted risks; ADR-0019 status and the ADR index
- [x] 4.2 `docs/metier.md`: RG-99 as worded in the proposal, cross-reference from RG-98
- [x] 4.3 `docs/glossary.md`: « Mise à jour automatique », « Frein à main », « Journal de mise à jour »

## 5. Runbook and deployment docs

- [x] 5.1 `docs/runbook-nas.md`: new section « Mise à jour automatique » (install the script and the cron line with ready commands, check `flock`, the time zone and `cron`, enable with `HIPPO_IMAGE_TAG=latest`, read the log, pause, check after a UGOS update, prune old images); sections 2, 7, 8 and 10 aligned (D12)
- [x] 5.2 Comments of `infra/.env.example` (`HIPPO_IMAGE_TAG`) and of the `infra/docker-compose.yml` header; `docs/spec-v1.md` « Déploiement »
- [x] 5.3 `docs/backlog.md`: story E6-05 and the idea « état de la mise à jour visible dans l'appli » after V1

## 6. Integration checks

- [x] 6.1 `make check` green
- [x] 6.2 `code-reviewer` pass on the uncommitted diff, no 🛑 nor 🔴 left
- [x] 6.3 `/opsx:verify`

## 7. User QA on the NAS (Ghislain)

Before the merge (the published image `latest` is still `3fb9955`, whose commit has no `infra/hippo-update.sh`):

- [x] 7.1 Facts: `flock`, time zone at Paris time (`Europe/Paris` or a zone with the same clock), image id semantics, revision label of the published image; owner, mode and ACLs of the project folder and its files once made root's (`ls -ldn`, `getfacl`, D15)
- [x] 7.2 Install the script from the branch and the cron line; a run with the pinned tag logs `paused`
- [x] 7.3 A run by hand with `HIPPO_IMAGE_TAG=latest` logs `failed`/`fetch_failed` (no script at `3fb9955`), replaces no file and leaves Hippo running: the early path on the real NAS

After the merge (Ghislain agreed on 2026-11-24 to merge before it, D13). Not tasks of this change: they run on the NAS once `image.yml` has published the merge commit, and their results go to a follow-up PR (« QA results › After the merge » of the archived `design.md`), with a fix if needed:

- 7.4 The first night updates to the merge commit: `updated`, a backup in `Sauvegardes/hippo`, `/healthz` with the new version
- 7.5 After that first update, the UGOS Docker app still shows the `hippo` project and its « restart project » works (Compose labels point into the state folder, D17); otherwise the runbook's SSH restart is the way
- 7.6 The next night, nothing new: `up-to-date`
- 7.7 Reboot of the NAS: the cron line and the script are still there

## 8. Done

- [ ] 8.1 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
