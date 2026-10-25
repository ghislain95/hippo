# Conventions git

Règles communes à Ghislain et aux agents, reprises de wifisense ([ADR-0029](../adr/0029-workflow-git-autonome.md)). Les agents (sous-agents compris) committent, poussent, ouvrent et fusionnent les PR eux-mêmes ; le hook `block-git-writes.sh` en impose la partie mécanique. Le cycle complet est dans [workflow.md](workflow.md).

## Branches

- `main` : stable. Elle ne bouge **que par une PR fusionnée** : jamais de commit ni de push direct, hotfix compris. La protection de branche de GitHub l'impose aussi (dépôt public).
- Change OpenSpec : `<type>/<change-id>`, par exemple `feat/capture-link`, `fix/url-normalization`.
- Hors change : `<type>/<scope>-<desc>`, par exemple `fix/backend-healthz-timeout`, `chore/tooling-dependabot`, `docs/backlog-resync`.
- `<type>` = le type Conventional Commits principal ; `<scope>` ∈ { `backend`, `app`, `api`, `infra`, `docs`, `prompt`, `tooling`, `ci` } ; `<desc>` en kebab-case anglais.
- Session dans un worktree créé par l'appli Claude (nom de branche imposé) : renommer la branche avant le premier push (`git branch -m <nom>`).

## Commits

[Conventional Commits](https://www.conventionalcommits.org/), **en anglais**, **toujours un sujet et un corps** :

```
<type>(<scope>): <imperative, short subject>

<what changes, and the key decisions: why this way>
```

- Types : `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `build`, `ci`, `chore`.
- Scope : **l'id du change** pour tout commit d'un change OpenSpec (`feat(capture-link): …`), sinon le module (`backend`, `app`, `api`, `infra`, `docs`, `prompt`, `tooling`, `ci`).
- **Aucune mention de Claude** : ni trailer `Co-Authored-By`, ni « Generated with Claude Code », dans les commits comme dans les PR.
- Un commit = une tranche cohérente qui laisse `make check` vert ; pas de commit « wip ».
- Staging par **chemins explicites** (`git add <chemins>`) : jamais `git add -A`, `.`, `-u`, ni `git commit -a` (plusieurs sessions peuvent partager un checkout).
- Avant un commit de code : tests du module au vert et relecture `code-reviewer` faite. Avant un commit qui touche `openspec/` : `make check-openspec`.
- Jamais de réécriture : pas d'`--amend`, de `rebase`, de `reset` vers une révision, de force-push. Une erreur se corrige par un nouveau commit.
- Message long, ou qui cite une commande ou un fichier protégé : l'écrire dans un fichier et `git commit -F <fichier>` (`gh pr create --body-file <fichier>`).

### Séquence d'un change

Sur la branche `<type>/<change-id>` :

1. `docs(<id>): frame <subject>` — proposal, puis deltas, design et tasks (un ou deux commits).
2. `feat|fix|refactor(<id>): …` — un commit par tranche, après relecture.
3. `chore(<id>): archive change, sync specs, post-archive`.
4. Push, PR, fusion.

## Pull requests

- Titre = sujet du commit principal (anglais). Description **en français**, selon [le template](../../.github/pull_request_template.md) : contexte, change et story, `RG-xx` et ADR, choix techniques, tests lancés, checklist de « terminé », QA.
- 1 PR = 1 change OpenSpec, ou 1 correctif.
- La relecture `code-reviewer` tient lieu d'approbation (développement solo) : la protection de `main` n'exige pas d'approbation, seulement le check `ci-ok`.
- **Conditions de fusion** (l'agent fusionne seul si toutes tiennent, sinon il demande) : `ci-ok` vert ; dernière relecture sans 🛑 ni 🔴 ; pour un change : `/opsx:verify` OK et change archivé ; si « User QA: yes » : QA validée par Ghislain.
- Fusion en **merge commit** uniquement (ni squash ni rebase), qui garde les commits atomiques du change :
  `gh pr merge <n> --merge --delete-branch --subject "<sujet> (#<n>)"`.
- Après fusion : `git switch main && git pull --ff-only`, puis `git branch -d <branche>`.

## Accès GitHub

`gh` doit agir au nom de `ghislain95`. Côté agents, un token *fine-grained* limité au dépôt est posé en `GH_TOKEN` dans `.claude/settings.local.json` (gitignoré, illisible par les agents, copié dans les worktrees par `.worktreeinclude`). Permissions : Contents, Pull requests et Workflows en lecture et écriture ; Actions et Metadata en lecture. Le push passe par le remote SSH.

## Ce que le hook impose

[`.claude/hooks/block-git-writes.sh`](../../.claude/hooks/block-git-writes.sh), repris de wifisense, bloque pour un agent :

- `commit`, `merge`, `revert`, `cherry-pick` sur `main` (la branche est celle du dossier où la commande s'exécute vraiment, `cd` et `git -C` compris ; un dossier ou une branche impossible à résoudre compte comme `main`) ;
- tout push de `main`, un push sans remote **et** branche explicites (`git push -u origin <branche>`), le force-push ;
- `pull` sans `--ff-only`, avec rebase, ou d'une autre branche sur `main` ;
- la réécriture d'historique (`rebase`, `commit --amend`, `reset` vers une révision, `branch -f`…) ;
- le staging en masse ; ce qui jette du travail (`reset --hard`, `clean -f`, `checkout .`, `restore .`, `stash drop`, `branch -D`) ;
- `--no-verify` ;
- `gh pr merge` autrement qu'en `--merge`.

Ses 343 cas de test tournent dans `make check-tooling` et en CI. Ne pas le contourner : un faux positif se règle par la bonne pratique (`git commit -F`, deux appels au lieu d'un).
