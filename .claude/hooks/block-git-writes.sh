#!/bin/bash
# PreToolUse hook (Bash): git guardrail for agents. The rules it enforces: docs/dev/git.md.
# Agents commit and push feature branches; main only moves through a merged PR.
# Blocked: commit/merge/revert/cherry-pick on main; pushing main (also through HEAD, @ or a
# $-expanded ref), pushing without an explicit remote and branch, force-push; pull without
# --ff-only, with rebase, or (on main) of another branch; history rewriting (rebase, amend, am,
# commit-tree, filter-branch, reset to a revision, branch -f); sweeping stages (add -A/./*/-u,
# commit -a); discarding work; --no-verify; `gh pr merge` other than a plain merge commit
# (`--merge`, optionally `--auto`: main requires ci-ok server-side, ADR-0034; never --admin).
# The branch is the one checked out where the command really runs: the tool call's cwd, then each
# `cd`/`pushd`/`popd` and `git -C <dir>` (subshells restore the directory), and a
# `git switch|checkout <branch>` earlier in the same command — to main at once, to another branch
# only when the next command is chained with && (a failed switch stops the chain). A directory or
# branch that cannot be resolved ($-expanded, `cd -`, a directory that does not exist) counts as main.
# Before the checks, the command is read the way the shell reads it (block-git-writes.pl): only
# what the shell runs is left, commit messages and PR bodies are never read as flags or commands.
# Adapted from mattpocock/skills (misc/git-guardrails-claude-code, MIT), via geomni and wifisense.
# A guardrail against mistakes, not a sandbox: deliberate evasions (sh -c "...", eval,
# `git --git-dir x commit`) are out of scope.
# Exit 2 = block the tool call and show stderr to the agent. Exit 1 = non-blocking error shown to the user.

INPUT=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  if printf '%s' "$INPUT" | grep -qE '(^|[^[:alnum:]_-]|\\[nt])(git|gh)[[:space:]]'; then
    echo "block-git-writes: jq not found — git and gh commands stay blocked until jq is installed (brew install jq)." >&2
    exit 2
  fi
  exit 0
fi

COMMAND=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty')
CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty')
[ -z "$COMMAND" ] && exit 0
set -f

# Read the command the way the shell does (block-git-writes.pl, see its header). Without perl,
# only backslash-continued lines are joined. If the reader fails, git and gh commands are blocked.
HOOK_DIR=$(cd "$(dirname "$0")" && pwd)
if command -v perl >/dev/null 2>&1; then
  if ! NEUTRAL=$(printf "%s" "$COMMAND" | perl "$HOOK_DIR/block-git-writes.pl"); then
    if printf "%s" "$COMMAND" | grep -qE "(^|[^[:alnum:]_-])(git|gh)[[:space:]]"; then
      echo "block-git-writes: the shell reader (block-git-writes.pl) failed — git and gh commands are blocked." >&2
      exit 2
    fi
    exit 0
  fi
else
  NEUTRAL=${COMMAND//$'\\\n'/ }
fi

# Where a command starts: line start, after ; & | ( { ! or $(, after a shell keyword
# (if/then/do/else/while/until) or a wrapper (env, command, exec, nohup, time, sudo, xargs,
# timeout N), or after a VAR=value assignment.
START='(^|[;&|({!]|\$\(|(^|[[:space:]])((if|then|do|else|while|until|env|command|exec|nohup|time|sudo|xargs)|timeout[[:space:]]+[0-9]+[smhd]?|[A-Za-z_][A-Za-z0-9_]*=("[^"]*"|'"'"'[^'"'"']*'"'"'|[^[:space:]]*))[[:space:]])'
# A path or option value: double-quoted, single-quoted, or a word with backslash escapes.
VALUE='("([^"\\]|\\.)*"|'"'"'[^'"'"']*'"'"'|(\\.|[^[:space:]\\])+)'
# Global options before the subcommand: -C dir, -c key=value, --long[=value], -p/-P.
GOPT="[[:space:]]+(-[cC][[:space:]]+${VALUE}|--[a-z-]+(=[^[:space:]]+)?|-[pP])"
# A git invocation at a command position, optionally via rtk (retired, kept defensively) or an absolute path.
GIT="${START}[[:space:]]*(rtk[[:space:]]+)?([^[:space:];&|()]*/)?git(${GOPT})*[[:space:]]+"
# gh global options before the subcommand: -R/--repo <owner/repo>.
GHOPT="[[:space:]]+(-R([[:space:]]*|=)${VALUE}|--repo(=|[[:space:]]+)${VALUE})"
GH="${START}[[:space:]]*([^[:space:];&|()]*/)?gh(${GHOPT})*[[:space:]]+"
# End of a subcommand word: whitespace, an operator, a redirection, or end of line.
END='([[:space:]]|[;&|)<>]|$)'
# Branches that only move through a merged PR.
PROTECTED='^(main|master)$'
# Inside one command segment: git's global options, its subcommand, the rest.
GITCMD_RE="(^|[[:space:]/])git((${GOPT})*)[[:space:]]+([a-z][a-z-]*)(.*)"
C_RE="-C[[:space:]]+${VALUE}(.*)"
KEYWORDS='^[[:space:]]*((if|then|do|else|while|until|!)[[:space:]]+)*'
CD_RE="${KEYWORDS}(cd|pushd)(([[:space:]]+-[LPe@]+)*)([[:space:]]+${VALUE})?([[:space:]]|$)"
POPD_RE="${KEYWORDS}popd([[:space:]]|$)"

# `git [-C dir] merge|rebase|cherry-pick|revert|am --abort|--quit` only leaves an operation in
# progress (e.g. a conflict the user opened): it is neutralised before the checks, the rest of the
# command is still checked (`git rebase --abort && git push` stays blocked).
CHECKED=$(printf '%s' "$NEUTRAL" | sed -E 's/(^|[^[:alnum:]_.\/-])git([[:space:]]+-[cC][[:space:]]+[^[:space:]]+)*[[:space:]]+(merge|rebase|cherry-pick|revert|am)[[:space:]]+--(abort|quit)([[:space:]]|[;&|)]|$)/\1true\5/g')

block() {
  echo "BLOCKED by .claude/hooks/block-git-writes.sh: '$COMMAND' — $1. Hippo git rules: docs/dev/git.md." >&2
  exit 2
}

# check <pattern> <reason>: blocks when the pattern matches anywhere in the command.
check() {
  printf '%s' "$CHECKED" | grep -qE -- "$1"
  local rc=$?
  [ "$rc" -eq 0 ] && block "$2"
  if [ "$rc" -ne 1 ]; then
    echo "block-git-writes: pattern error (grep exit $rc) on '$1' — the guardrail is degraded." >&2
    exit 1
  fi
}

# matches <segment> <pattern>
matches() {
  printf '%s' "$1" | grep -qE -- "$2"
}

unquote() {
  local w=${1//\"/}
  printf '%s' "${w//\'/}"
}

# resolve_dir <base> <path>: the directory a cd/pushd/-C lands in, or "?" when it cannot be known.
resolve_dir() {
  local base=$1 p=$2
  [ "$base" = "?" ] && { echo "?"; return; }
  case "$p" in
    \"*\") p=${p#\"}; p=${p%\"} ;;
    \'*\') p=${p#\'}; p=${p%\'} ;;
    *) p=$(printf '%s' "$p" | sed -E 's/\\(.)/\1/g') ;;
  esac
  case "$p" in *'$'*|*'`'*|-|'') echo "?"; return ;; esac
  case "$p" in '~') p=$HOME ;; '~/'*) p="$HOME/${p#\~/}" ;; esac
  case "$p" in /*) ;; *) p="$base/$p" ;; esac
  [ -d "$p" ] || { echo "?"; return; }
  echo "$p"
}

top_of() {
  [ "$1" = "?" ] && { echo "?"; return; }
  git -C "$1" rev-parse --show-toplevel 2>/dev/null || echo "$1"
}

# A `git switch|checkout` earlier in the same command overrides the checked-out branch of its repo.
OV_TOP=
OV_BRANCH=
branch_of() {
  local top
  top=$(top_of "$1")
  [ "$top" = "?" ] && { echo "?"; return; }
  if [ -n "$OV_TOP" ] && [ "$top" = "$OV_TOP" ]; then
    echo "$OV_BRANCH"
    return
  fi
  git -C "$top" symbolic-ref --quiet --short HEAD 2>/dev/null
}

protected() {
  [ "$1" = "?" ] || [[ "$1" =~ $PROTECTED ]]
}

describe() {
  if [ "$1" = "?" ]; then echo "an unresolvable directory or branch (counts as main)"; else echo "$1"; fi
}

# track_switch <dir> <words...>
track_switch() {
  local dir=$1 w new= target= pos=0 top
  shift
  while [ $# -gt 0 ]; do
    w=$(unquote "$1")
    shift
    case "$w" in
      '>'|'>>'|[0-9]'>'|[0-9]'>>'|'<') shift ;;
      [0-9]'>'*|'>'*|'<'*) ;;
      -) pos=$((pos + 1)); [ -z "$target" ] && target=- ;;
      -c|-b|--create|--orphan) new=$(unquote "$1"); shift ;;
      --detach|-d) target=DETACHED ;;
      --) return ;;
      -*) ;;
      *) pos=$((pos + 1)); [ -z "$target" ] && target=$w ;;
    esac
  done
  top=$(top_of "$dir")
  if [ -n "$new" ]; then
    case "$new" in *'$'*) new="?" ;; esac
    switch_to "$top" "$new"
    return
  fi
  # `checkout <tree-ish> <paths>` restores files, it does not switch.
  [ "$pos" -ge 2 ] && return
  case "$target" in
    '') return ;;
    DETACHED) switch_to "$top" "" ;;
    -|*'$'*) switch_to "$top" "?" ;;
    *)
      if [[ "$target" =~ $PROTECTED ]] \
        || git -C "$top" show-ref --verify --quiet "refs/heads/$target" 2>/dev/null \
        || [ -n "$(git -C "$top" for-each-ref --count=1 --format='%(refname)' "refs/remotes/*/$target" 2>/dev/null)" ]; then
        switch_to "$top" "$target"
      fi
      ;;
  esac
}

# switch_to <top> <branch>: a switch to main (or to an unknown branch) counts at once; to another
# branch it stays pending until the next command is chained with &&.
PEND=0
PEND_TOP=
PEND_BRANCH=
switch_to() {
  if protected "$2"; then
    OV_TOP=$1
    OV_BRANCH=$2
    PEND=0
  else
    PEND=1
    PEND_TOP=$1
    PEND_BRANCH=$2
  fi
}

# check_reset <dir> <words...>: `reset <commit>` (other than HEAD) moves the branch back.
check_reset() {
  local dir=$1 w pos=0 rev= top
  shift
  for w in "$@"; do
    w=$(unquote "$w")
    case "$w" in
      --) return ;;
      -*|[0-9]'>'*|'>'*|'<'*) ;;
      *) pos=$((pos + 1)); [ "$pos" = 1 ] && rev=$w ;;
    esac
  done
  [ "$pos" = 1 ] || return
  case "$rev" in HEAD|@) return ;; esac
  top=$(top_of "$dir")
  [ "$top" = "?" ] && return
  git -C "$top" rev-parse --verify --quiet "$rev^{commit}" >/dev/null 2>&1 \
    && block "never move a branch back: add a new commit (git revert)"
}

# check_pull <dir> <words...>: --ff-only only, no rebase; on main, only main itself.
check_pull() {
  local dir=$1 w pos=0 second= br ref
  shift
  case " $* " in *" --ff-only "*) ;; *) block "pull only with --ff-only" ;; esac
  case " $* " in *" --rebase"*|*" -r "*) block "never pull with rebase" ;; esac
  for w in "$@"; do
    w=$(unquote "$w")
    case "$w" in
      -*|[0-9]'>'*|'>'*|'<'*) ;;
      *) pos=$((pos + 1)); [ "$pos" = 2 ] && second=$w ;;
    esac
  done
  [ -z "$second" ] && return
  br=$(branch_of "$dir")
  if protected "$br"; then
    ref=${second%%:*}
    ref=${ref#+}
    ref=${ref#refs/heads/}
    [[ "$ref" =~ $PROTECTED ]] || block "on $(describe "$br"), pull only main itself: git pull --ff-only [origin main]"
  fi
}

# check_push <dir> <words...>: explicit remote and branch, never main, never forced.
check_push() {
  local dir=$1 w ref positional=0 skip=0 br= br_read=0
  shift
  for w in "$@"; do
    if [ "$skip" = 1 ]; then
      skip=0
      continue
    fi
    w=$(unquote "$w")
    case "$w" in
      '') ;;
      --force*|--mirror|--all|--prune) block "never force-push nor push every ref" ;;
      -o|--push-option|--receive-pack|--exec) skip=1 ;;
      --repo) skip=1; positional=$((positional + 1)) ;;
      --repo=*) positional=$((positional + 1)) ;;
      --*) ;;
      -*f*) block "never force-push" ;;
      -*) ;;
      '>'|'>>'|[0-9]'>'|[0-9]'>>'|'<') skip=1 ;;
      [0-9]'>'*|'>'*|'<'*) ;;
      *)
        positional=$((positional + 1))
        [ "${w#+}" != "$w" ] && block "never force-push (+refspec)"
        [ "$positional" -lt 2 ] && continue
        ref=${w##*:}
        ref=${ref#refs/heads/}
        case "$ref" in
          HEAD|@|*'$'*)
            if [ "$br_read" = 0 ]; then br=$(branch_of "$dir"); br_read=1; fi
            protected "$br" && block "never push $(describe "$br"): it only moves through a merged PR"
            ;;
        esac
        [[ "$ref" =~ $PROTECTED ]] && block "never push $ref: it only moves through a merged PR"
        ;;
    esac
  done
  [ "$positional" -lt 2 ] && block "name the remote and the branch explicitly: git push -u origin <branch>"
}

# check_gh_merge <segment>: a merge commit, optionally deferred with --auto (ADR-0034), never
# --admin. `--disable-auto` alone (stopping a deferred merge) needs no merge method.
check_gh_merge() {
  case " $1 " in *" --disable-auto"*)
    case " $1 " in *" --auto"*|*" --admin"*|*" --squash"*|*" -s "*|*" --rebase"*|*" -r "*) ;; *) return ;; esac
    ;;
  esac
  case " $1 " in *" --squash"*|*" -s "*|*" --rebase"*|*" -r "*) block "merge PRs with a merge commit only: gh pr merge <n> --merge" ;; esac
  # --auto is allowed (ADR-0034): the ruleset of main requires ci-ok, so GitHub merges only once the
  # CI is green. --admin would bypass that ruleset.
  case " $1 " in *" --admin"*) block "never bypass the merge conditions (--admin)" ;; esac
  case " $1 " in *" --merge"*|*" -m "*) ;; *) block "merge PRs with a merge commit only: gh pr merge <n> --merge" ;; esac
}

# Merging through the REST or GraphQL API skips the checks of `gh pr merge`. The REST route is
# a word of the command as the shell reads it; a GraphQL mutation sits inside a quoted string,
# so it is looked for in the raw command, but only when `gh api graphql` really runs (a heredoc
# or a message that merely mentions it is not a call). The mutation name is searched in the whole
# raw command, so `gh api graphql … && git commit -m "…mergePullRequest…"` is blocked too: rare,
# accepted. A query read from a file (-f query=@file) is out of scope: a deliberate step.
# Even a read (GET) of the merge route is blocked; gh pr view reads the state.
if printf '%s' "$CHECKED" | grep -qE "${GH}api[[:space:]]([^;&|]*[[:space:]])?[\"']?([^[:space:]\"']*/)?repos/[^[:space:]]*pulls/[^[:space:]/]+/merge"; then
  block "merge PRs with gh pr merge <n> --merge (or --auto --merge), not through gh api; to read the state: gh pr view <n> --json state,mergedAt"
fi
if printf '%s' "$CHECKED" | grep -qE "${GH}api[[:space:]]([^;&|]*[[:space:]])?graphql${END}" &&
  printf '%s' "$COMMAND" | grep -qE 'mergePullRequest|enablePullRequestAutoMerge'; then
  block "merge PRs with gh pr merge <n> --merge (or --auto --merge), not through gh api graphql"
fi

# --- Blocked wherever they run.
# History rewriting and raw commit creation.
check "${GIT}(rebase|am|commit-tree|filter-branch|filter-repo)${END}" "history rewriting is never delegated"
check "${GIT}commit[[:space:]][^;&|]*--amend" "never amend a commit: add a new one"
check "${GIT}reset[[:space:]]([^;&|]*[[:space:]])?[\"']?(--soft|--mixed|--hard|--keep|--merge|[^[:space:];&|]*[~^][^[:space:];&|]*|[^[:space:];&|]*@\{[^[:space:];&|]*)[\"']?${END}" "never move a branch back: add a new commit (git revert)"
# Sweeping stages: commit explicit paths only (parallel sessions share checkouts).
check "${GIT}commit[[:space:]]([^;&|]*[[:space:]])?(-[a-zA-Z]*a[a-zA-Z]*|--all)${END}" "commit -a sweeps every tracked change: stage explicit paths"
check "${GIT}add[[:space:]]([^;&|]*[[:space:]])?(-A|--all|-u|--update|\.|\./|\.\.|\.\./|\*|:/)${END}" "stage explicit paths, never the whole tree"
# Discarding work.
check "${GIT}clean[[:space:]]([^;&|]*[[:space:]])?(-[a-zA-Z]*f|--force)" "never discard work"
check "${GIT}checkout[[:space:]]([^;&|]*[[:space:]])?(\.|-f|--force|-B)${END}" "never discard work"
check "${GIT}switch[[:space:]]([^;&|]*[[:space:]])?(-f|--force|--discard-changes|-C|--force-create)${END}" "never discard work"
check "${GIT}worktree[[:space:]]+remove[[:space:]]([^;&|]*[[:space:]])?(-f|--force)${END}" "never discard work"
check "${GIT}restore[[:space:]]([^;&|]*[[:space:]])?\.${END}" "never discard work"
check "${GIT}stash[[:space:]]+(drop|clear)" "never discard work"
# Force-deleting, force-renaming or force-moving a branch: -D, -M, -f, combined -df/-fd.
check "${GIT}branch[[:space:]]([^;&|]*[[:space:]])?(-D|-M|-f|--force|-[a-zA-Z]*(df|fd)[a-zA-Z]*)${END}" "never force-delete, force-rename or force-move a branch"
# Skipping hooks. Only inside a git command: a bare match also blocked heredocs and greps
# that merely mention the flag.
check "${GIT}[^;&|]*--no-verify" "never skip hooks"

# --- Checked per command segment, in order, following the directory and branch.
# `&&` and parentheses (subshells, command substitutions) become segments of their own: a pending
# switch needs the &&, a subshell restores the directory on exit. The `&` of a redirection
# (`2>&1`, `&>file`) is dropped first, so it neither splits a command nor breaks an && chain.
SEGMENTS=$(printf '%s' "$CHECKED" | sed -E -e 's/[0-9]*>&[0-9-]*/ /g' -e 's/&>(>?)/>\1/g' -e 's/&&/\
@AND@\
/g' -e 's/([()])/\
\1\
/g' | tr ';&|{}`' '\n\n\n\n\n\n')
DIR=${CWD:-$PWD}
SUBSHELLS=()
PUSHED=()
while IFS= read -r seg; do
  trimmed=${seg#"${seg%%[![:space:]]*}"}
  trimmed=${trimmed%"${trimmed##*[![:space:]]}"}
  case "$trimmed" in
    '') continue ;;
    @AND@)
      if [ "$PEND" = 1 ]; then OV_TOP=$PEND_TOP; OV_BRANCH=$PEND_BRANCH; fi
      PEND=0
      continue
      ;;
  esac
  PEND=0
  case "$trimmed" in
    '(') SUBSHELLS[${#SUBSHELLS[@]}]=$DIR; continue ;;
    ')')
      k=${#SUBSHELLS[@]}
      if [ "$k" -gt 0 ]; then DIR=${SUBSHELLS[$((k - 1))]}; unset "SUBSHELLS[$((k - 1))]"; fi
      continue
      ;;
  esac
  # Cheap pre-filter: only segments that may move the directory or run git/gh are parsed.
  case "$seg" in *git*|*gh*|*cd*|*pushd*|*popd*) ;; *) continue ;; esac
  if [[ "$seg" =~ $POPD_RE ]]; then
    k=${#PUSHED[@]}
    if [ "$k" -gt 0 ]; then DIR=${PUSHED[$((k - 1))]}; unset "PUSHED[$((k - 1))]"; else DIR="?"; fi
    continue
  fi
  if [[ "$seg" =~ $CD_RE ]]; then
    verb=${BASH_REMATCH[3]}
    target=${BASH_REMATCH[7]}
    [ "$verb" = pushd ] && PUSHED[${#PUSHED[@]}]=$DIR
    if [ -n "$target" ]; then DIR=$(resolve_dir "$DIR" "$target"); else DIR=$HOME; fi
    continue
  fi
  if matches "$seg" "${GH}pr[[:space:]]+merge${END}"; then
    check_gh_merge "$seg"
    continue
  fi
  matches "$seg" "$GIT" || continue
  [[ "$seg" =~ $GITCMD_RE ]] || continue
  n=${#BASH_REMATCH[@]}
  opts=${BASH_REMATCH[2]}
  sub=${BASH_REMATCH[$((n - 2))]}
  rest=${BASH_REMATCH[$((n - 1))]}
  eff=$DIR
  while [[ "$opts" =~ $C_RE ]]; do
    eff=$(resolve_dir "$eff" "${BASH_REMATCH[1]}")
    opts=${BASH_REMATCH[$((${#BASH_REMATCH[@]} - 1))]}
  done
  case "$sub" in
    switch|checkout) track_switch "$eff" $rest ;;
    commit|merge|revert|cherry-pick)
      br=$(branch_of "$eff")
      protected "$br" && block "never $sub on $(describe "$br"): work on a branch, main only moves through a merged PR"
      ;;
    reset) check_reset "$eff" $rest ;;
    pull) check_pull "$eff" $rest ;;
    push) check_push "$eff" $rest ;;
  esac
done <<< "$SEGMENTS"
exit 0
