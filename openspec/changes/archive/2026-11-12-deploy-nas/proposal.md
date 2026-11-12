# Proposal

Backlog stories: **E6-01 — Image et compose**, **E6-02 — HTTPS par Tailscale** and **E6-04 — Exploitation** (decided with Ghislain on 2026-11-07: E6 is split in two changes, `deploy-nas` first, then `nightly-backup` for E6-03).

Rules: RG-12, RG-27, RG-30, RG-81, RG-90, RG-92 (unchanged, deployed as they are) · ADRs: ADR-0013, ADR-0015, ADR-0016, ADR-0019, ADR-0020 (deferred to `nightly-backup`), ADR-0021, ADR-0027, ADR-0030, ADR-0032, ADR-0038.

## Why

E3-01 to E3-05 are merged: the nightly batch sends, follows and applies its results. The next step is to put the Anthropic API key on the NAS, but the NAS (Ugreen DXP4800 Plus) runs nothing yet: no Hippo, no data, no key. Ghislain needs a step-by-step procedure, in French, to install Hippo there and reach it in HTTPS through Tailscale, then to update it and roll it back. The image (GHCR, public, multi-arch) and the production compose exist since the setup; they have never run on the NAS.

## What Changes

- **`docs/runbook-nas.md`** (French), the central deliverable, in the style of `docs/runbook-ios.md`:
  - **prerequisites, checked first**: UGOS Pro and its Docker app, `docker compose version` recent enough for the compose (`depends_on.restart`), `sudo`, `/dev/net/tun` present, SSH enabled for the installation (decided with Ghislain: SSH to install and update, the UGOS Docker interface for day-to-day logs, health and restarts), no scheduled shutdown or sleep around 02:00 (a missed night is not caught up, RG-30); the existing Tailscale account with HTTPS certificates enabled (checked, not changed: enabled during E1); no existing `hippo` node in the tailnet (else the new one becomes `hippo-1`, a name that ends up in the public certificate logs and in the app's build);
  - **placement and permissions**: the project folder in an admin-only shared folder, excluded from any UGOS sync or cloud backup task until `nightly-backup` (the content must not leave the NAS in clear, RG-92, ADR-0020); `infra/.env` in `600`, `tailscale/state` in `700`, `data/` created and owned by uid 65532 **before** the first start (checked with `ls -ln`, UGOS ACLs may override a `chown`);
  - **versioned files fetched at a commit SHA** (`docker-compose.yml`, `tailscale/config/serve.json`, and `.env.example` as the model of `.env`), never at a branch name; the SHA and the image tag are noted in a journal kept outside the repository;
  - **`infra/.env`** from `infra/.env.example`: a new `API_TOKEN` for the NAS (not the Mac's), `TS_AUTHKEY`, the two `ANALYSIS_*_PRICE_USD_PER_MTOK`, `BATCH_SCHEDULE`, `MONTHLY_BUDGET_USD`, `HIPPO_IMAGE_TAG` set to an existing `sha-<commit>` tag (read on the GHCR package page, checked with `docker manifest inspect`); secrets kept in the password manager, never in the repository;
  - **Tailscale**: a dedicated `hippo` node (decided with Ghislain: the compose's sidecar, ADR-0015, not the NAS's own Tailscale), from a one-off, pre-approved, non-ephemeral, untagged auth key (no ACL change), erased from `.env` once the node is enrolled; key expiry disabled on that node (a device setting, not a tailnet setting);
  - **first start in two phases**: phase 1 **without** `ANTHROPIC_API_KEY` (nothing can be sent to the AI; the backend logs a warning) for every check — container health, JSON logs (`hippo started` with the full commit SHA, compared with the tag; `analysis prices`), `https://hippo.<tailnet>.ts.net/healthz` from the iPhone (SideStore's VPN off, ADR-0013; first call slow while the certificate is issued), an authenticated capture with `curl` from the Mac (« Use Tailscale DNS » on), plain HTTP on `:8080` refused from the tailnet, a restart of the project and a reboot of the NAS followed by `/healthz` again; phase 2 adds the API key (dedicated key, prepaid credits without auto-reload, ADR-0021);
  - **update and rollback**: pin `HIPPO_IMAGE_TAG`, `docker compose pull && docker compose up -d`; before every update, check whether the commits between the two SHAs add a migration (`backend/internal/store/migrations/`), and copy `data/` with Hippo stopped (database in WAL: the `-wal` and `-shm` files go with it) to a folder outside `data/`; a rollback past a migration is not refused by goose but silently runs the old binary on the new schema, so it is stop, restore the copy, then put the old tag back; a Tailscale image update (Dependabot, in the repository) reaches the NAS only by fetching the compose again at the new SHA;
  - **day-to-day**: logs, health and restarts in the UGOS Docker interface; **restart the project, never the `tailscale` container alone** (it carries Hippo's network); troubleshooting table;
  - a clear warning that **no backup exists until `nightly-backup`**: nothing precious goes into Hippo before then. Losing `tailscale/state` creates a new node (decided in `nightly-backup`: back it up or not).
- **Production compose hardening** (`infra/docker-compose.yml`), each point justified in the design:
  - the sidecar **stays in kernel mode** (`TS_USERSPACE=false`, `NET_ADMIN` and `/dev/net/tun`, valid only in the container's network namespace): in userspace mode, netstack forwards any tailnet connection to a port without a `serve` handler to `127.0.0.1:<port>`, which would expose `:8080` in clear to the tailnet (adversarial pass, objection 1);
  - `TS_AUTH_ONCE=true` and `TS_AUTHKEY` optional: the key is used once to enrol, then the persisted state is enough; a consumed key no longer has to stay in `.env`;
  - `hippo` restarts with the sidecar (`depends_on` with `restart: true`), since its network lives in the sidecar;
  - `HIPPO_IMAGE_TAG` required (`:?`): a missing line no longer pulls `latest` silently;
  - Tailscale image pinned to an exact version instead of `latest`, followed by Dependabot (`docker-compose` ecosystem, ADR-0030: an update channel, not a new dependency);
  - Docker log rotation (`json-file`, bounded size and count) on both services, so that the logs never fill the NAS volume.
- **Static check of the compose files** in `make check-infra` (and the CI `infra` job), read with `--env-file /dev/null --no-interpolate` so that no value of `infra/.env` is ever loaded: the production compose publishes no port, Hippo listens on `127.0.0.1:8080`, the sidecar has exactly `NET_ADMIN` and `/dev/net/tun`, neither `privileged` nor host network, log rotation on every service, Tailscale image not `latest`; every uncommented variable of `infra/.env.example` reaches the `hippo` service of both compose files and the reverse, with an explicit exclusion list (`TS_AUTHKEY`, `HIPPO_IMAGE_TAG`). One assertion, one message per invariant.
- **`infra/.env.example`**: NAS section rewritten (auth key to create, erase after enrolment, why the NAS gets its own `API_TOKEN`, how to pin the image).
- **Docs**: `docs/spec-v1.md` « Déploiement » corrected (GHCR public per ADR-0027, compose sketch aligned); `infra/CLAUDE.md` lists the invariants that `check-infra` enforces; `docs/CLAUDE.md` and `README.md` link the runbook; `docs/dev/setup-report.md` « À faire » items 6 and 8 updated; backlog E6-01, E6-02, E6-04 marked done once Ghislain has followed the runbook.

## Capabilities

### New Capabilities
- none

### Modified Capabilities
- none

`skip_specs: true`: no observable behaviour of the API changes, and no RG-xx covers the deployment (`openspec/config.yaml` asks for both). The invariants live in `infra/CLAUDE.md`, are enforced by `make check-infra` in CI, and are proven on the NAS by the runbook's checks during the QA.

## Non-goals

- The nightly backup, local and external encrypted, and the restore test (E6-03, change `nightly-backup`); the external service is chosen then.
- Continuous deployment to the NAS: updates stay manual and deliberate (ADR-0019).
- Any change to the backend code, the API contract, the prompt or the fiche schemas.
- The app pointing to the NAS: the app (E5) does not exist yet; the share extension's capture from the iPhone is checked by E5-02. Here, the iPhone checks `/healthz` in Safari and the capture is checked with `curl`.
- Tailscale ACLs, tags or any other tailnet setting; Tailscale on the NAS host itself.
- Monitoring or alerting beyond the Docker healthcheck and the UGOS Docker interface (the healthcheck probes loopback and stays green when the node is offline: known limit, written in the runbook's troubleshooting).
- Disabling tailscaled's own logs to Tailscale (logtail): they carry no item data, RG-92 holds.

## Decision taken with Ghislain (2026-11-07, stop case 5: cost)

**Paid end-to-end test on the NAS: yes.** Once phase 2 is done, capture one fictitious public link and one fictitious image, let them leave in the next nightly batch (under 0.01 $), and check in the morning the logs, the analysed items and the image copy time on amd64 (`spec-v1.md` « reste à mesurer », ADR-0038 measured on arm64). The analysed items are read back by replaying each capture with its `client_id` (RG-08: `200` with the item, its classification and fiche), since `getItem`, `listItems` and `deleteItem` answer `501` until E4; then Hippo starts again from an empty `data/`, so that the fictitious items never reach the library.

## Impact

- **AI cost**: none by itself. Phase 1 runs without the API key, so nothing can be sent. Once Ghislain adds the key, the nightly batch runs for real (about 2 to 4 $ a month at 20 items a day, under `MONTHLY_BUDGET_USD`, RG-81). The end-to-end test above costs under 0.01 $ (agreed by Ghislain).
- **Private items**: none sent; no behaviour change.
- **Persisted data**: the first real database is created on the NAS by the existing migrations. No backup until `nightly-backup`: stated in the runbook, with the copy of `data/` before every update. Nothing personal enters the repository (ADR-0027): the runbook uses placeholders (`<tailnet>`, `<volume>`), never Ghislain's tailnet name, addresses or keys.
- **API contract**: none.
- **Prompt and fiche schemas**: none.
- **Security**: exposure unchanged (HTTPS on the tailnet only, ADR-0015, `:8080` on loopback); the check now enforces it in CI. Secrets live only in the NAS's `infra/.env` (mode `600`) and Ghislain's password manager; the consumed auth key is erased.
- **Dependencies**: no new image or service; the Tailscale image is pinned, and Dependabot gets a `docker-compose` entry.
- **User QA: yes** — Ghislain follows the runbook on the NAS once, up to a running Hippo reachable in HTTPS through Tailscale, before the merge.
