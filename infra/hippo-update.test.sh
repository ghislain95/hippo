#!/bin/bash
# Tests of infra/hippo-update.sh: one test per scenario of the automatic-update capability
# (openspec change nightly-update, design D11), named after it.
# Run: bash infra/hippo-update.test.sh [path/to/hippo-update.sh]
#
# Each test builds a temporary project folder, a copy of the script outside it ($T/sbin/hippo-update,
# as /usr/local/sbin/hippo-update on the NAS; the script replaces its own file: never run the
# original), a state folder outside the project folder ($T/state, as /var/lib/hippo-update on the
# NAS) and a scenario folder ($FAKE_DIR). The script is run with the project folder as its
# argument, HIPPO_UPDATE_STATE_DIR set to the state folder, and HIPPO_UPDATE_OWNER_UID set to the
# uid of the user running the tests: the paths it checks belong to that user, not to root. Fakes of docker, curl, flock and ls are put first on
# PATH: docker and curl answer from the files of $FAKE_DIR and append each call to
# $FAKE_DIR/calls.log; flock fails when $FAKE_DIR/lock_held exists; ls is the real one, except
# that it reports another owner for the path named in $FAKE_DIR/ls_owner (a file of another owner
# without being root). No real Docker, network nor lock is used: the suite runs on the Mac
# (bash 3.2, BSD tools, no flock) and in CI.
#
# Files of $FAKE_DIR read by the fake docker:
#   running_image      image id of the running hippo container (absent: hippo not running)
#   latest_local       image id of the local ghcr.io/ghislain95/hippo:latest tag (absent: none)
#   published_image    image id that `docker pull` puts behind the latest tag
#   labels/<id>        revision label of an image (absent: no label)
#   pull_exit, config_exit, services_pull_exit, backup_exit, up_exit: exit codes (default 0)
#   health             health statuses returned one per poll, the last one repeated; a line
#                      "<status> <image id>" is returned as is (a container still on another image)
#   swap_on_pull       a script run once by `docker pull` (a project folder swapped during the run)
#   on_exec            a script run by `docker compose exec` (a write during the backup)
#   race               a script run by each `docker compose`, as `race in <dir>` between its
#                      Getwd and the opening of its files, and `race out <dir>` after
# `docker compose` behaves as Compose does (checked with strace on Compose v5.7.1): it takes the
# working directory from $PWD (Go os.Getwd; bash, like Go, keeps an inherited $PWD only while it
# still names the working directory), then opens -f and --env-file by absolute path, a relative
# one being joined to that directory. Every file it opens is appended to compose_reads. `up -d`
# makes the container run the image of the latest tag and keeps the compose and .env it read
# (compose_at_up, env_at_up). `config` prints the --env-file it is given, as a real rendering
# would print its values.
set -u
# The checked paths must not be writable by group or others, whatever the caller's umask.
umask 022

SCRIPT=${1:-$(dirname "$0")/hippo-update.sh}
case $SCRIPT in /*) ;; *) SCRIPT=$PWD/$SCRIPT ;; esac
[ -f "$SCRIPT" ] || { echo "hippo-update.test: $SCRIPT not found." >&2; exit 1; }
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

# rep <char> <count>: <char> repeated <count> times.
rep() {
  local s='' i=0
  while [ "$i" -lt "$2" ]; do s=$s$1; i=$((i + 1)); done
  printf '%s' "$s"
}

IMAGE=ghcr.io/ghislain95/hippo:latest
REPO_URL=https://raw.example.test/ghislain95/hippo
OLD_REV=$(rep c 40)
NEW_REV=$(rep a 40)
CRASH_REV=$(rep b 40)
OLD_ID=sha256:$(rep 1 64)
NEW_ID=sha256:$(rep 2 64)
BACKUP=2026-11-25T040000+0100
DEPLOY_FILES="docker-compose.yml tailscale/config/serve.json .env.example"
# One JSON object built by the script: string values, or a number for duration_s.
JSON_LINE='^\{"time":"[^"]+","msg":"update","outcome":"[a-z-]+"(,"[a-z_]+":("[^"]*"|[0-9]+))*\}$'

# --- Fakes -------------------------------------------------------------------------------------

mkdir -p "$ROOT/bin"

cat >"$ROOT/bin/docker" <<'EOF'
#!/bin/bash
# Fake docker: answers from the files of $FAKE_DIR, records each call.
set -u
F=$FAKE_DIR
IMAGE=ghcr.io/ghislain95/hippo:latest
printf 'docker %s\n' "$*" >>"$F/calls.log"
code() { if [ -f "$F/$1" ]; then cat "$F/$1"; else echo 0; fi; }
label_file() { printf '%s/labels/%s' "$F" "$(printf '%s' "$1" | tr ':' '_')"; }
next_health() {
  local n line
  [ -f "$F/health" ] || { echo healthy; return; }
  n=$(cat "$F/health_n" 2>/dev/null || echo 0)
  n=$((n + 1))
  echo "$n" >"$F/health_n"
  line=$(sed -n "${n}p" "$F/health")
  [ -n "$line" ] || line=$(tail -n 1 "$F/health")
  echo "$line"
}
case ${1:-} in
compose)
  shift
  file='' envfile=''
  while [ $# -gt 0 ]; do
    case $1 in
    -f) file=$2; shift 2 ;;
    --env-file) envfile=$2; shift 2 ;;
    --project-directory) shift 2 ;;
    *) break ;;
    esac
  done
  sub=${1:-}
  shift
  getwd=$PWD
  if [ -f "$F/race" ]; then bash "$F/race" in "$getwd"; fi
  case $file in /*) ;; ?*) file=$getwd/${file#./} ;; esac
  case $envfile in /*) ;; ?*) envfile=$getwd/${envfile#./} ;; esac
  # What Compose read, kept before the race puts the folders back.
  cp "$file" "$F/cur-compose" 2>/dev/null || : >"$F/cur-compose"
  if [ -n "$envfile" ]; then cp "$envfile" "$F/cur-env" 2>/dev/null || : >"$F/cur-env"; else : >"$F/cur-env"; fi
  cat "$F/cur-compose" "$F/cur-env" >>"$F/compose_reads"
  if [ -f "$F/race" ]; then bash "$F/race" out "$getwd"; fi
  file=$F/cur-compose
  envfile=$F/cur-env
  case $sub in
  ps) [ -s "$F/running_image" ] && echo 0123456789ab; exit 0 ;;
  config)
    case " $* " in *" --services "*) printf 'tailscale\nhippo\n'; exit 0 ;; esac
    [ -n "$envfile" ] && cat "$envfile" && cat "$envfile" >&2
    echo compose-noise-output
    echo compose-noise-error >&2
    exit "$(code config_exit)" ;;
  pull) exit "$(code services_pull_exit)" ;;
  exec)
    if [ -f "$F/on_exec" ]; then bash "$F/on_exec"; fi
    cp "$file" "$F/compose_at_backup"
    echo "{\"time\":\"2026-11-25T04:00:00+01:00\",\"level\":\"INFO\",\"msg\":\"backup\",\"outcome\":\"ok\",\"backup\":\"$(cat "$F/backup_name")\"}"
    exit "$(code backup_exit)" ;;
  up)
    cp "$file" "$F/compose_at_up"
    cp "$envfile" "$F/env_at_up"
    m=$HIPPO_UPDATE_STATE_DIR/installing
    if [ -f "$m" ]; then cp "$m" "$F/marker_at_up"; fi
    c=$(code up_exit)
    if [ "$c" = 0 ]; then cp "$F/latest_local" "$F/running_image"; fi
    exit "$c" ;;
  *) exit 0 ;;
  esac ;;
inspect)
  [ -s "$F/running_image" ] || exit 1
  case $3 in
  *Health*)
    h=$(next_health)
    case $h in *' '*) echo "$h" ;; *) echo "$h $(cat "$F/running_image")" ;; esac ;;
  *) cat "$F/running_image" ;;
  esac ;;
image)
  ref=$5
  if [ "$ref" = "$IMAGE" ]; then
    [ -s "$F/latest_local" ] || exit 1
    id=$(cat "$F/latest_local")
  else
    id=$ref
  fi
  case $4 in
  *revision*) if [ -f "$(label_file "$id")" ]; then cat "$(label_file "$id")"; else echo '<no value>'; fi ;;
  *) echo "$id" ;;
  esac ;;
pull)
  if [ -f "$F/swap_on_pull" ] && [ ! -f "$F/swapped" ]; then
    : >"$F/swapped"
    bash "$F/swap_on_pull"
  fi
  c=$(code pull_exit)
  [ "$c" = 0 ] || exit "$c"
  cp "$F/published_image" "$F/latest_local"
  echo "$IMAGE" ;;
tag) echo "$2" >"$F/latest_local" ;;
rmi) rm -f "$F/latest_local" ;;
*) exit 0 ;;
esac
EOF

cat >"$ROOT/bin/curl" <<'EOF'
#!/bin/bash
# Fake curl: copies $FAKE_DIR/repo/<revision>/infra/<path> to the -o file, for a URL that holds
# a 40-character revision; fails like curl -f when the file is absent or named in curl_fail.
set -u
printf 'curl %s\n' "$*" >>"$FAKE_DIR/calls.log"
out='' url=''
while [ $# -gt 0 ]; do
  case $1 in
  -o) out=$2; shift 2 ;;
  --max-time | --retry) shift 2 ;;
  -*) shift ;;
  *) url=$1; shift ;;
  esac
done
rev=$(printf '%s' "$url" | sed -n 's|^.*/\([0-9a-f]\{40\}\)/infra/.*$|\1|p')
[ -n "$rev" ] && [ -n "$out" ] || exit 22
path=${url#*/"$rev"/infra/}
if [ -f "$FAKE_DIR/curl_fail" ] && [ "$(cat "$FAKE_DIR/curl_fail")" = "$path" ]; then exit 22; fi
[ -f "$FAKE_DIR/repo/$rev/infra/$path" ] || exit 22
cp "$FAKE_DIR/repo/$rev/infra/$path" "$out"
EOF

cat >"$ROOT/bin/flock" <<'EOF'
#!/bin/bash
# Fake flock: the lock is held by another run when $FAKE_DIR/lock_held exists.
printf 'flock %s\n' "$*" >>"$FAKE_DIR/calls.log"
[ -f "$FAKE_DIR/lock_held" ] && exit 1
exit 0
EOF

cat >"$ROOT/bin/ls" <<'EOF'
#!/bin/bash
# Fake ls: the real one, except that `ls -ldn <path>` reports the uid written in
# $FAKE_DIR/ls_owner ("<path> <uid>") for that path.
out=$(/bin/ls "$@") || exit $?
if [ "${1:-}" = -ldn ] && [ -f "$FAKE_DIR/ls_owner" ]; then
  read -r path uid <"$FAKE_DIR/ls_owner"
  if [ "${2:-}" = "$path" ]; then
    read -r mode links _ rest <<<"$out"
    printf '%s %s %s %s\n' "$mode" "$links" "$uid" "$rest"
    exit 0
  fi
fi
printf '%s\n' "$out"
EOF

chmod 755 "$ROOT/bin/docker" "$ROOT/bin/curl" "$ROOT/bin/flock" "$ROOT/bin/ls"

# --- Scenario helpers --------------------------------------------------------------------------

# label <image id> <revision>: the revision label of an image.
label() { printf '%s\n' "$2" >"$F/labels/$(printf '%s' "$1" | tr ':' '_')"; }

# publish <image id> <revision>: the image that `docker pull` gets, and its label.
publish() {
  printf '%s\n' "$1" >"$F/published_image"
  label "$1" "$2"
}

# repo_same <revision>: the repository at <revision> holds the installed deployment files and
# the original script.
repo_same() {
  local d=$F/repo/$1/infra f
  mkdir -p "$d/tailscale/config"
  for f in $DEPLOY_FILES; do cp "$P/$f" "$d/$f"; done
  cp "$SCRIPT" "$d/hippo-update.sh"
}

# repo_file <revision> <path> <content>: one file of the repository at <revision>.
repo_file() { printf '%s\n' "$3" >"$F/repo/$1/infra/$2"; }

# set_env <line>...: the lines of .env.
set_env() { printf '%s\n' "$@" >"$P/.env"; }

# setup: a fresh project folder where hippo runs the old image (the local latest tag points to
# it), the installed script outside it, and a new published image whose commit changes the
# compose and serve.json.
setup() {
  T=$(mktemp -d "$ROOT/t.XXXXXX")
  P=$T/proj
  F=$T/fake
  SELF=$T/sbin/hippo-update
  STATE=$T/state
  LOG=$STATE/update.log
  mkdir -p "$P/tailscale/config" "$F/labels" "$T/sbin"
  cp "$SCRIPT" "$SELF"
  chmod 700 "$SELF"
  printf 'name: hippo\n# compose at the old revision\n' >"$P/docker-compose.yml"
  printf '{"serve": "old"}\n' >"$P/tailscale/config/serve.json"
  printf 'API_TOKEN=\nHIPPO_IMAGE_TAG=latest\n# DATA_DIR=/data\n' >"$P/.env.example"
  set_env 'API_TOKEN=test-token-value' 'HIPPO_IMAGE_TAG=latest'
  printf '%s\n' "$OLD_ID" >"$F/running_image"
  printf '%s\n' "$OLD_ID" >"$F/latest_local"
  printf '%s\n' "$BACKUP" >"$F/backup_name"
  label "$OLD_ID" "$OLD_REV"
  publish "$NEW_ID" "$NEW_REV"
  repo_same "$NEW_REV"
  repo_file "$NEW_REV" docker-compose.yml 'name: hippo
# compose at the new revision'
  repo_file "$NEW_REV" tailscale/config/serve.json '{"serve": "new"}'
  cp "$P/docker-compose.yml" "$T/old-compose"
  : >"$F/calls.log"
  HEALTH_TIMEOUT=5
  HEALTH_INTERVAL=0
}

# tree [folder]: the paths under the project folder (or <folder>), sorted.
tree() { (cd "${1:-$P}" && find . | sort); }
# state_tree: the paths under the state folder, or "none" when there is none.
state_tree() { if [ -d "$STATE" ]; then tree "$STATE"; else echo none; fi; }

# snapshot: checksums of the installed deployment files and script.
snapshot() {
  (cd "$T" && cksum proj/docker-compose.yml proj/tailscale/config/serve.json proj/.env.example sbin/hippo-update)
}

# run_with <argument>...: one run of the installed copy of the script with the fakes, from $T;
# sets RC, $T/out and $T/err. run: the same with the project folder as argument.
run_with() {
  snapshot >"$T/snapshot"
  tree >"$T/tree"
  state_tree >"$T/state-tree"
  (cd "$T" && env PATH="$ROOT/bin:$PATH" FAKE_DIR="$F" HIPPO_UPDATE_OWNER_UID="$(id -u)" \
    HIPPO_UPDATE_STATE_DIR="$STATE" \
    HIPPO_UPDATE_REPO_URL="$REPO_URL" HIPPO_UPDATE_HEALTH_TIMEOUT="$HEALTH_TIMEOUT" \
    HIPPO_UPDATE_HEALTH_INTERVAL="$HEALTH_INTERVAL" "$SELF" "$@" >"$T/out" 2>"$T/err")
  RC=$?
}
run() { run_with "$P"; }

# --- Assertions --------------------------------------------------------------------------------

# check <message> <command>...: fails the current test when <command> fails.
check() {
  if ! "${@:2}"; then
    echo "FAIL [$CURRENT] $1"
    ok=0
  fi
}
status_is() { [ "$RC" = "$1" ]; }
status_not_zero() { [ "$RC" != 0 ]; }
# last_log_has <fragment>...: the last line of the update log holds every fragment.
last_log_has() {
  local line f
  line=$(tail -n 1 "$LOG" 2>/dev/null) || return 1
  for f in "$@"; do
    case $line in *"$f"*) ;; *) return 1 ;; esac
  done
}
# line_has <n> <fragment>...: line <n> of the update log holds every fragment.
line_has() {
  local line f
  line=$(sed -n "$1p" "$LOG" 2>/dev/null)
  shift
  for f in "$@"; do
    case $line in *"$f"*) ;; *) return 1 ;; esac
  done
}
has_call() { grep -qE -- "$1" "$F/calls.log"; }
no_call() { ! grep -qE -- "$1" "$F/calls.log"; }
call_count() { [ "$(grep -cE -- "$1" "$F/calls.log")" = "$2" ]; }
# before <ERE a> <ERE b>: the first call matching a comes before the first matching b.
before() {
  local a b
  a=$(grep -nE -- "$1" "$F/calls.log" | head -n 1 | cut -d: -f1)
  b=$(grep -nE -- "$2" "$F/calls.log" | head -n 1 | cut -d: -f1)
  [ -n "$a" ] && [ -n "$b" ] && [ "$a" -lt "$b" ]
}
unchanged() { snapshot | cmp -s - "$T/snapshot"; }
same_file() { cmp -s "$1" "$2"; }
# new_files_installed [folder]: the project folder (or <folder>) holds the fetched files.
new_files_installed() {
  local f
  for f in $DEPLOY_FILES; do cmp -s "${1:-$P}/$f" "$F/repo/$NEW_REV/infra/$f" || return 1; done
}
# not_in <text> <file>: <file> does not hold <text>.
not_in() { ! grep -qF -- "$1" "$2"; }
# no_script_under <folder>: no update script file anywhere under <folder>.
no_script_under() { [ -z "$(find "$1" -name 'hippo-update*')" ]; }
absent() { [ ! -e "$1" ]; }
mode_is() { [ "$(ls -l "$2" | cut -c1-10)" = "$1" ]; }
# nowhere <text>: neither the update log nor the run's output holds <text>.
nowhere() { ! grep -sqF -- "$1" "$LOG" "$T/out" "$T/err"; }
# err_has <text>: the run's error output holds <text>.
err_has() { grep -qF -- "$1" "$T/err"; }
# refused: what every refused run shares: non-zero exit, no docker command, nothing created in
# the project folder nor the state folder, no log line on the output.
refused() {
  check "exits non-zero" status_not_zero
  check "runs no docker command" no_call '^docker '
  check "creates nothing in the project folder" same_file "$T/tree" <(tree)
  check "creates nothing in the state folder" same_file "$T/state-tree" <(state_tree)
  check "prints no log line" [ ! -s "$T/out" ]
  check "replaces no file" unchanged
}
inode() { ls -i "$1" | awk '{print $1}'; }

BACKUP_CALL='^docker compose .* exec -T hippo /hippo backup$'
UP_CALL='^docker compose .* up -d$'
SERVICES_PULL_CALL='^docker compose .* pull '

# --- A pinned image tag pauses the automatic update --------------------------------------------

test_a_pinned_tag_pauses_the_update() {
  set_env 'API_TOKEN=x' 'HIPPO_IMAGE_TAG=sha-3fb9955'
  run
  check "exits 0" status_is 0
  check "logs paused" last_log_has '"outcome":"paused"'
  check "runs no docker command" no_call '^docker '
  check "runs no curl command" no_call '^curl '
}

test_a_quoted_latest_tag_is_followed() {
  set_env 'API_TOKEN=x' 'HIPPO_IMAGE_TAG="latest" '
  run
  check "pulls the published image" has_call "^docker pull -q $IMAGE\$"
  check "goes on to the update" last_log_has '"outcome":"updated"'
}

test_a_missing_image_tag_fails_the_run() {
  set_env 'API_TOKEN=x'
  run
  check "exits non-zero" status_not_zero
  check "logs failed/no_image_tag" last_log_has '"outcome":"failed"' '"reason":"no_image_tag"'
  check "runs no docker command" no_call '^docker '
}

test_the_environment_file_is_never_executed() {
  set_env 'API_TOKEN=x' \
    "INJECTED=\$(touch $T/pwned-1)" \
    "BACKTICK=\`touch $T/pwned-2\`" \
    "HIPPO_IMAGE_TAG=\$(touch $T/pwned-3)" \
    'HIPPO_IMAGE_TAG=latest'
  run
  check "runs no command of .env" absent "$T/pwned-1"
  check "runs no command of .env (backticks)" absent "$T/pwned-2"
  check "runs no command of .env (image tag)" absent "$T/pwned-3"
  check "reads the last HIPPO_IMAGE_TAG line" last_log_has '"outcome":"updated"'
}

# --- One run at a time -------------------------------------------------------------------------

test_a_second_run_while_one_holds_the_lock() {
  : >"$F/lock_held"
  run
  check "exits non-zero" status_not_zero
  check "logs locked" last_log_has '"outcome":"locked"'
  check "runs no docker command" no_call '^docker '
}

# --- The deployment files come from the commit of the published image --------------------------

test_the_files_are_fetched_at_the_images_revision() {
  run
  local f
  for f in $DEPLOY_FILES hippo-update.sh; do
    check "fetches $f at the revision" has_call "^curl .* $REPO_URL/$NEW_REV/infra/$f\$"
  done
  check "fetches four files" call_count '^curl ' 4
  check "fetches nothing from a branch name" no_call '^curl .*/(main|master|HEAD)/'
}

test_a_malformed_revision_fails_the_run() {
  rm -f "$F/labels/$(printf '%s' "$NEW_ID" | tr ':' '_')"
  run
  check "no label: exits non-zero" status_not_zero
  check "no label: logs failed/bad_revision" last_log_has '"outcome":"failed"' '"reason":"bad_revision"'
  check "no label: fetches nothing" no_call '^curl '
  check "no label: replaces no file" unchanged
  label "$NEW_ID" "$(rep A 40)"
  : >"$F/calls.log"
  run
  check "uppercase label: exits non-zero" status_not_zero
  check "uppercase label: logs failed/bad_revision" last_log_has '"outcome":"failed"' '"reason":"bad_revision"'
  check "uppercase label: fetches nothing" no_call '^curl '
  check "uppercase label: replaces no file" unchanged
}

test_a_failed_fetch_fails_the_run() {
  printf 'tailscale/config/serve.json' >"$F/curl_fail"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/fetch_failed" last_log_has '"outcome":"failed"' '"reason":"fetch_failed"'
  check "replaces no file" unchanged
}

test_a_failed_pull_fails_the_run() {
  echo 1 >"$F/pull_exit"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/pull_failed" last_log_has '"outcome":"failed"' '"reason":"pull_failed"'
  check "runs no backup" no_call "$BACKUP_CALL"
  check "runs no up" no_call "$UP_CALL"
}

# --- Nothing new means nothing happens ---------------------------------------------------------

test_same_image_and_same_files() {
  publish "$OLD_ID" "$OLD_REV"
  repo_same "$OLD_REV"
  run
  check "exits 0" status_is 0
  check "logs up-to-date" last_log_has '"outcome":"up-to-date"'
  check "runs no backup" no_call "$BACKUP_CALL"
  check "runs no up" no_call "$UP_CALL"
  check "pulls no other image" no_call "$SERVICES_PULL_CALL"
  check "changes no file" unchanged
}

test_a_changed_deployment_file_alone_is_an_update() {
  publish "$OLD_ID" "$OLD_REV"
  repo_same "$OLD_REV"
  repo_file "$OLD_REV" docker-compose.yml 'name: hippo
# compose changed at the same image'
  run
  check "runs the backup" has_call "$BACKUP_CALL"
  check "runs up" has_call "$UP_CALL"
  check "logs updated" last_log_has '"outcome":"updated"'
}

# --- Files that do not fit the environment are refused -----------------------------------------

test_a_new_required_variable_is_missing_from_the_environment_file() {
  repo_file "$NEW_REV" .env.example 'API_TOKEN=
HIPPO_IMAGE_TAG=latest
NEW_SETTING='
  run
  check "exits non-zero" status_not_zero
  check "logs failed/missing_variables with the name" \
    last_log_has '"outcome":"failed"' '"reason":"missing_variables"' '"missing":"NEW_SETTING"'
  check "runs no backup" no_call "$BACKUP_CALL"
  check "replaces no file" unchanged
}

test_a_commented_variable_is_not_required() {
  repo_file "$NEW_REV" .env.example 'API_TOKEN=
HIPPO_IMAGE_TAG=latest
# DATA_DIR=/data'
  run
  check "goes on to the backup" has_call "$BACKUP_CALL"
}

test_a_compose_that_does_not_render_is_refused() {
  echo 1 >"$F/config_exit"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/compose_invalid" last_log_has '"outcome":"failed"' '"reason":"compose_invalid"'
  check "checks the new compose with the real .env" \
    has_call "^docker compose --project-directory \\. --env-file $STATE/run/\\.env -f $STATE/staging/docker-compose\\.yml config --quiet\$"
  check "runs no backup" no_call "$BACKUP_CALL"
  check "replaces no file" unchanged
  check "copies nothing printed by docker compose" nowhere compose-noise
  check "copies no value of .env" nowhere test-token-value
}

# --- The running version is backed up before anything changes ----------------------------------

test_the_backup_runs_in_the_running_container_before_the_files_change() {
  run
  check "runs the backup once" call_count "$BACKUP_CALL" 1
  check "backs up with a copy of the installed compose" \
    has_call "^docker compose --project-directory \\. --env-file $STATE/run/\\.env -f $STATE/run/docker-compose\\.yml exec -T hippo /hippo backup\$"
  check "the installed compose was the old one at backup time" same_file "$F/compose_at_backup" "$T/old-compose"
  check "pulls the other images before the backup" before "$SERVICES_PULL_CALL" "$BACKUP_CALL"
  check "backs up before up" before "$BACKUP_CALL" "$UP_CALL"
  check "never runs docker compose run" no_call '^docker compose .* run( |$)'
}

test_a_failed_backup_stops_the_update() {
  echo 1 >"$F/backup_exit"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/backup_failed" last_log_has '"outcome":"failed"' '"reason":"backup_failed"'
  check "leaves no copy of .env" absent "$STATE/run"
  check "replaces no file" unchanged
  check "runs no up" no_call "$UP_CALL"
}

test_hippo_not_running_stops_the_update() {
  rm -f "$F/running_image"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/hippo_not_running" last_log_has '"outcome":"failed"' '"reason":"hippo_not_running"'
  check "runs no backup" no_call "$BACKUP_CALL"
  check "replaces no file" unchanged
}

# --- A run stopped before the update leaves the local latest tag as it was ---------------------

test_the_previous_latest_tag_is_put_back_after_a_failed_backup() {
  echo 1 >"$F/backup_exit"
  run
  check "the run stopped" last_log_has '"reason":"backup_failed"'
  check "latest points to the running image again" [ "$(cat "$F/latest_local" 2>/dev/null)" = "$OLD_ID" ]
}

# --- An update replaces the files, restarts and checks the health ------------------------------

test_a_successful_update() {
  # The old container is still healthy at the first poll: only the new image counts.
  printf 'healthy %s\nstarting\nhealthy\n' "$OLD_ID" >"$F/health"
  local inode_before f
  inode_before=$(inode "$P/docker-compose.yml")
  mkdir -p "$T/installed/tailscale/config"
  for f in $DEPLOY_FILES; do cp "$P/$f" "$T/installed/$f"; done
  run
  check "exits 0" status_is 0
  check "installs the fetched files" new_files_installed
  for f in $DEPLOY_FILES; do
    check "keeps the previous $f apart" same_file "$STATE/previous/$f" "$T/installed/$f"
  done
  check "replaces the compose by a rename" [ "$(inode "$P/docker-compose.yml")" != "$inode_before" ]
  check "runs up once" call_count "$UP_CALL" 1
  check "marks the revision being installed during up" [ "$(cat "$F/marker_at_up" 2>/dev/null)" = "$NEW_REV" ]
  check "waits until healthy on the new image" call_count '^docker inspect -f \{\{\.State\.Health' 3
  check "logs updated with the revisions and the backup" last_log_has '"outcome":"updated"' \
    "\"old_revision\":\"$OLD_REV\"" "\"new_revision\":\"$NEW_REV\"" "\"backup\":\"$BACKUP\""
  check "leaves no marker" absent "$STATE/installing"
  check "leaves no copy of .env" absent "$STATE/run"
}

test_an_unhealthy_start_is_logged_and_left_as_is() {
  echo starting >"$F/health"
  HEALTH_TIMEOUT=1
  HEALTH_INTERVAL=1
  local start=$SECONDS
  run
  check "gives up within the health timeout" [ $((SECONDS - start)) -le 4 ]
  check "exits non-zero" status_not_zero
  check "logs failed/unhealthy with the revisions and the backup" \
    last_log_has '"outcome":"failed"' '"reason":"unhealthy"' \
    "\"old_revision\":\"$OLD_REV\"" "\"new_revision\":\"$NEW_REV\"" "\"backup\":\"$BACKUP\""
  check "runs no other up" call_count "$UP_CALL" 1
  check "leaves the new files in place" new_files_installed
}

test_a_failed_start_is_logged_and_left_as_is() {
  echo 1 >"$F/up_exit"
  run
  check "exits non-zero" status_not_zero
  check "logs failed/up_failed with the backup" \
    last_log_has '"outcome":"failed"' '"reason":"up_failed"' "\"backup\":\"$BACKUP\""
  check "leaves the new files in place" new_files_installed
}

# --- The update script replaces itself only after a successful update --------------------------

test_the_script_is_replaced_after_an_update() {
  { cat "$SCRIPT"; echo '# next version'; } >"$F/repo/$NEW_REV/infra/hippo-update.sh"
  run
  check "logs updated" last_log_has '"outcome":"updated"'
  check "installs the fetched script in place of its own file" same_file "$SELF" "$F/repo/$NEW_REV/infra/hippo-update.sh"
  check "executable by its owner only" mode_is -rwx------ "$SELF"
  check "writes no update script under the project folder" no_script_under "$P"
}

test_the_script_is_kept_after_a_failed_update() {
  { cat "$SCRIPT"; echo '# next version'; } >"$F/repo/$NEW_REV/infra/hippo-update.sh"
  echo unhealthy >"$F/health"
  HEALTH_TIMEOUT=1
  HEALTH_INTERVAL=1
  run
  check "logs failed" last_log_has '"outcome":"failed"'
  check "keeps the installed script" same_file "$SELF" "$SCRIPT"
}

# --- An interrupted run is reported ------------------------------------------------------------

test_a_marker_left_by_a_crash() {
  mkdir -p "$STATE"
  printf '%s\n' "$CRASH_REV" >"$STATE/installing"
  run
  check "first logs interrupted with the revision" line_has 1 '"outcome":"interrupted"' "\"new_revision\":\"$CRASH_REV\""
  check "then goes on" line_has 2 '"outcome":"updated"'
  check "leaves no marker" absent "$STATE/installing"
}

# --- Each run writes one bounded log line, never a secret --------------------------------------

test_secrets_of_the_environment_file_never_reach_the_log() {
  set_env 'API_TOKEN=s3cr3t-token-value' 'HIPPO_IMAGE_TAG=latest'
  run
  check "the run updates" last_log_has '"outcome":"updated"'
  check "the secret is nowhere" nowhere s3cr3t-token-value
}

test_the_log_is_readable_by_its_owner_only() {
  mkdir -p "$STATE"
  printf '{"time":"2026-11-24T04:00:00+0100","msg":"update","outcome":"paused","duration_s":0}\n' >"$LOG"
  chmod 644 "$LOG"
  printf '%s\n' "$CRASH_REV" >"$STATE/installing"
  run
  check "mode 0600" mode_is -rw------- "$LOG"
  check "one more line per outcome (interrupted, updated)" [ "$(wc -l <"$LOG" | tr -d ' ')" = 3 ]
  check "each line a JSON object" [ "$(grep -cvE "$JSON_LINE" "$LOG")" = 0 ]
  check "the output holds the same lines" same_file "$T/out" <(tail -n 2 "$LOG")
}

test_a_full_log_is_rotated() {
  mkdir -p "$STATE"
  { printf '{"old":"'; head -c 1048576 /dev/zero | tr '\0' x; printf '"}\n'; } >"$LOG"
  cp "$LOG" "$T/full-log"
  set_env 'API_TOKEN=x' 'HIPPO_IMAGE_TAG=sha-3fb9955'
  run
  check "the previous content is in the rotated file" same_file "$STATE/update.log.1" "$T/full-log"
  check "the log holds only the new line" [ "$(wc -l <"$LOG" | tr -d ' ')" = 1 ]
  check "the new line is the run's" last_log_has '"outcome":"paused"'
}

# --- The project folder and the deployment files belong to root alone -------------------------

test_a_project_folder_writable_by_its_group_is_refused() {
  chmod g+w "$P"
  run
  refused
  check "names the project folder and the cause" err_has "$P is writable by its group"
}

test_a_state_folder_writable_by_its_group_is_refused() {
  mkdir -p "$STATE"
  chmod g+w "$STATE"
  run
  refused
  check "names the state folder and the cause" err_has "$STATE is writable by its group"
}

test_a_compose_file_of_another_owner_is_refused() {
  printf './docker-compose.yml 4242\n' >"$F/ls_owner"
  run
  refused
  check "names the compose file and its owner" err_has "$P/docker-compose.yml belongs to uid 4242"
}

test_a_compose_file_that_is_a_symbolic_link_is_refused() {
  mv "$P/docker-compose.yml" "$T/elsewhere.yml"
  ln -s "$T/elsewhere.yml" "$P/docker-compose.yml"
  run
  refused
  check "names the compose file and the cause" err_has "$P/docker-compose.yml is a symbolic link"
}

test_a_missing_project_folder_argument_is_refused() {
  run_with
  refused
  check "no argument: prints the usage" err_has "usage: hippo-update"
  : >"$F/calls.log"
  run_with proj
  refused
  check "relative path: says it must be absolute" err_has "must be an absolute path"
}

test_a_project_folder_swapped_during_the_run_is_not_followed() {
  { cat "$SCRIPT"; echo '# next version'; } >"$F/repo/$NEW_REV/infra/hippo-update.sh"
  cp "$P/.env" "$T/genuine-env"
  # Whoever can write the parent folder attacks in two ways. First, on `docker pull`, after the
  # checks, it renames the project folder and puts its own (A) at the same path for good, with a
  # compose, a .env, and the files a run that kept its state in the project folder would read.
  cat >"$F/swap_on_pull" <<EOF
mv "$P" "$T/genuine"
mkdir -p "$P/tailscale/config" "$P/update/staging"
printf 'name: hippo\n# ATTACKER COMPOSE A\n' >"$P/docker-compose.yml"
printf 'API_TOKEN=x\nHIPPO_IMAGE_TAG=latest\nATTACKER_ENV=A\n' >"$P/.env"
cp "$T/genuine/.env.example" "$P/.env.example"
cp "$T/genuine/tailscale/config/serve.json" "$P/tailscale/config/serve.json"
cp "$P/docker-compose.yml" "$P/update/staging/docker-compose.yml"
printf '#!/bin/bash\n# ATTACKER SCRIPT\n' >"$P/update/staging/hippo-update.sh"
(cd "$P" && find . | sort) >"$F/attacker_tree"
EOF
  cat >"$F/on_exec" <<EOF
if [ -d "$P/update/staging" ]; then printf '#!/bin/bash\n# ATTACKER SCRIPT\n' >"$P/update/staging/hippo-update.sh"; fi
EOF
  # Second, around every docker compose, it swaps the folder Compose took as its working
  # directory with its own (B) right after Getwd, and back once Compose opened its files: two
  # renames, as a watcher on the folder would do.
  mkdir -p "$T/attacker-b/tailscale/config"
  printf 'name: hippo\n# ATTACKER COMPOSE B\n' >"$T/attacker-b/docker-compose.yml"
  printf 'API_TOKEN=x\nHIPPO_IMAGE_TAG=latest\nATTACKER_ENV=B\n' >"$T/attacker-b/.env"
  tree "$T/attacker-b" >"$T/attacker-b-tree"
  cat >"$F/race" <<EOF
case \$1 in
in) mv "\$2" "\$2.genuine" && mv "$T/attacker-b" "\$2" ;;
out) mv "\$2" "$T/attacker-b" && mv "\$2.genuine" "\$2" ;;
esac
EOF
  run
  check "the swap happened" [ -d "$T/genuine" ]
  check "the run updates" last_log_has '"outcome":"updated"'
  check "no docker compose read a file of an attacker" not_in "ATTACKER" "$F/compose_reads"
  check "backs up with the compose of the checked folder" same_file "$F/compose_at_backup" "$T/old-compose"
  check "up -d gets a compose outside the project folder" has_call "^docker compose .* -f $STATE/staging/docker-compose\\.yml up -d\$"
  check "up -d reads the fetched compose" same_file "$F/compose_at_up" "$F/repo/$NEW_REV/infra/docker-compose.yml"
  check "up -d reads the .env of the checked folder" same_file "$F/env_at_up" "$T/genuine-env"
  check "installs the new files in the checked folder" new_files_installed "$T/genuine"
  check "writes nothing in the folder put in its place" same_file "$F/attacker_tree" <(tree)
  check "leaves its compose as it was" grep -q 'ATTACKER COMPOSE A' "$P/docker-compose.yml"
  check "writes nothing in the folder swapped in around Compose" same_file "$T/attacker-b-tree" <(tree "$T/attacker-b")
  check "installs the repository's script" same_file "$SELF" "$F/repo/$NEW_REV/infra/hippo-update.sh"
  check "writes no update script under the checked folder" no_script_under "$T/genuine"
  check "leaves no copy of .env" absent "$STATE/run"
}

# --- Runner ------------------------------------------------------------------------------------

passed=0
failed=0
# t <scenario> <function>: one test, in a fresh project folder.
t() {
  CURRENT=$1
  ok=1
  setup
  "$2"
  if [ "$ok" = 1 ]; then passed=$((passed + 1)); else failed=$((failed + 1)); fi
}

t "A pinned tag pauses the update" test_a_pinned_tag_pauses_the_update
t "A quoted latest tag is followed" test_a_quoted_latest_tag_is_followed
t "A missing image tag fails the run" test_a_missing_image_tag_fails_the_run
t "The environment file is never executed" test_the_environment_file_is_never_executed
t "A second run while one holds the lock" test_a_second_run_while_one_holds_the_lock
t "The files are fetched at the image's revision" test_the_files_are_fetched_at_the_images_revision
t "A malformed revision fails the run" test_a_malformed_revision_fails_the_run
t "A failed fetch fails the run" test_a_failed_fetch_fails_the_run
t "A failed pull fails the run" test_a_failed_pull_fails_the_run
t "Same image and same files" test_same_image_and_same_files
t "A changed deployment file alone is an update" test_a_changed_deployment_file_alone_is_an_update
t "A new required variable is missing from the environment file" test_a_new_required_variable_is_missing_from_the_environment_file
t "A commented variable is not required" test_a_commented_variable_is_not_required
t "A compose that does not render is refused" test_a_compose_that_does_not_render_is_refused
t "The backup runs in the running container before the files change" test_the_backup_runs_in_the_running_container_before_the_files_change
t "A failed backup stops the update" test_a_failed_backup_stops_the_update
t "Hippo not running stops the update" test_hippo_not_running_stops_the_update
t "The previous latest tag is put back after a failed backup" test_the_previous_latest_tag_is_put_back_after_a_failed_backup
t "A successful update" test_a_successful_update
t "An unhealthy start is logged and left as is" test_an_unhealthy_start_is_logged_and_left_as_is
t "A failed start is logged and left as is" test_a_failed_start_is_logged_and_left_as_is
t "The script is replaced after an update" test_the_script_is_replaced_after_an_update
t "The script is kept after a failed update" test_the_script_is_kept_after_a_failed_update
t "A marker left by a crash" test_a_marker_left_by_a_crash
t "Secrets of the environment file never reach the log" test_secrets_of_the_environment_file_never_reach_the_log
t "The log is readable by its owner only" test_the_log_is_readable_by_its_owner_only
t "A full log is rotated" test_a_full_log_is_rotated
t "A state folder writable by its group is refused" test_a_state_folder_writable_by_its_group_is_refused
t "A project folder writable by its group is refused" test_a_project_folder_writable_by_its_group_is_refused
t "A compose file of another owner is refused" test_a_compose_file_of_another_owner_is_refused
t "A compose file that is a symbolic link is refused" test_a_compose_file_that_is_a_symbolic_link_is_refused
t "A missing project folder argument is refused" test_a_missing_project_folder_argument_is_refused
t "A project folder swapped during the run is not followed" test_a_project_folder_swapped_during_the_run_is_not_followed

if [ "$failed" = 0 ]; then
  echo "hippo-update.test: ALL $passed SCENARIOS OK"
else
  echo "hippo-update.test: $failed of $((passed + failed)) scenarios FAILED" >&2
  exit 1
fi
