# ADR-0030 — Outillage de qualité

- **Statut** : Accepté
- **Date** : 2026-10-25

## Contexte

Les agents doivent lancer exactement les mêmes vérifications que la CI, avec peu d'outils, bien configurés. Ni geomni ni wifisense n'ont de point d'entrée unique, de hooks pré-commit ni de détection de secrets.

## Décision

| Besoin | Outil |
| --- | --- |
| Point d'entrée unique | **Makefile** racine (GNU Make, déjà présent sur macOS) : `make check` = ce que vérifie la CI |
| Lint et format Go | **golangci-lint** v2, formateurs gofumpt et goimports, règles d'architecture par `depguard` |
| Lint et format de l'appli | **ESLint** (`eslint-config-expo`) et **Prettier** |
| Lint du contrat | **Redocly CLI** |
| Hooks pré-commit | **lefthook** (binaire unique, sans dépendance à Node ni Python) |
| Détection de secrets | **gitleaks**, au pré-commit et en CI |
| Liens de la doc | **lychee** hors ligne |
| Scripts et workflows | **shellcheck**, **actionlint** |
| Dépendances | **Dependabot** (Go, npm, GitHub Actions, Docker), hebdomadaire et groupé |

Les outils de qualité sont installés globalement sur le Mac (Homebrew) et par leurs actions officielles en CI. Les outils de génération sont épinglés dans le dépôt ([ADR-0031](0031-generation-depuis-les-contrats.md)).

## Conséquences

- Un agent n'a qu'une commande à connaître par usage ; la CI appelle les mêmes cibles.
- Les versions de golangci-lint, gitleaks et lychee sont épinglées dans la CI ; le Mac les suit par Homebrew, avec un écart possible de quelques versions.

## Alternatives écartées

- **Task ou just** : une installation de plus pour ce que Make fait déjà.
- **pre-commit (Python) ou husky (Node)** : une dépendance à un écosystème pour un dépôt surtout Go.
- **Renovate** : plus puissant, mais un service à configurer ; Dependabot est natif.
- **Outils de qualité épinglés par `go tool`** : reproductible, mais Ghislain a préféré l'installation globale.
