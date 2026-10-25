# ADR-0034 — Fusion différée par l'auto-merge de GitHub

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Amende** : [ADR-0029](0029-workflow-git-autonome.md), sur le moment de la fusion

## Contexte

ADR-0029 laisse l'agent fusionner une PR quand `ci-ok` est vert et que la relecture est propre. En pratique, l'agent n'apprend pas que la CI est passée : l'appli le réveille sur un échec, pas sur un succès, et la surveillance active de la CI lui est interdite. Les PR vertes restaient donc ouvertes jusqu'à une relance de Ghislain.

Le garde-fou `block-git-writes.sh`, repris de wifisense, interdisait `gh pr merge --auto`, parce que sans protection côté serveur GitHub aurait fusionné sans attendre la CI. Depuis le setup, le ruleset de `main` exige le check `ci-ok`, et l'option « Allow auto-merge » du dépôt est active.

## Décision

- L'agent peut demander la fusion différée : `gh pr merge <n> --auto --merge --delete-branch`. GitHub fusionne alors seul, en merge commit, dès que `ci-ok` est vert.
- **Il ne la demande qu'une fois toutes les autres conditions d'ADR-0029 remplies** : dernière relecture `code-reviewer` sans 🛑 ni 🔴, `/opsx:verify` OK et change archivé pour un change OpenSpec, QA de Ghislain validée si le proposal la demande. Seule l'attente de `ci-ok` est déléguée à GitHub.
- **Avant de pousser tout nouveau commit** sur une PR dont l'auto-merge est actif, l'agent le désactive (`gh pr merge <n> --disable-auto`), puis ne le réactive qu'après une relecture propre du nouveau commit. Sans ça, le `ci-ok` du nouveau commit pourrait passer au vert avant la relecture, et GitHub fusionnerait un commit non relu (le ruleset n'exige aucune approbation).
- Le hook autorise `--auto` avec `--merge`, et `--disable-auto` seul ; il bloque toujours `--admin` (qui contournerait le ruleset), squash et rebase, y compris derrière `gh -R/--repo`, et les fusions par `gh api` (route REST `pulls/<n>/merge`, mutations GraphQL de fusion écrites dans la commande).

## Conséquences

- Une PR prête est fusionnée quelques minutes après la fin de sa CI, sans relance.
- La garantie « pas de fusion sans CI verte » repose désormais sur le ruleset de GitHub ; un ruleset désactivé ou un check `ci-ok` retiré la ferait tomber : `doc-keeper` et `devops` le vérifient s'ils touchent à la CI.
- Une CI rouge laisse la PR ouverte ; l'Auto-fix de l'appli réveille l'agent, qui corrige.

## Alternatives écartées

- **Fusion manuelle par l'agent après la CI** (ADR-0029 seul) : demande une relance à chaque PR.
- **Surveillance de la CI par l'agent** : interdite par l'appli, et coûteuse.
- **Bouton d'auto-merge de l'appli** : il passe par le compte `gh` de la machine, qui n'est pas celui du dépôt.
