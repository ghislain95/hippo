#!/bin/bash
# Static check of the compose files and of the serve configuration (openspec change deploy-nas,
# design D6). Run by `make check-infra`, so by the CI `infra` job too.
#
# Each compose file is rendered with
#   docker compose --env-file /dev/null -f <file> config --no-interpolate --format json
# in an emptied environment (env -i, only PATH and HOME kept): --env-file replaces the default
# .env of the project folder, which is never loaded; --no-interpolate keeps every ${...} as
# written, so no value of the shell nor of a secret file enters the rendering. The rendering is
# never printed: only the messages below are.
#
# Then it asserts the invariants listed in infra/CLAUDE.md, one message per failure, and exits 1
# if any fails:
# - both files: bounded json-file log rotation on every service; no privileged service, no host
#   network, pid, ipc or user namespace, no env_file, no Docker socket mount, cap_add on the
#   tailscale sidecar only; a mem_limit on hippo (ADR-0038); the hippo environment matches
#   .env.example (every uncommented variable reaches hippo through ${NAME...}, and the reverse),
#   minus COMPOSE_ONLY_VARS, which never reach hippo, plus the variables each compose sets
#   itself; hippo mounts a volume on /backups, the backup folder (change nightly-backup);
# - production (docker-compose.yml): exactly the services hippo and tailscale, no published port,
#   hippo on 127.0.0.1:8080 in the network of the sidecar and restarted with it; sidecar in
#   kernel mode (TS_USERSPACE=false, no userspace-networking in its extra arguments) with exactly
#   NET_ADMIN and /dev/net/tun, TS_AUTH_ONCE=true, serve configuration read from
#   ./tailscale/config/serve.json; Tailscale image pinned to an exact version, hippo image tag
#   required (HIPPO_IMAGE_TAG); the /backups volume of hippo read from ${BACKUP_HOST_DIR:?...},
#   required, so that a missing line stops `docker compose up` instead of backing up into the
#   project folder, and never creating its host folder (bind.create_host_path: false);
# - local (docker-compose.local.yml): ports published on 127.0.0.1 only;
# - tailscale/config/serve.json: HTTPS on port 443 only, a single handler proxying to
#   http://127.0.0.1:8080, Funnel off everywhere.
#
# Usage: infra/check-compose.sh
# COMPOSE_DIR: folder holding docker-compose.yml, docker-compose.local.yml, .env.example and
#   tailscale/config/serve.json (default: the folder of this script). Points the check at a
#   scratch copy, to see an assertion fail: make check-infra COMPOSE_DIR=/path/to/copy
# Needs: docker compose v2, jq.
set -euo pipefail

COMPOSE_DIR=${COMPOSE_DIR:-$(cd "$(dirname "$0")" && pwd)}
PROD=docker-compose.yml
LOCAL=docker-compose.local.yml
EXAMPLE=.env.example
SERVE=tailscale/config/serve.json

# Variables of .env.example read by Compose itself, not by the backend: they never reach hippo.
COMPOSE_ONLY_VARS='["TS_AUTHKEY", "HIPPO_IMAGE_TAG", "BACKUP_HOST_DIR"]'
# Variables each compose sets on hippo itself, absent from .env.example on purpose.
PROD_SET_VARS='{"HTTP_ADDR": "127.0.0.1:8080"}'
LOCAL_SET_VARS='{}'

for f in "$PROD" "$LOCAL" "$EXAMPLE" "$SERVE"; do
  [ -f "$COMPOSE_DIR/$f" ] || { echo "check-compose: $COMPOSE_DIR/$f not found." >&2; exit 1; }
done

# render <file>: the configuration of <file>, uninterpolated, as JSON. Fails on a rendering
# error and on an empty rendering (jq reads an empty input as no value and asserts nothing).
render() {
  local json
  if ! json=$(env -i PATH="$PATH" HOME="$HOME" \
    docker compose --env-file /dev/null -f "$COMPOSE_DIR/$1" config --no-interpolate --format json); then
    echo "check-compose: docker compose cannot render $1." >&2
    return 1
  fi
  if [ -z "$json" ]; then
    echo "check-compose: docker compose rendered $1 as an empty output." >&2
    return 1
  fi
  printf '%s' "$json"
}

# Uncommented variable names of .env.example, as a JSON array.
EXAMPLE_NAMES=$(grep -oE '^[A-Z_][A-Z0-9_]*=' "$COMPOSE_DIR/$EXAMPLE" | tr -d '=' || true)
if [ -z "$EXAMPLE_NAMES" ]; then
  echo "check-compose: $EXAMPLE holds no uncommented NAME=value line: the environment of hippo cannot be checked." >&2
  exit 1
fi
EXAMPLE_VARS=$(printf '%s\n' "$EXAMPLE_NAMES" | jq -R . | jq -sc .)

# Assertions shared by both files. Each emits one message per failure.
COMMON='
def envmap:
  if type == "array" then
    map(capture("^(?<k>[^=]+)(=(?<v>.*))?$") | {(.k): .v}) | add // {}
  else . // {} end;
def services: .services // {} | to_entries[];
def hippo: .services.hippo // {};
def hippo_env: hippo.environment | envmap;
def mounts: (.value.volumes // [])[]
  | if type == "string" then . else "\(.source // "") \(.target // "")" end;
# The volumes of hippo mounted on /backups (a short-form string becomes {source, target}).
def backup_mounts: (hippo.volumes // [])[]
  | if type == "string" then (split(":") | {source: .[0], target: .[1]}) else . end
  | select(.target == "/backups");

def common:
  ( services
    | select((.value.logging.driver // "") != "json-file"
        or (.value.logging.options["max-size"] // "") == ""
        or (.value.logging.options["max-file"] // "") == "")
    | "service \(.key): no bounded log rotation (logging: json-file with max-size and max-file)" ),
  ( services | select(.value.privileged == true)
    | "service \(.key): privileged is forbidden" ),
  ( services | select(.value.network_mode == "host")
    | "service \(.key): the host network is forbidden" ),
  ( services | .key as $s | .value | ("pid", "ipc", "userns_mode") as $k
    | select(.[$k] == "host")
    | "service \($s): \($k): host is forbidden" ),
  ( services | select(.key != "tailscale" and ((.value.cap_add // []) | length > 0))
    | "service \(.key): cap_add is allowed on the tailscale sidecar only" ),
  ( services | select(.value | has("env_file"))
    | "service \(.key): env_file is forbidden (variables are listed one by one, so that this check sees them)" ),
  ( services | .key as $s | select([mounts | select(contains("docker.sock"))] | length > 0)
    | "service \($s): mounting the Docker socket is forbidden" ),
  ( if (hippo.mem_limit // "") == "" then "hippo: no mem_limit (ADR-0038)" else empty end ),
  ( if ([backup_mounts] | length) == 0 then
      "hippo: no volume on /backups (the backup folder, BACKUP_HOST_DIR)" else empty end ),
  ( ($example - $compose_only) as $documented
    | hippo_env as $env
    | ( $documented[] | . as $n
        | if ($env | has($n)) | not then
            "hippo: \($n) of .env.example does not reach the container (missing from environment)"
          elif (($env[$n] // "") | test("^\\$\\{" + $n + "([:}?-]|$)")) | not then
            "hippo: \($n) is set without reading ${\($n)} from .env"
          else empty end ),
      ( $set | keys[] | . as $n
        | if ($env | has($n)) | not then
            "hippo: \($n) must be set by the compose itself"
          elif $env[$n] != $set[$n] then
            "hippo: \($n) must be \($set[$n])"
          else empty end ),
      ( $env | keys[] | . as $n | select(($compose_only | index($n)) != null)
        | "hippo: \($n) is read by Compose only and must not reach the container" ),
      ( $env | keys[] | . as $n
        | select((($documented + ($set | keys) + $compose_only) | index($n)) == null)
        | "hippo: \($n) is not documented in .env.example (or in this script'"'"'s lists)" ) );
'

PROD_CHECKS='
def tailscale: .services.tailscale // {};
def ts_env: tailscale.environment | envmap;
def device_paths: [(tailscale.devices // [])[] | if type == "string" then split(":")[0] else .source end];
def config_mount: (tailscale.volumes // [])[]
  | if type == "string" then
      (split(":") | {source: .[0], target: .[1]})
    else . end
  | select(.target == "/config");

common,
( if (.services // {} | keys) != ["hippo", "tailscale"] then
    "services must be exactly hippo and tailscale" else empty end ),
( services | select((.value.ports // []) | length > 0)
  | "service \(.key): publishes a port; production exposes nothing but tailscale serve (ADR-0015)" ),
( if hippo.network_mode != "service:tailscale" then
    "hippo: network_mode must be service:tailscale" else empty end ),
( if hippo.depends_on.tailscale.restart != true then
    "hippo: depends_on.tailscale.restart must be true (hippo lives in the network of the sidecar)" else empty end ),
( if (tailscale.cap_add // []) != ["NET_ADMIN"] then
    "tailscale: cap_add must be exactly [NET_ADMIN]" else empty end ),
( if device_paths != ["/dev/net/tun"] then
    "tailscale: devices must be exactly /dev/net/tun" else empty end ),
( if (ts_env.TS_USERSPACE // "") != "false" then
    "tailscale: TS_USERSPACE must be false (kernel mode; userspace would expose :8080 to the tailnet, design D1)" else empty end ),
( ("TS_EXTRA_ARGS", "TS_TAILSCALED_EXTRA_ARGS") as $k
  | select((ts_env[$k] // "") | contains("userspace-networking"))
  | "tailscale: \($k) must not ask for userspace-networking (kernel mode, design D1)" ),
( if (ts_env.TS_AUTH_ONCE // "") != "true" then
    "tailscale: TS_AUTH_ONCE must be true (the auth key enrols the node once, design D2)" else empty end ),
( if (ts_env.TS_SERVE_CONFIG // "") != "/config/serve.json"
     or ([config_mount | select((.source // "") | endswith("/tailscale/config"))] | length) != 1 then
    "tailscale: the serve configuration must be ./tailscale/config/serve.json (TS_SERVE_CONFIG=/config/serve.json, ./tailscale/config on /config)" else empty end ),
( if ((tailscale.image // "") | test("^tailscale/tailscale:v?[0-9]+\\.[0-9]+\\.[0-9]+(@sha256:[0-9a-f]{64})?$")) | not then
    "tailscale: image must be pinned to an exact version (tailscale/tailscale:vX.Y.Z), never latest" else empty end ),
( if ((hippo.image // "") | startswith("ghcr.io/ghislain95/hippo:${HIPPO_IMAGE_TAG:?")) | not then
    "hippo: image must be ghcr.io/ghislain95/hippo:${HIPPO_IMAGE_TAG:?...} (tag required, never a silent latest)" else empty end ),
( if [backup_mounts] != [] and ([backup_mounts | select((.source // "") | contains("${BACKUP_HOST_DIR:?") | not)] | length) > 0 then
    "hippo: the /backups volume must read ${BACKUP_HOST_DIR:?...} (required on the NAS, never a default in the project folder)" else empty end ),
( if [backup_mounts] != [] and ([backup_mounts | select(.bind.create_host_path != false)] | length) > 0 then
    "hippo: the /backups volume must not create its host folder (bind: create_host_path: false; the folder is created beforehand, owned by 65532)" else empty end )
'

LOCAL_CHECKS='
common,
( services | .key as $s | (.value.ports // [])[]
  | select((if type == "object" then .host_ip else (tostring | split(":")[0]) end) != "127.0.0.1")
  | "service \($s): publishes a port outside 127.0.0.1" )
'

# Assertions on serve.json, read as written (no interpolation: ${TS_CERT_DOMAIN} stays as is).
SERVE_CHECKS='
( if (keys - ["TCP", "Web", "AllowFunnel"]) != [] then
    "only TCP, Web and AllowFunnel are allowed (no other serve block)" else empty end ),
( if (.TCP // {}) != {"443": {"HTTPS": true}}
     or ([(.Web // {}) | keys[] | select(endswith(":443") | not)] | length) > 0 then
    "only port 443 in HTTPS is served (TCP must be {\"443\": {\"HTTPS\": true}}, every Web host on :443)" else empty end ),
( if [(.Web // {})[] | (.Handlers // {})[]] != [{"Proxy": "http://127.0.0.1:8080"}] then
    "exactly one handler, {\"Proxy\": \"http://127.0.0.1:8080\"}" else empty end ),
( if [(.AllowFunnel // {})[] | select(. != false)] != [] then
    "every AllowFunnel value must be false (no Funnel, ADR-0015)" else empty end )
'

# report <label> <messages>: prints "<label>: <message>" for each failure, returns 1 if any.
report() {
  [ -z "$2" ] && return 0
  printf '%s\n' "$2" | sed "s|^|$1: |"
  return 1
}

# check <file> <checks> <set vars>: the assertions of <checks> on the rendering of <file>.
# A rendering or jq error fails too: set -e does not apply in the `|| status=1` context below,
# and empty messages must never mean a silent pass.
check() {
  local json messages
  json=$(render "$1") || return 1
  if ! messages=$(printf '%s' "$json" | jq -r \
    --argjson example "$EXAMPLE_VARS" \
    --argjson compose_only "$COMPOSE_ONLY_VARS" \
    --argjson set "$3" \
    "$COMMON $2"); then
    echo "check-compose: the assertions cannot run on $1 (jq error)." >&2
    return 1
  fi
  report "$1" "$messages"
}

# check_serve: the assertions on serve.json. An empty file is not valid JSON for jq -e.
check_serve() {
  local messages
  if ! jq -e 'type == "object"' "$COMPOSE_DIR/$SERVE" >/dev/null 2>&1; then
    echo "check-compose: $SERVE is not a JSON object." >&2
    return 1
  fi
  if ! messages=$(jq -r "$SERVE_CHECKS" "$COMPOSE_DIR/$SERVE"); then
    echo "check-compose: the assertions cannot run on $SERVE (jq error)." >&2
    return 1
  fi
  report "$SERVE" "$messages"
}

status=0
check "$PROD" "$PROD_CHECKS" "$PROD_SET_VARS" || status=1
check "$LOCAL" "$LOCAL_CHECKS" "$LOCAL_SET_VARS" || status=1
check_serve || status=1

if [ "$status" -eq 0 ]; then
  echo "check-compose: $PROD, $LOCAL and $SERVE meet their invariants."
else
  echo "check-compose: invariants broken (see infra/CLAUDE.md)." >&2
fi
exit "$status"
