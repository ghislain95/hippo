#!/bin/bash
# Regression tests for block-git-writes.sh. Run: bash .claude/hooks/block-git-writes.test.sh
# Each case: the cwd the agent runs in (a repo on main, or on a feature branch), the command string,
# and the expected hook exit code (2 = blocked, 0 = allowed).
H="${1:-$(dirname "$0")/block-git-writes.sh}"
case "$H" in /*) ;; *) H="$PWD/$H" ;; esac
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
MAIN="$TMP/on-main"
FEAT="$TMP/on-feat"
git init -q -b main "$MAIN"
git -C "$MAIN" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
git -C "$MAIN" branch feat/other
git -C "$MAIN" update-ref refs/remotes/origin/feat/rem HEAD
mkdir -p "$MAIN/sub" "$MAIN/sub dir"
git init -q -b feat/x "$FEAT"
fail=0; n=0
t() {
  local expected="$1" dir="$2" cmd="$3" got
  n=$((n+1))
  printf '%s' "$(jq -n --arg c "$cmd" --arg d "$dir" '{cwd:$d, tool_input:{command:$c}}')" | "$H" 2>/dev/null
  got=$?
  if [ "$got" != "$expected" ]; then echo "FAIL (got $got, want $expected, cwd ${dir##*/}): $cmd"; fail=1; fi
}
NL=$'\n'
BT='`'

# --- must block on any branch (exit 2). Checked from a feature branch, where commits are allowed,
# so each case proves the rule itself.
for c in \
  'git rebase main' 'git rebase main --no-verify' 'git am p.patch' 'cd a && git am --no-verify p' \
  'git commit-tree HEAD^{tree}' 'git filter-branch --all' 'git rebase --abort && git push' \
  'git commit --amend' 'git commit --amend --no-edit' 'git commit --amend -m "two words"' \
  'git reset --soft HEAD~1' 'git reset HEAD~1' 'git reset --mixed HEAD^' 'git reset HEAD@{1}' \
  'git commit -a -m x' 'git commit -am x' 'git commit --all -m x' \
  'git add -A' 'git add .' 'git add --all' 'git add -u' 'git add -- .' 'git add src && git add .' \
  'git add ./' 'git add *' 'git add ..' 'cd sub && git add ../' \
  'git push' 'git push origin' 'git push -u origin' 'git push;' 'git push|tee log' 'git push&& echo ok' \
  '(cd x && git push)' 'echo $(git push)' 'git push; echo "exit=$?"' 'git push origin 2>&1' \
  'git push origin > out.log' 'git push -o ci.skip origin' 'git push --push-option ci.skip origin' \
  'git push origin main' 'git push -u origin main' 'git push origin master' 'git push origin HEAD:main' \
  'git push origin feat/x:main' 'git push origin :main' 'git push origin --delete main' 'git push origin -d main' \
  'git push origin refs/heads/main' 'git push origin "main"' \
  'git push --force origin feat/x' 'git push -f origin feat/x' 'git push -uf origin feat/x' \
  'git push --force-with-lease origin feat/x' 'git push --force-if-includes origin feat/x' \
  'git push origin +feat/x' 'git push --mirror origin' 'git push --all origin' \
  'git pull' 'git pull origin main' 'git pull --rebase' 'git pull --ff-only --rebase' 'git pull -r --ff-only' \
  'git reset --hard HEAD~1' 'git reset --keep HEAD~1' 'git clean -fd' 'git clean -d -f' 'git clean --force' 'git clean -xdf' \
  'git branch -D x' 'git branch -M x' 'git branch -f feat/x HEAD~2' 'git branch --force x' \
  'git branch --delete --force x' 'git branch -d -f x' 'git branch -f -d x' 'git branch -df x' \
  'git checkout .' 'git checkout -- .' 'git checkout .;echo' 'git checkout HEAD -- .' 'git checkout -f main' \
  'git checkout -B main origin/main' 'git restore .' 'git restore --staged .' 'git restore .;' \
  'git switch -C main origin/main' 'git switch --discard-changes main' 'git switch -f main' \
  'git worktree remove --force wt' 'git stash drop' 'git stash clear' \
  'git merge --no-verify feature' 'git commit --no-verify -m x' 'git commit -m "x y" --no-verify' \
  'rtk git push' 'env git push' 'command git push' 'exec git push' 'nohup git push' 'timeout 60 git push' \
  '/usr/bin/git push' 'if git push; then echo ok; fi' '! git push' 'GIT_SSH_COMMAND="ssh -i k" git push origin main' \
  "cd $MAIN && git commit -m x" "cd $MAIN; git add a.ts; git commit -F msg" "cd $MAIN && git push -u origin HEAD" \
  "git -C $MAIN commit -m x" "git -C $MAIN push origin HEAD" "git -C $MAIN merge feat/x" \
  "cd $MAIN/sub && git commit -m x" "cd \"$MAIN/sub dir\" && git commit -m x" \
  'git switch main && git merge --no-ff feat/x' 'git checkout main && git commit -m x' \
  'git switch main; git push origin HEAD' 'cd "$REPO" && git commit -m x' 'git -C "$REPO" commit -m x' \
  'cd - && git commit -m x' 'git switch "$B" && git commit -m x' \
  "cd $TMP/nowhere && git commit -m x" "git -C $TMP/nowhere commit -m x" \
  "if cd $MAIN; then git commit -m x; fi" "cd -P $MAIN && git commit -m x" "cd $MAIN/sub\\ dir && git commit -m x" \
  "pushd $MAIN && git commit -m x" "pushd $MAIN >/dev/null; git push origin HEAD" \
  'git push "--force" origin feat/x' "git push '-f' origin feat/x" 'git reset "--hard" HEAD' \
  'git clean "-fd"' "git branch '-D' x" 'git checkout "-f" main' 'git commit "--amend"' 'git add "-A"' \
  'cat <<EOF'"${NL}"'$(git push origin main)'"${NL}EOF" 'echo "$(git push origin main)"' \
  "echo ${BT}git push origin main${BT}" \
  'git -C "$(git rev-parse --show-toplevel)" push origin main' 'git -C $(pwd) push origin main' \
  'git push origin "$(git branch --show-current):main"' 'X="$(date)" git push origin main' \
  "printf \$'it\\'s ok\\n' && git push origin main && echo 'done'" \
  "git status # check \\${NL}git push origin main" "cat > m <<'EOF'${NL}x \\${NL}EOF${NL}git push origin main" \
  'echo $((x<<y))'"${NL}git push origin main" "(( m = 1 << bit ))${NL}git push origin main" \
  "cat <<E\"OF\"${NL}b${NL}EOF${NL}git push origin main" "echo \"\$(echo '\"')\" && git push origin main && echo 'z'" \
  "git commit -m \"\$(cat <<'EOF'${NL}fix: msg${NL}EOF)\"${NL}git push origin main" \
  "git commit -m \"\$(cat <<'EOF'${NL}fix: msg${NL}EOF)\" && git push origin main" \
  "git status \"\$(cat <<'EOF'${NL}x)\"" '((git push origin main) && echo pushed)' \
  "echo \"\$(case \$b in main) echo '\"';; esac)\" && git push origin main" \
  "(cd $MAIN && case \$x in a) :;; esac && git commit -m x)" \
  'gh pr merge 26' 'gh pr merge 26 --squash' 'gh pr merge 26 --rebase' 'gh pr merge 26 -s' \
  'gh pr merge 26 --merge --admin' 'gh pr merge 26 --auto --merge'; do
  t 2 "$FEAT" "$c"
done

# --- must block on main only: commits and merges land on a branch, never on main.
for c in 'git commit -m x' 'git commit -F msg.txt' 'git commit;' 'x && git commit -m z' 'git merge feature' \
         'git revert HEAD' 'git cherry-pick abc123' 'git -C sub commit -m y' 'git -C "sub dir" commit -m y' \
         'git --no-pager commit -m x' 'git --git-dir=.git commit -m x' "git -c 'user.name=x y' commit" \
         "git \\${NL}commit -m x" "GIT_AUTHOR_NAME='A B' git commit -m x" 'HUSKY=0 git commit -m x' \
         'env git commit -m x' 'for r in a; do git commit -m x; done' '{ git commit -m x; }' \
         'git merge --abort; git commit -m x' 'git switch feat/other && git switch main && git commit -m x' \
         'git push origin HEAD' 'git push -u origin HEAD' 'git push origin @' \
         'git push -u origin "$(git branch --show-current)"' 'git push -u origin $(git branch --show-current)' \
         'git push origin "$BRANCH"' 'git pull --ff-only origin feat/x' 'git pull --ff-only origin feat/x:main' \
         "echo 'it'\\''s' && git commit -m 'x'" \
         "cat > m <<'EOF'${NL}fix: don't drop alerts${NL}EOF${NL}git commit -F m && git push origin main && echo 'ok'" \
         "git status # it's fine${NL}git commit -m 'x'" \
         'git switch feat/other; git switch -; git commit -m x' 'git checkout feat/other a.txt; git commit -m x' \
         "(cd $FEAT && git status); git commit -m x" "pushd $FEAT; popd; git commit -m x" \
         "git push origin ${BT}git branch --show-current${BT}" \
         'git reset feat/other' 'git reset --soft feat/other' \
         'git -C "$(git rev-parse --show-toplevel)" commit -m x' \
         "git switch feat/other${NL}git commit -m x" 'git switch feat/other || true; git push origin HEAD' \
         'git switch feat/other; git commit -m x' 'git checkout -b feat/y; git commit -m x' \
         'case $b in main) git push origin HEAD;; esac'; do
  t 2 "$MAIN" "$c"
done

# --- must pass on a feature branch (exit 0)
for c in 'git commit -m x' 'git commit -F msg.txt' 'git commit;' 'x && git commit -m z' 'git -C . commit -m y' \
         "cat > m <<'EOF'${NL}gh pr merge 1${NL}git push origin main${NL}don't${NL}EOF${NL}git commit -F m" \
         "cat > m <<-EOF${NL}	git push origin main${NL}	EOF${NL}git commit -F m && git push -u origin feat/x" \
         "git commit -m 'x' # then never git push origin main" "echo 'it'\\''s' && git commit -m 'x'" \
         "git push origin ${BT}git branch --show-current${BT}" "pushd $MAIN; popd; git commit -m x" \
         "git commit -m \"\$(cat <<'EOF'${NL}fix: never ${BT}git push origin main${BT}, no (git rebase), no ${BT}git add -A${BT}${NL}EOF${NL})\"" \
         "gh pr create --title t --body \"\$(cat <<'EOF'${NL}Blocks (git rebase) and ${BT}gh pr merge 1 --squash${BT}.${NL}EOF${NL})\"" \
         'git -C "$(git rev-parse --show-toplevel)" status' \
         "git commit -m \"\$(cat <<'EOF'${NL}fix: msg${NL}EOF)\" && git push -u origin feat/x" \
         'echo $((1 + 2)) && git commit -m x' '((n = 1 << 2)) && git commit -m x' \
         'git --no-pager commit -m x' "GIT_AUTHOR_NAME='A B' git commit -m x" "git \\${NL}commit -m x" \
         'git commit -m "docs: never use --no-verify"' 'git commit -m "fix: handle -patch suffix"' \
         'git commit -m "explain --amend and -a; then git push origin main"' \
         "git commit -m \"Never run \\${BT}git pull\\${BT} alone.\"" "git commit -m 'reset --hard is gone'" \
         'gh pr create --title "x" --body "Blocks (git rebase) forms."' 'git -C x rebase --abort' \
         'git add src/a.ts docs/b.md' 'git add ./src/a.ts' 'git add -- src/a.ts' 'git add ./src' \
         'git add src/a.ts && git commit -F msg.txt && git push -u origin feat/x' \
         'git push -u origin feat/x' 'git push origin feat/x' 'git push origin HEAD' 'git push origin @' \
         'git push -u origin "$(git branch --show-current)"' 'git push origin feat/x 2>&1' \
         'git push origin --delete feat/old' 'git push origin feat/main-fix' 'git push --follow-tags origin feat/x' \
         'git push -o ci.skip origin feat/x' 'git push origin feat/x > out.log' \
         'GIT_SSH_COMMAND="ssh -i k" git push origin feat/x' 'timeout 60 git push origin feat/x' \
         'git merge origin/main' 'git merge main' 'git revert HEAD' 'git cherry-pick abc123' \
         'git pull --ff-only' 'git pull --ff-only origin feat/x' 'git branch -m chore/new-name' \
         'git branch -d x; ls -f' 'git reset HEAD file' 'git reset' 'git reset HEAD -- src/a.ts' \
         'gh pr merge 26 --merge' 'gh pr merge 26 --merge --delete-branch' 'gh pr view 26' \
         "cd $FEAT && git commit -m x" 'cd sub && git status' \
         'git status' 'git log --oneline' 'git diff' 'grep "git commit" f' 'git checkout -b feat' \
         'git checkout main' 'git restore src/a.ts' 'git stash list' 'git branch -d merged' \
         'echo committed' 'git commit-graph verify' 'grep -- "--no-verify" f' \
         'echo "--no-verify"' 'git log; echo --no-verify' "$(printf 'cat <<EOF\nbloque --no-verify\nEOF')" \
         'git merge-base main HEAD' 'git status; echo done' 'cd a && git status' 'timeout 5 git status' \
         'HUSKY=0 git status' 'git switch main' 'git switch -c new' 'git checkout ./src/a.ts' 'git clean -n' \
         'git branch --delete merged' 'git rev-parse HEAD' 'git show HEAD --stat' \
         'printf "%s\n" "- run git commit/push yourself"' "echo \"use \\${BT}git commit\\${BT} manually\"" \
         'grep -E "(git commit|git push)" CLAUDE.md' 'rg -n "(git push)" docs' \
         'git fetch origin' 'git worktree list' 'git log --merges' \
         'git config --get user.name' 'git diff --cached' \
         'git rebase --abort' 'git merge --abort' 'git cherry-pick --abort' 'git am --quit' 'git revert --quit' \
         'git worktree remove wt' 'grep -E "git (commit|push)" f' 'xargs git log' 'do_it git status' \
         'GIT_PAGER="less -R" git log'; do
  t 0 "$FEAT" "$c"
done

# --- must pass on main: refresh it after a merged PR, start a branch, clean up merged branches,
# work in another checkout that is on a branch.
for c in 'git pull --ff-only' 'git pull --ff-only origin main' 'git switch -c feat/new' 'git checkout -b fix/y' \
         'git switch -c feat/new && git add a.ts && git commit -m x && git push -u origin HEAD' \
         'git switch feat/other && git commit -m x' "git -C $FEAT commit -m x" "cd $FEAT && git commit -m x" \
         "git -C $FEAT push -u origin HEAD" \
         'git status' 'git fetch origin' 'git branch -d feat/merged' 'git push origin --delete feat/merged' \
         'git merge --abort' 'git merge-base main HEAD' 'git log --merges' 'gh pr merge 26 --merge --delete-branch' \
         'gh pr merge 26 --merge --subject "feat(x): y -s --squash (#26)"' 'git switch feat/rem && git commit -m x' \
         "(cd $FEAT && git commit -m x)" 'git reset' 'git reset HEAD' 'git reset HEAD a.txt' 'git reset feat/other -- a.txt' \
         'git switch -c feat/new 2>&1 && git add a.ts && git commit -m x' \
         'git switch -c feat/new &>/dev/null && git commit -m x' 'git switch -c feat/new >/dev/null 2>&1 && git commit -m x'; do
  t 0 "$MAIN" "$c"
done

# --- without jq: git and gh commands are blocked, anything else passes.
mkdir -p "$TMP/bin"
for b in cat grep; do ln -s "$(command -v "$b")" "$TMP/bin/$b"; done
tj() {
  local expected="$1" cmd="$2" got
  n=$((n+1))
  printf '{"cwd":"%s","tool_input":{"command":"%s"}}' "$FEAT" "$cmd" | PATH="$TMP/bin" /bin/bash "$H" 2>/dev/null
  got=$?
  if [ "$got" != "$expected" ]; then echo "FAIL without jq (got $got, want $expected): $cmd"; fail=1; fi
}
tj 2 'git commit -m x'
tj 2 'gh pr merge 26 --merge'
tj 2 'ls\ngit push origin main'
tj 0 'ls -la'

[ "$fail" = 0 ] && echo "ALL $n CASES OK"
exit "$fail"
