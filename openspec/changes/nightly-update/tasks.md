# Tasks

## 1. Publication of the image and its files

- [ ] 1.1 `.github/workflows/image.yml`: add `infra/docker-compose.yml`, `infra/tailscale/config/serve.json`, `infra/.env.example` and `infra/hippo-update.sh` to `paths`; restrict the job to `refs/heads/main` (D2); update its header comment (the NAS pulls by itself, RG-99)
- [ ] 1.2 `actionlint` green (`make check-tooling`)

## 2. Test harness and failing tests

- [ ] 2.1 `infra/hippo-update.test.sh`: temporary project folder, fakes of `docker`, `curl` and `flock` on `PATH`, call log, helpers (D11)
- [ ] 2.2 Tests of « A pinned image tag pauses the automatic update » (4 scenarios)
- [ ] 2.3 Test of « One run at a time »
- [ ] 2.4 Tests of « The deployment files come from the commit of the published image » (4 scenarios)
- [ ] 2.5 Tests of « Nothing new means nothing happens » (2 scenarios)
- [ ] 2.6 Tests of « Files that do not fit the environment are refused » (3 scenarios)
- [ ] 2.7 Tests of « The running version is backed up before anything changes » (3 scenarios)
- [ ] 2.8 Test of « A run stopped before the update leaves the local latest tag as it was »
- [ ] 2.9 Tests of « An update replaces the files, restarts and checks the health » (3 scenarios)
- [ ] 2.10 Tests of « The update script replaces itself only after a successful update » (2 scenarios)
- [ ] 2.11 Test of « An interrupted run is reported »
- [ ] 2.12 Tests of « Each run writes one bounded log line, never a secret » (3 scenarios)

## 3. Update script

- [ ] 3.1 `infra/hippo-update.sh`: lock, marker, `.env` reading, hand brake, log and rotation (D1, D3 steps 1 to 3, D5, D7, D10)
- [ ] 3.2 Pull, revision, fetch at the revision, nothing-new check, restore of the local `latest` tag (D2, D3 steps 4 to 8, D4)
- [ ] 3.3 Variables and `config --quiet`, pull of the other services, backup by `exec -T` (D3 steps 9 to 11)
- [ ] 3.4 Install by rename with `update/previous/`, `up -d`, health, self-replacement (D3 steps 12 to 15, D6, D8)
- [ ] 3.5 Every test of group 2 green; script portable to the Mac's bash 3.2 (D11)
- [ ] 3.6 `Makefile` `check-infra`: shellcheck of both files and the test suite; `infra/CLAUDE.md` (files, commands, the script's rules)

## 4. Decision and business rule

- [ ] 4.1 ADR-0040 « Mise à jour automatique de nuit sur le NAS » (`/new-adr`): amends ADR-0019, records proposal decisions 1 to 6 and the accepted risks; ADR-0019 status and the ADR index
- [ ] 4.2 `docs/metier.md`: RG-99 as worded in the proposal, cross-reference from RG-98
- [ ] 4.3 `docs/glossary.md`: « Mise à jour automatique », « Frein à main », « Journal de mise à jour »

## 5. Runbook and deployment docs

- [ ] 5.1 `docs/runbook-nas.md`: new section « Mise à jour automatique » (install the script and the cron line with ready commands, check `flock`, the time zone and `cron`, enable with `HIPPO_IMAGE_TAG=latest`, read the log, pause, check after a UGOS update, prune old images); sections 2, 7, 8 and 10 aligned (D12)
- [ ] 5.2 Comments of `infra/.env.example` (`HIPPO_IMAGE_TAG`) and of the `infra/docker-compose.yml` header; `docs/spec-v1.md` « Déploiement »
- [ ] 5.3 `docs/backlog.md`: story E6-05 and the idea « état de la mise à jour visible dans l'appli » after V1

## 6. Integration checks

- [ ] 6.1 `make check` green
- [ ] 6.2 `code-reviewer` pass on the uncommitted diff, no 🛑 nor 🔴 left
- [ ] 6.3 `/opsx:verify`

## 7. User QA on the NAS (Ghislain)

Before the merge (the published image `latest` is still `3fb9955`, whose commit has no `infra/hippo-update.sh`):

- [ ] 7.1 Facts: `flock`, time zone `Europe/Paris`, image id semantics, revision label of the published image
- [ ] 7.2 Install the script from the branch and the cron line; a run with the pinned tag logs `paused`
- [ ] 7.3 A run by hand with `HIPPO_IMAGE_TAG=latest` logs `failed`/`fetch_failed` (no script at `3fb9955`), replaces no file and leaves Hippo running: the early path on the real NAS

After the merge (needs Ghislain's agreement to merge before the end of the QA, D13):

- [ ] 7.4 The first night updates to the merge commit: `updated`, a backup in `Sauvegardes/hippo`, `/healthz` with the new version
- [ ] 7.5 The next night, nothing new: `up-to-date`
- [ ] 7.6 Reboot of the NAS: the cron line is still there; results written in « QA results » of `design.md` (follow-up PR if the QA finds a defect)

## 8. Done

- [ ] 8.1 /dod-check passes (make check green, definition of done in docs/dev/workflow.md)
