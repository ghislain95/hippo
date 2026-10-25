#!/bin/bash
# PreToolUse hook (Read, Edit, Write, MultiEdit, NotebookEdit, Bash): protects what an agent must
# never touch. Rules: docs/dev/workflow.md ("Garde-fous").
# - Secrets: .env and .env.* (except .env.example) and .claude/settings.local.json are never read
#   nor written, neither through the file tools nor through a shell command that names them.
# - Generated files (*.gen.go, *.gen.ts, the bundled fiche schema) are never edited by hand:
#   regenerate them with `make generate`.
# - Sibling repositories (../geomni, ../wifisense, any folder next to this one) are read-only.
# - A recursive rm whose target leaves the repository is blocked.
# A guardrail against mistakes, not a sandbox: deliberate evasions are out of scope.
# Exit 2 = block the tool call and show stderr to the agent.

INPUT=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  echo "protect-files: jq not found — install it (brew install jq)." >&2
  exit 2
fi

TOOL=$(printf '%s' "$INPUT" | jq -r '.tool_name // empty')
ROOT=${CLAUDE_PROJECT_DIR:-$(printf '%s' "$INPUT" | jq -r '.cwd // empty')}
ROOT=${ROOT%/}
PARENT=$(dirname "$ROOT")

block() {
  echo "BLOCKED by .claude/hooks/protect-files.sh: $1" >&2
  exit 2
}

# is_secret <path>: .env, .env.<anything> except .env.example, settings.local.json.
is_secret() {
  local base
  base=$(basename "$1")
  case "$base" in
    .env.example) return 1 ;;
    .env | .env.*) return 0 ;;
    settings.local.json) case "$1" in *.claude/settings.local.json) return 0 ;; esac ;;
  esac
  return 1
}

is_generated() {
  case "$1" in
    *.gen.go | *.gen.ts | */internal/analysis/schema/classify-item.bundled.json) return 0 ;;
  esac
  return 1
}

# absolute <path>: the path made absolute against the repository root (no symlink resolution).
absolute() {
  case "$1" in
    /*) printf '%s' "$1" ;;
    '~'*) printf '%s' "$HOME${1#\~}" ;;
    *) printf '%s' "$ROOT/$1" ;;
  esac
}

case "$TOOL" in
  Read | Edit | Write | MultiEdit | NotebookEdit)
    FILE=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // .tool_input.notebook_path // empty')
    [ -z "$FILE" ] && exit 0
    if is_secret "$FILE"; then
      block "'$FILE' holds secrets: agents never read nor write it. Ask Ghislain, or use .env.example for the documented variables."
    fi
    [ "$TOOL" = Read ] && exit 0
    if is_generated "$FILE"; then
      block "'$FILE' is generated: never edit it by hand. Change its source (api/openapi.yaml, docs/prompt/schemas/) and run 'make generate'."
    fi
    ABS=$(absolute "$FILE")
    case "$ABS" in
      "$ROOT"/*) ;;
      "$PARENT"/*) block "'$FILE' is in a sibling repository, which is read-only from Hippo." ;;
    esac
    ;;
  Bash)
    COMMAND=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty')
    [ -z "$COMMAND" ] && exit 0
    # Any word that names a .env file other than .env.example.
    while IFS= read -r word; do
      [ -z "$word" ] && continue
      [ "$word" = ".env.example" ] && continue
      block "the command names '$word', which holds secrets. Use the make targets (make up, make dev), which load it without showing it."
    done < <(printf '%s' "$COMMAND" | grep -oE '(^|[[:space:]/"'"'"'=:<>])\.env(\.[A-Za-z0-9_-]+)?([[:space:]"'"'"';|&)<>]|$)' | grep -oE '\.env(\.[A-Za-z0-9_-]+)?')
    if printf '%s' "$COMMAND" | grep -qE 'settings\.local\.json'; then
      block "the command names .claude/settings.local.json, which holds the GH_TOKEN."
    fi
    # Recursive rm: every target must stay inside the repository.
    while IFS= read -r segment; do
      printf '%s' "$segment" | grep -qE '(^|[[:space:]])rm[[:space:]]' || continue
      printf '%s' "$segment" | grep -qE '[[:space:]](-[a-zA-Z]*[rR][a-zA-Z]*|--recursive)([[:space:]]|$)' || continue
      for arg in $(printf '%s' "$segment" | sed -E 's/.*(^|[[:space:]])rm[[:space:]]+//'); do
        case "$arg" in -*) continue ;; esac
        arg=${arg//\"/}
        arg=${arg//\'/}
        case "$arg" in
          *'$'* | *'`'*) block "recursive rm with a target the hook cannot resolve: '$arg'. Write the path literally." ;;
          *..*) block "recursive rm with a '..' target: '$arg'. Write the path from the repository root." ;;
          '~'* | /*)
            ABS=$(absolute "$arg")
            case "$ABS" in
              "$ROOT"/?* | /tmp/?* | /private/tmp/?* | /var/folders/?* | /private/var/folders/?*) ;;
              *) block "recursive rm outside the repository: '$arg'." ;;
            esac
            ;;
        esac
      done
    done < <(printf '%s\n' "$COMMAND" | tr ';&|' '\n')
    ;;
esac
exit 0
