#!/bin/bash
# PostToolUse hook (Edit, Write, MultiEdit): formats the file an agent just wrote, so formatting
# never costs a lint round-trip. Go files go through `golangci-lint fmt` (gofumpt + goimports, as
# configured in backend/.golangci.yml); app/ and api/ files through the project's Prettier.
# Never blocks: a missing tool or a formatter error is reported, not fatal (the pre-commit hook
# and the CI check formatting anyway).

INPUT=$(cat)
command -v jq >/dev/null 2>&1 || exit 0
FILE=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty')
ROOT=${CLAUDE_PROJECT_DIR:-$(printf '%s' "$INPUT" | jq -r '.cwd // empty')}
[ -z "$FILE" ] || [ ! -f "$FILE" ] && exit 0
case "$FILE" in /*) ;; *) FILE="$ROOT/$FILE" ;; esac

warn() {
  echo "format-on-edit: $1" >&2
  exit 0
}

case "$FILE" in
  *.gen.go | *.gen.ts) exit 0 ;;
  "$ROOT"/backend/*.go)
    command -v golangci-lint >/dev/null 2>&1 || warn "golangci-lint not found, $FILE left unformatted"
    (cd "$ROOT/backend" && golangci-lint fmt "$FILE" >/dev/null 2>&1) || warn "golangci-lint fmt failed on $FILE"
    ;;
  "$ROOT"/app/*.ts | "$ROOT"/app/*.tsx | "$ROOT"/app/*.js | "$ROOT"/app/*.json | "$ROOT"/app/*.md | "$ROOT"/app/*.yml | "$ROOT"/app/*.yaml | \
  "$ROOT"/api/*.yaml | "$ROOT"/api/*.yml | "$ROOT"/api/*.json)
    pkg="$ROOT/$(printf '%s' "${FILE#"$ROOT"/}" | cut -d/ -f1)"
    [ -x "$pkg/node_modules/.bin/prettier" ] || exit 0
    (cd "$pkg" && ./node_modules/.bin/prettier --write --log-level warn "$FILE" >/dev/null) || warn "prettier failed on $FILE"
    ;;
esac
exit 0
