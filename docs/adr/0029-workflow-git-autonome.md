# ADR-0029 — Workflow git autonome des agents

- **Statut** : Accepté — moment de la fusion amendé par [ADR-0034](0034-auto-merge-github.md)
- **Date** : 2026-10-25

## Contexte

Hippo est développé par des agents. Ghislain participe au cadrage et à la QA ; le reste (code, tests, doc, PR, fusion) doit avancer sans lui. Sur geomni, les commits restent manuels ; sur wifisense, les agents committent, poussent et fusionnent sous des règles explicites, imposées par un hook. Avec des workflows filtrés par chemin, un check obligatoire qui ne tourne pas bloque la fusion.

## Décision

- Les agents **committent, poussent, ouvrent et fusionnent** les PR, selon [`docs/dev/git.md`](../dev/git.md) (modèle de wifisense).
- `main` ne bouge que par une PR fusionnée, en **merge commit**. Une branche par change : `<type>/<change-id>`.
- Fusion par l'agent seulement si : check `ci-ok` vert, relecture `code-reviewer` sans constat bloquant ou majeur, change vérifié et archivé, QA de Ghislain faite quand le proposal la demande.
- Le hook `block-git-writes.sh` (repris de wifisense, testé en CI) impose la partie mécanique ; la protection de `main` sur GitHub la double (dépôt public, [ADR-0027](0027-depot-public.md)).
- **Une seule porte de CI** : un workflow `ci.yml` dont les jobs sont filtrés par chemin et un job final `ci-ok` qui échoue si un job lancé a échoué. La protection de `main` n'exige que `ci-ok`.
- Aucune mention de Claude dans les commits ni les PR.

## Conséquences

- Un change va du proposal validé à `main` sans intervention, sauf cas d'arrêt listés dans [`docs/dev/workflow.md`](../dev/workflow.md).
- La qualité repose sur la CI, la relecture et `/dod-check` : leurs règles doivent rester strictes.
- Ajouter un job de CI exige de l'ajouter aux dépendances de `ci-ok`, sinon il ne protège rien.

## Alternatives écartées

- **Commits manuels** (geomni) : Ghislain deviendrait le goulot de chaque change.
- **Un workflow par domaine** (wifisense) : chaque check devrait être obligatoire, et un check filtré par chemin qui ne tourne pas bloquerait la fusion.
- **Squash merge** : perd les commits atomiques d'un change (cadrage, tranches, archive).
