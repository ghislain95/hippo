#!/bin/bash
# Nightly update of Hippo on the NAS (RG-99, ADR-0040; openspec change nightly-update, design.md).
# Installed as /usr/local/sbin/hippo-update (root, 0700), outside the project folder, which sits in
# a UGOS share; run by root's cron at 04:00 (/etc/cron.d/hippo-update, docs/runbook-nas.md):
#   hippo-update /<absolute path of the project folder>
# Its state lives outside the share too, in /var/lib/hippo-update (root, 0700): lock, update.log,
# run/, staging/, previous/, installing. The only writes to the project folder are the deployment
# files installed at step 6. One run, in this order:
#  0. enters the project folder (cd -P) and from then on reaches it by relative paths only: the
#     working directory holds the folder that was checked, even if its path is renamed and another
#     folder put in its place (design D16). Docker Compose opens its files by absolute path, so it
#     never gets the project folder's compose nor .env: once the lock is held, both are copied
#     (through the working directory) into <state>/run/, and Compose reads those copies, then the
#     fetched compose once it is installed (design D17). run/ is removed when the run ends. Refuses to start, with a message on standard error, no
#     log line and no docker command, unless the project folder, its deployment files, .env, the
#     folders that hold them, the state folder and this script belong to root alone: not a
#     symbolic link, owned by root, not writable by group nor others, no ACL (design D15);
#  1. takes a lock (flock -n on <state>/lock) and reports a run interrupted while installing (the
#     marker <state>/installing);
#  2. hand brake: unless HIPPO_IMAGE_TAG in .env is `latest`, only logs `paused`;
#  3. pulls ghcr.io/ghislain95/hippo:latest, reads the commit it was built from (its
#     org.opencontainers.image.revision label) and fetches the deployment files of that commit
#     (docker-compose.yml, tailscale/config/serve.json, .env.example and this script) into
#     <state>/staging/; same image and same files as what runs: logs `up-to-date`;
#  4. refuses new files that do not fit .env: a variable of the new .env.example missing from
#     .env, or a new compose that fails `docker compose config --quiet`;
#  5. pulls the other images of the new compose, then backs up with the running version
#     (`docker compose exec -T hippo /hippo backup`, never `run`, RG-98);
#  6. point of no return: writes the marker, keeps the installed files in <state>/previous/,
#     installs the new ones by rename, runs `docker compose up -d` and waits for hippo to be
#     healthy on the new image;
#  7. after `updated` only, replaces its own file with the fetched script (infra/hippo-update.sh).
# A failed step stops the run: log line `failed` with its reason, exit 1. Before the point of no
# return, the local latest tag is put back as it was; after it, nothing is undone. The script never
# writes .env, never runs `docker compose down`, `run` nor `--force-recreate`.
#
# .env is never sourced nor evaluated: only the value of HIPPO_IMAGE_TAG and the variable names are
# read, with sed. The log, <state>/update.log (0600, rotated over 1 MiB into update.log.1), holds one
# JSON line per outcome, built from fixed keys whose values are checked against their form; nothing
# printed by docker or curl is ever copied into it. The line is printed on standard output too.
#
# Environment, for the tests only (cron sets none of them):
#   HIPPO_UPDATE_OWNER_UID        uid that must own the checked paths (default 0, root)
#   HIPPO_UPDATE_STATE_DIR        state folder, absolute (default /var/lib/hippo-update)
#   HIPPO_UPDATE_HEALTH_TIMEOUT   seconds to wait for a healthy hippo (default 180)
#   HIPPO_UPDATE_HEALTH_INTERVAL  seconds between two health polls (default 5)
#   HIPPO_UPDATE_REPO_URL         root of the raw files of the repository
#                                 (default https://raw.githubusercontent.com/ghislain95/hippo)
# Needs: bash, docker compose v2, curl, flock (util-linux), sed, cmp.
# Portable to bash 3.2 and BSD tools: its tests, infra/hippo-update.test.sh, run on the Mac.
# The body is functions only and the last line calls main: bash has read the whole file before the
# script replaces itself.
set -u

IMAGE=ghcr.io/ghislain95/hippo:latest
DEFAULT_REPO_URL=https://raw.githubusercontent.com/ghislain95/hippo
DEFAULT_STATE_DIR=/var/lib/hippo-update
# Deployment files, relative to the project folder and to infra/ in the repository.
DEPLOY_FILES="docker-compose.yml tailscale/config/serve.json .env.example"
# The update script in the repository (infra/) and in the staging folder; installed elsewhere.
SCRIPT_FILE=hippo-update.sh
LOG_MAX_BYTES=1048576

RE_REVISION='^[0-9a-f]{40}$'
RE_IMAGE_ID='^sha256:[0-9a-f]{64}$'
RE_BACKUP='^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{6}[+-][0-9]{4}$'
RE_NAMES='^[A-Za-z_][A-Za-z0-9_]*(,[A-Za-z_][A-Za-z0-9_]*)*$'
RE_TIME='^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[+-][0-9]{4}$'
RE_NUMBER='^[0-9]+$'
RE_SERVICE='^[A-Za-z0-9][A-Za-z0-9_.-]*$'
RE_CONTAINER='^[0-9A-Za-z][0-9A-Za-z_.-]*$'

# Facts of the run, written to its log line when set.
OLD_REVISION=''
NEW_REVISION=''
BACKUP_NAME=''
MISSING=''
RUNNING_ID=''
NEW_ID=''
PREVIOUS_LATEST_ID=''
# 1 between the pull and the point of no return: a stop puts the local latest tag back.
RESTORE_LATEST=0

# valid <key> <value>: whether <value> has the form of the log field <key>.
valid() {
  case $1 in
  outcome)
    case $2 in paused | locked | interrupted | up-to-date | updated | failed) return 0 ;; esac ;;
  reason)
    case $2 in
    no_image_tag | hippo_not_running | pull_failed | bad_revision | fetch_failed | \
      missing_variables | compose_invalid | backup_failed | install_failed | up_failed | unhealthy)
      return 0 ;;
    esac ;;
  old_revision | new_revision)
    [ "$2" = unknown ] && return 0
    [[ $2 =~ $RE_REVISION ]] && return 0 ;;
  backup) [[ $2 =~ $RE_BACKUP ]] && return 0 ;;
  missing) [[ $2 =~ $RE_NAMES ]] && return 0 ;;
  duration_s) [[ $2 =~ $RE_NUMBER ]] && return 0 ;;
  time) [[ $2 =~ $RE_TIME ]] && return 0 ;;
  esac
  return 1
}

# log_event <outcome> [<key> <value>]...: appends one JSON line to the log and prints it. Only
# the keys below are written, empty values are left out, and a value that does not have its form
# is written as "invalid".
log_event() {
  local line key value
  value=$(date +%Y-%m-%dT%H:%M:%S%z)
  valid time "$value" || value=invalid
  line="{\"time\":\"$value\",\"msg\":\"update\""
  value=$1
  shift
  valid outcome "$value" || value=invalid
  line="$line,\"outcome\":\"$value\""
  while [ $# -ge 2 ]; do
    key=$1
    value=$2
    shift 2
    [ -n "$value" ] || continue
    valid "$key" "$value" || value=invalid
    case $key in
    reason | old_revision | new_revision | backup | missing) line="$line,\"$key\":\"$value\"" ;;
    duration_s)
      if [ "$value" = invalid ]; then
        line="$line,\"$key\":\"invalid\""
      else
        line="$line,\"$key\":$value"
      fi ;;
    esac
  done
  line="$line}"
  printf '%s\n' "$line"
  printf '%s\n' "$line" >>"$LOG" 2>/dev/null
  chmod 600 "$LOG" 2>/dev/null
}

# fail <reason>: puts the local latest tag back when the run may have moved it, logs `failed`
# with the facts known so far, returns 1.
fail() {
  [ "$RESTORE_LATEST" = 1 ] && restore_latest
  log_event failed reason "$1" old_revision "$OLD_REVISION" new_revision "$NEW_REVISION" \
    backup "$BACKUP_NAME" missing "$MISSING" duration_s "$SECONDS"
  return 1
}

# rotate_log: over LOG_MAX_BYTES, the log becomes update.log.1 (one previous file kept).
rotate_log() {
  local size
  [ -f "$LOG" ] || return 0
  size=$(wc -c <"$LOG" | tr -d '[:space:]')
  [[ $size =~ $RE_NUMBER ]] || return 0
  if [ "$size" -gt "$LOG_MAX_BYTES" ]; then
    mv -f "$LOG" "$LOG.1" && chmod 600 "$LOG.1"
  fi
}

# report_interrupted: a marker left by a run interrupted while installing is logged and removed.
report_interrupted() {
  local revision
  [ -f "$MARKER" ] || return 0
  revision=$(head -n 1 "$MARKER" 2>/dev/null)
  [[ $revision =~ $RE_REVISION ]] || revision=unknown
  log_event interrupted new_revision "$revision"
  rm -f "$MARKER"
}

# read_image_tag: prints the value of the last HIPPO_IMAGE_TAG line of .env, trimmed of spaces,
# carriage returns, an inline comment (` #…`, as Compose reads it) and one pair of surrounding
# quotes. Fails when .env, the line or the value is
# missing. .env is read as text, never executed.
read_image_tag() {
  local lines value
  [ -f "$ENV_FILE" ] || return 1
  lines=$(sed -n 's/^[[:space:]]*HIPPO_IMAGE_TAG[[:space:]]*=/=/p' "$ENV_FILE" 2>/dev/null) || return 1
  [ -n "$lines" ] || return 1
  value=$(printf '%s\n' "$lines" | tail -n 1 | tr -d '\r' |
    sed -e 's/^=//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]#.*$//' -e 's/[[:space:]]*$//')
  case $value in
  \"*\") value=${value#\"} && value=${value%\"} ;;
  \'*\') value=${value#\'} && value=${value%\'} ;;
  esac
  [ -n "$value" ] || return 1
  printf '%s' "$value"
}

# variable_names <file>: the names of the uncommented NAME=value lines of <file>.
variable_names() {
  sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$1" 2>/dev/null
}

# missing_variables: the names set by the new .env.example and absent from .env, comma-separated.
missing_variables() {
  local env_names name missing=''
  env_names=" $(variable_names "$ENV_FILE" | tr '\n' ' ') "
  for name in $(variable_names "$STAGING/.env.example"); do
    case $env_names in
    *" $name "*) ;;
    *) missing="$missing${missing:+,}$name" ;;
    esac
  done
  printf '%s' "$missing"
}

# compose_current / compose_staged <args>...: docker compose on the compose in use or on the
# fetched one, with the copy of .env; the project directory is the working directory, so the
# relative mounts (./data, ./tailscale/...) resolve in the project folder. Compose opens -f and
# --env-file by absolute path: it is only ever given files of the state folder, never a file of the
# project folder (D17). The compose in use is the copy made at the start of the run, then the
# fetched one once it is installed (the same bytes as the installed file).
compose_current() {
  docker compose --project-directory . --env-file "$RUN_ENV" -f "$CURRENT_COMPOSE" "$@"
}
compose_staged() {
  docker compose --project-directory . --env-file "$RUN_ENV" -f "$STAGING/docker-compose.yml" "$@"
}

# copy_run_files: the project folder's compose and .env, read through the working directory (the
# checked folder), copied into an emptied <state>/run/ (0700, files 0600). Compose and the
# reading of .env use these copies only.
copy_run_files() {
  rm -rf "${RUN_DIR:?}" && mkdir -p "$RUN_DIR" && chmod 700 "$RUN_DIR" || return 1
  cp ./docker-compose.yml "$RUN_COMPOSE" && cp ./.env "$RUN_ENV" || return 1
  chmod 600 "$RUN_COMPOSE" "$RUN_ENV"
}

# hippo_container: prints the id of the running hippo container; fails when there is none.
hippo_container() {
  local id
  id=$(compose_current ps -q hippo 2>/dev/null) || return 1
  id=$(printf '%s\n' "$id" | head -n 1)
  [[ $id =~ $RE_CONTAINER ]] || return 1
  printf '%s' "$id"
}

# image_id <ref>: prints the local image id of <ref>, empty when there is none.
image_id() {
  local id
  id=$(docker image inspect -f '{{.Id}}' "$1" 2>/dev/null)
  [[ $id =~ $RE_IMAGE_ID ]] && printf '%s' "$id"
  return 0
}

# revision_of <image id>: prints its revision label, or unknown.
revision_of() {
  local revision
  revision=$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1" 2>/dev/null)
  if [[ $revision =~ $RE_REVISION ]]; then printf '%s' "$revision"; else printf unknown; fi
}

# restore_latest: the local latest tag points again to the image it pointed to before the pull,
# or is removed when there was none (the image itself stays if a container or a tag uses it).
# Best effort, on purpose: it runs on a path that already logs `failed`, and a failed `docker tag`
# or `rmi` changes nothing that runs. If latest stays on the new image, the next run pulls it again
# and backs up before installing it; only a manual `docker compose up -d` with HIPPO_IMAGE_TAG=latest
# could start it without a backup, and the runbook asks to set the hand brake before any manual up.
restore_latest() {
  RESTORE_LATEST=0
  [ "$(image_id "$IMAGE")" = "$PREVIOUS_LATEST_ID" ] && return 0
  if [ -n "$PREVIOUS_LATEST_ID" ]; then
    docker tag "$PREVIOUS_LATEST_ID" "$IMAGE" >/dev/null 2>&1
  else
    docker rmi "$IMAGE" >/dev/null 2>&1
  fi
  return 0
}

# fetch_files: the deployment files and the script at NEW_REVISION, into an emptied staging
# folder. An empty file is a failed fetch.
fetch_files() {
  local f
  rm -rf "${STAGING:?}" && mkdir -p "$STAGING/tailscale/config" || return 1
  for f in $DEPLOY_FILES $SCRIPT_FILE; do
    curl -fsS --max-time 60 --retry 2 -o "$STAGING/$f" "$REPO_URL/$NEW_REVISION/infra/$f" >/dev/null 2>&1 || return 1
    [ -s "$STAGING/$f" ] || return 1
  done
}

# same_files: the fetched deployment files are byte for byte the installed ones.
same_files() {
  local f
  for f in $DEPLOY_FILES; do
    cmp -s "$STAGING/$f" "./$f" || return 1
  done
}

# pull_other_images: pulls the services of the new compose other than hippo, whose image was
# pulled already. Returns 2 when the services cannot be listed, 1 when the pull fails.
pull_other_images() {
  local services service others=''
  services=$(compose_staged config --services 2>/dev/null) || return 2
  for service in $services; do
    [ "$service" = hippo ] && continue
    [[ $service =~ $RE_SERVICE ]] || return 2
    others="$others $service"
  done
  [ -n "$others" ] || return 0
  # Word splitting of $others is wanted: one argument per service.
  # shellcheck disable=SC2086
  compose_staged pull -q $others >/dev/null 2>&1 || return 1
}

# run_backup: `hippo backup` in the running container, with the copy of the installed compose. Its output
# stays in memory: only the backup name is read from it, and checked. Fails on a non-zero exit or
# without a valid backup name.
run_backup() {
  local out
  out=$(compose_current exec -T hippo /hippo backup 2>/dev/null) || return 1
  BACKUP_NAME=$(printf '%s\n' "$out" |
    sed -n 's/.*"backup"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | tail -n 1)
  [[ $BACKUP_NAME =~ $RE_BACKUP ]] || { BACKUP_NAME=''; return 1; }
}

# keep_previous: the installed deployment files, copied to update/previous/.
keep_previous() {
  local f
  rm -rf "${PREVIOUS:?}" && mkdir -p "$PREVIOUS/tailscale/config" || return 1
  for f in $DEPLOY_FILES; do
    [ -f "./$f" ] || continue
    cp -p "./$f" "$PREVIOUS/$f" || return 1
  done
}

# install_file <source> <target> <mode of a new file>: replaces <target> by a rename of a copy
# made in its folder, never by a write in place. An existing target keeps its owner and mode.
install_file() {
  local tmp=$2.update-tmp
  rm -f "$tmp"
  if [ -f "$2" ]; then
    cp -p "$2" "$tmp" && cat "$1" >"$tmp"
  else
    mkdir -p "$(dirname "$2")" && cp "$1" "$tmp" && chmod "$3" "$tmp"
  fi && mv -f "$tmp" "$2" && return 0
  rm -f "$tmp"
  return 1
}

# install_files: the fetched deployment files in place of the installed ones.
install_files() {
  local f
  for f in $DEPLOY_FILES; do
    install_file "$STAGING/$f" "./$f" 644 || return 1
  done
}

# replace_self: the fetched script in place of the running script's own file when it differs, by
# a rename in that file's folder; the running bash keeps reading the old file.
replace_self() {
  local target=$SELF
  cmp -s "$STAGING/$SCRIPT_FILE" "$target" && return 0
  rm -f "$target.new"
  cp "$STAGING/$SCRIPT_FILE" "$target.new" && chmod 700 "$target.new" && mv -f "$target.new" "$target" && return 0
  rm -f "$target.new"
  echo "hippo-update: cannot replace $target with the fetched script; the old one stays." >&2
  return 1
}

# wait_healthy: polls hippo until it is healthy on the new image, at most HEALTH_TIMEOUT seconds.
wait_healthy() {
  local start=$SECONDS id state elapsed pause
  while :; do
    if id=$(hippo_container) &&
      state=$(docker inspect -f '{{.State.Health.Status}} {{.Image}}' "$id" 2>/dev/null) &&
      [ "$state" = "healthy $NEW_ID" ]; then
      return 0
    fi
    elapsed=$((SECONDS - start))
    [ "$elapsed" -ge "$HEALTH_TIMEOUT" ] && return 1
    pause=$HEALTH_INTERVAL
    [ "$pause" -gt $((HEALTH_TIMEOUT - elapsed)) ] && pause=$((HEALTH_TIMEOUT - elapsed))
    sleep "$pause"
  done
}

# path_problem <path>: prints why <path> is not safe for a script run by root, nothing when it
# is safe: a symbolic link, an owner other than OWNER_UID, writable by its group or by others, an
# ACL (a `+` after the mode in `ls -ldn`; the `@` of macOS and the `.` of SELinux are accepted).
# Read with `ls -ldn`, portable to BSD and GNU (no stat).
path_problem() {
  local entry mode uid
  if [ -L "$1" ]; then
    echo "is a symbolic link"
    return
  fi
  entry=$(ls -ldn "$1" 2>/dev/null) || { echo "cannot be listed"; return; }
  read -r mode _ uid _ <<EOF
$entry
EOF
  if [ "$uid" != "$OWNER_UID" ]; then
    echo "belongs to uid $uid, not $OWNER_UID"
  elif [ "${mode:5:1}" = w ]; then
    echo "is writable by its group"
  elif [ "${mode:8:1}" = w ]; then
    echo "is writable by others"
  elif [ "${mode:10:1}" = + ]; then
    echo "has an ACL"
  fi
}

# shown <path>: the path as shown in a message; a path relative to the project folder (the working
# directory) is shown under the project folder's argument, which is never used to reach it.
shown() {
  case $1 in
  .) printf '%s' "$HIPPO_DIR" ;;
  ./*) printf '%s/%s' "$HIPPO_DIR" "${1#./}" ;;
  *) printf '%s' "$1" ;;
  esac
}

# check_ownership: every path the run reads as root or writes to belongs to root alone (D15);
# else a message on standard error that names the path and the cause, and 1. Runs in the project
# folder, before any write and any docker command: the state folder is not trusted yet, so nothing
# is logged. The project folder, docker-compose.yml and .env must exist; the other paths are
# checked when they exist. Not atomic: a file can change between this check and its use, but only
# by someone who can write a folder the check found root's alone (D15, D16).
check_ownership() {
  local path problem
  for path in ./docker-compose.yml ./.env; do
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then
      echo "hippo-update: $(shown "$path") is missing; refusing to run." >&2
      return 1
    fi
  done
  for path in . ./docker-compose.yml ./.env ./.env.example ./tailscale ./tailscale/config \
    ./tailscale/config/serve.json "$STATE_DIR" "$SELF"; do
    [ -e "$path" ] || [ -L "$path" ] || continue
    problem=$(path_problem "$path")
    if [ -n "$problem" ]; then
      echo "hippo-update: $(shown "$path") $problem; it must belong to root alone, refusing to run (docs/runbook-nas.md)." >&2
      return 1
    fi
  done
}

# update: one run once the lock is held (design D3, steps 3 to 15). Returns the exit status.
update() {
  local tag id status

  # Hand brake (D5).
  tag=$(read_image_tag) || { fail no_image_tag; return 1; }
  if [ "$tag" != latest ]; then
    log_event paused duration_s "$SECONDS"
    return 0
  fi

  # What runs now.
  id=$(hippo_container) || { fail hippo_not_running; return 1; }
  RUNNING_ID=$(docker inspect -f '{{.Image}}' "$id" 2>/dev/null)
  [[ $RUNNING_ID =~ $RE_IMAGE_ID ]] || { fail hippo_not_running; return 1; }
  OLD_REVISION=$(revision_of "$RUNNING_ID")
  PREVIOUS_LATEST_ID=$(image_id "$IMAGE")

  # The published image, its revision and the files of its commit (D2).
  RESTORE_LATEST=1
  docker pull -q "$IMAGE" >/dev/null 2>&1 || { fail pull_failed; return 1; }
  NEW_ID=$(image_id "$IMAGE")
  [ -n "$NEW_ID" ] || { fail pull_failed; return 1; }
  NEW_REVISION=$(revision_of "$NEW_ID")
  [ "$NEW_REVISION" != unknown ] || { fail bad_revision; return 1; }
  fetch_files || { fail fetch_failed; return 1; }

  # Nothing new (D4): the latest tag already points to the running image.
  if [ "$NEW_ID" = "$RUNNING_ID" ] && same_files; then
    RESTORE_LATEST=0
    log_event up-to-date old_revision "$OLD_REVISION" new_revision "$NEW_REVISION" duration_s "$SECONDS"
    return 0
  fi

  # Files that do not fit .env (D5); the output of docker compose config is discarded.
  MISSING=$(missing_variables)
  [ -z "$MISSING" ] || { fail missing_variables; return 1; }
  compose_staged config --quiet >/dev/null 2>&1 || { fail compose_invalid; return 1; }

  # The other images, then the backup with the running version.
  pull_other_images
  status=$?
  [ "$status" = 2 ] && { fail compose_invalid; return 1; }
  [ "$status" = 0 ] || { fail pull_failed; return 1; }
  run_backup || { fail backup_failed; return 1; }

  # Point of no return: nothing below is undone.
  RESTORE_LATEST=0
  printf '%s\n' "$NEW_REVISION" >"$MARKER" || { fail install_failed; return 1; }
  keep_previous || { fail install_failed; return 1; }
  install_files || { fail install_failed; return 1; }
  # From here on, Compose reads the fetched compose, never the file just installed in the share.
  CURRENT_COMPOSE=$STAGING/docker-compose.yml
  compose_current up -d >/dev/null 2>&1 || { fail up_failed; return 1; }
  wait_healthy || { fail unhealthy; return 1; }

  # After a successful update only (D6): logged first, then the script replaces itself. A failed
  # replacement is not an error of the update, which is done and healthy: the old script stays,
  # and the next update tries again, since the fetched script still differs from it.
  log_event updated old_revision "$OLD_REVISION" new_revision "$NEW_REVISION" \
    backup "$BACKUP_NAME" duration_s "$SECONDS"
  replace_self || true
  return 0
}

main() {
  local status self_dir
  umask 077
  # The project folder: the only argument, an absolute path to an existing folder.
  if [ $# -ne 1 ]; then
    echo "usage: hippo-update /<absolute path of the project folder>" >&2
    return 1
  fi
  HIPPO_DIR=$1
  while [ "$HIPPO_DIR" != / ] && [ "${HIPPO_DIR%/}" != "$HIPPO_DIR" ]; do HIPPO_DIR=${HIPPO_DIR%/}; done
  case $HIPPO_DIR in
  /*) ;;
  *)
    echo "hippo-update: the project folder must be an absolute path: $1" >&2
    return 1 ;;
  esac
  # This script's own file, by an absolute path (no readlink -f), resolved before leaving the
  # working directory $0 may be relative to: replaced after an update.
  if ! self_dir=$(cd "$(dirname "$0")" && pwd -P); then
    echo "hippo-update: cannot resolve the folder of $0." >&2
    return 1
  fi
  SELF=$self_dir/$(basename "$0")
  STATE_DIR=${HIPPO_UPDATE_STATE_DIR:-$DEFAULT_STATE_DIR}
  case $STATE_DIR in
  /*) ;;
  *)
    echo "hippo-update: the state folder must be an absolute path: $STATE_DIR" >&2
    return 1 ;;
  esac
  if [ -L "$HIPPO_DIR" ]; then
    echo "hippo-update: $HIPPO_DIR is a symbolic link; it must belong to root alone, refusing to run (docs/runbook-nas.md)." >&2
    return 1
  fi
  if [ ! -d "$HIPPO_DIR" ]; then
    echo "hippo-update: $HIPPO_DIR is not a folder." >&2
    return 1
  fi
  # From here on, the project folder is the working directory and is reached by relative paths
  # only; HIPPO_DIR serves the messages (D16).
  if ! cd -P -- "$HIPPO_DIR"; then
    echo "hippo-update: cannot enter $HIPPO_DIR." >&2
    return 1
  fi
  OWNER_UID=${HIPPO_UPDATE_OWNER_UID:-0}
  [[ $OWNER_UID =~ $RE_NUMBER ]] || OWNER_UID=0
  RUN_DIR=$STATE_DIR/run
  RUN_COMPOSE=$RUN_DIR/docker-compose.yml
  RUN_ENV=$RUN_DIR/.env
  CURRENT_COMPOSE=$RUN_COMPOSE
  # HIPPO_IMAGE_TAG and the variable names are read from the copy of .env too.
  ENV_FILE=$RUN_ENV
  LOG=$STATE_DIR/update.log
  STAGING=$STATE_DIR/staging
  PREVIOUS=$STATE_DIR/previous
  MARKER=$STATE_DIR/installing
  REPO_URL=${HIPPO_UPDATE_REPO_URL:-$DEFAULT_REPO_URL}
  HEALTH_TIMEOUT=${HIPPO_UPDATE_HEALTH_TIMEOUT:-180}
  [[ $HEALTH_TIMEOUT =~ $RE_NUMBER ]] || HEALTH_TIMEOUT=180
  HEALTH_INTERVAL=${HIPPO_UPDATE_HEALTH_INTERVAL:-5}
  [[ $HEALTH_INTERVAL =~ $RE_NUMBER ]] || HEALTH_INTERVAL=5

  check_ownership || return 1
  if ! command -v flock >/dev/null 2>&1; then
    echo "hippo-update: flock is missing (util-linux); see docs/runbook-nas.md." >&2
    return 1
  fi
  if ! { mkdir -p "$STATE_DIR" && chmod 700 "$STATE_DIR"; }; then
    echo "hippo-update: cannot create $STATE_DIR." >&2
    return 1
  fi
  # The lock is held by file descriptor 9 until the script exits.
  if ! exec 9>"$STATE_DIR/lock"; then
    echo "hippo-update: cannot open $STATE_DIR/lock." >&2
    return 1
  fi
  if ! flock -n 9; then
    log_event locked
    return 1
  fi
  # The copy of .env (0600, system disk) lives only for the run, whatever its outcome.
  trap 'rm -rf "$RUN_DIR"' EXIT
  if ! copy_run_files; then
    echo "hippo-update: cannot copy docker-compose.yml and .env into $RUN_DIR." >&2
    return 1
  fi

  rotate_log
  report_interrupted
  update
  status=$?
  rm -f "$MARKER"
  return "$status"
}

main "$@"; exit $?
