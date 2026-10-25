#!/bin/bash
# Regression tests for protect-files.sh. Run: bash .claude/hooks/protect-files.test.sh
H="${1:-$(dirname "$0")/protect-files.sh}"
case "$H" in /*) ;; *) H="$PWD/$H" ;; esac
ROOT=$(mktemp -d)/hippo
mkdir -p "$ROOT"
trap 'rm -rf "$(dirname "$ROOT")"' EXIT
export CLAUDE_PROJECT_DIR="$ROOT"
fail=0; n=0
NL=$'\n'

# file <expected> <tool> <path>
file() {
  n=$((n+1))
  jq -n --arg t "$2" --arg f "$3" '{tool_name:$t, tool_input:{file_path:$f}}' | "$H" 2>/dev/null
  local got=$?
  [ "$got" = "$1" ] || { echo "FAIL (got $got, want $1): $2 $3"; fail=1; }
}
# cmd <expected> <command>
cmd() {
  n=$((n+1))
  jq -n --arg c "$2" '{tool_name:"Bash", tool_input:{command:$c}}' | "$H" 2>/dev/null
  local got=$?
  [ "$got" = "$1" ] || { echo "FAIL (got $got, want $1): $2"; fail=1; }
}

# Secrets, through the file tools.
for t in Read Edit Write; do
  file 2 $t "$ROOT/infra/.env"
  file 2 $t "$ROOT/.env"
  file 2 $t "$ROOT/app/.env.local"
  file 2 $t "$ROOT/.claude/settings.local.json"
  file 0 $t "$ROOT/infra/.env.example"
done
file 0 Read "$ROOT/.claude/settings.json"
file 0 Read "$ROOT/app/src/env.ts"

# Generated files: readable, never edited.
file 0 Read "$ROOT/backend/internal/httpapi/api.gen.go"
file 2 Edit "$ROOT/backend/internal/httpapi/api.gen.go"
file 2 Write "$ROOT/app/src/api/schema.gen.ts"
file 2 Edit "$ROOT/backend/internal/analysis/schema/classify-item.bundled.json"
file 0 Edit "$ROOT/backend/internal/httpapi/server.go"
file 0 Edit "$ROOT/docs/prompt/schemas/classify-item.schema.json"

# Sibling repositories are read-only.
file 0 Read "$(dirname "$ROOT")/geomni/AGENTS.md"
file 2 Edit "$(dirname "$ROOT")/geomni/AGENTS.md"
file 2 Write "$(dirname "$ROOT")/wifisense/CLAUDE.md"
file 0 Write "$HOME/.claude/projects/x/memory/note.md"
file 0 Write "/private/tmp/claude-501/scratch/x.md"

# Secrets, through a shell command.
cmd 2 'cat infra/.env'
cmd 2 'cat .env'
cmd 2 'source ./.env && go run ./cmd/hippo'
cmd 2 'grep TOKEN infra/.env.local'
cmd 2 'docker compose --env-file infra/.env up -d'
cmd 2 'cp infra/.env.example infra/.env'
cmd 2 'cat .claude/settings.local.json'
cmd 0 'cat infra/.env.example'
cmd 0 'git grep "process.env" app/src'
cmd 0 'ls -la .envrc'
cmd 0 'make up'
cmd 0 'docker compose --env-file=x up'
cmd 0 "cat > infra/CLAUDE.md <<'EOF'${NL}.env is created by Ghislain; agents never read infra/.env${NL}EOF"
cmd 0 'git commit -m "docs: explain why .env is never read"'
cmd 0 'git commit -m "chore: ignore .claude/settings.local.json"'
cmd 2 "cat <<'EOF' > infra/.env${NL}X=1${NL}EOF"
cmd 2 'cat "infra/.env"'
cmd 2 'echo "$(cat infra/.env)"'
cmd 0 'git commit -m "warn about rm -rf ../geomni in docs"'

# Commands that print .env without naming it.
cmd 2 'grep -rn API_TOKEN infra'
cmd 2 'grep -R TOKEN .'
cmd 2 'grep --recursive TS_AUTHKEY infra'
cmd 2 'cd infra && grep -ril secret .'
cmd 2 'rg --no-ignore API_TOKEN'
cmd 2 'rg -uu TOKEN infra'
cmd 2 'docker compose -f infra/docker-compose.yml config'
cmd 2 'docker compose config --format json'
cmd 2 'docker inspect hippo-local-hippo-1'
cmd 2 'docker container inspect hippo-local-hippo-1'
cmd 0 'grep -n API_TOKEN infra/.env.example'
cmd 0 'grep -n TOKEN backend/internal/config/config.go'
cmd 0 'git grep -n API_TOKEN'
cmd 0 'rg -n API_TOKEN infra'
cmd 0 'rg --hidden -n TOKEN .claude'
cmd 0 'docker compose -f infra/docker-compose.yml config --quiet'
cmd 0 'API_TOKEN=check docker compose -f infra/docker-compose.local.yml config -q'
cmd 0 'docker ps --format "{{.Names}}"'
cmd 0 'make check-infra'

# Paths are normalised before the sibling check.
file 2 Edit "$ROOT/../geomni/AGENTS.md"
file 2 Write "$ROOT/backend/../../wifisense/CLAUDE.md"
file 0 Edit "$ROOT/backend/../docs/glossary.md"

# Recursive rm.
cmd 0 'rm -rf backend/bin'
cmd 0 'rm -rf app/node_modules app/.expo'
cmd 0 "rm -rf $ROOT/backend/bin"
cmd 0 'rm -rf /private/tmp/claude-501/scratch/out'
cmd 0 'rm -f backend/coverage.out'
cmd 0 'rm build/x.txt'
cmd 2 'rm -rf ../geomni'
cmd 2 'rm -rf ~/dev'
cmd 2 'rm -rf /Users'
cmd 2 'rm -r -f /opt/homebrew'
cmd 2 'rm --recursive /etc'
# shellcheck disable=SC2016 # the $ is the case under test
cmd 2 'cd app && rm -rf $HOME'
cmd 2 'rm -Rf backend/../../wifisense'

if [ "$fail" = 0 ]; then echo "ALL $n CASES OK"; else exit 1; fi
