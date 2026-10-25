---
name: dod-check
description: Check the definition of done of the current change or branch before committing the final slice, opening or merging a pull request — local CI green, tasks checked, RG-xx and ADRs cited, contract and generated code up to date, tests for every scenario, docs and backlog updated, no masked quality drop. Use as the last task of every OpenSpec change and before every merge.
argument-hint: "[change id]"
---

# /dod-check — définition de « terminé »

Référence : `docs/dev/workflow.md`, section « Définition de terminé ». Chaque point est **vérifié** (commande lancée, fichier lu), jamais supposé. Rendre une liste ✅ / ❌ avec la preuve ; un seul ❌ = pas terminé.

## Vérifications

1. **CI locale** : `make check` vert (lancé maintenant, sortie lue).
2. **Change** (`openspec/changes/<id>/`, ou `$ARGUMENTS`) : `openspec validate <id>` OK ; `tasks.md` entièrement coché ; `design.md` à jour de ce qui a été fait.
3. **Traçabilité** : le proposal et les deltas citent les `RG-xx` et les ADR concernés ; aucune règle métier ajoutée hors de `docs/metier.md`.
4. **Tests** : chaque scénario des deltas a son test (chercher le nom du scénario dans les tests) ; les tests obligatoires du périmètre existent (privé jamais envoyé à l'IA, isolation des utilisateurs, budget).
5. **Contrats** : `make check-generated` vert ; toute route ou tout schéma touché l'est d'abord dans `api/openapi.yaml` ; un prompt ou un schéma de fiche modifié a sa version incrémentée et un rapport `/eval-prompt` sans échec bloquant.
6. **Baisse de qualité masquée** : `git diff origin/main...HEAD -U0` ne contient aucun `t.Skip`, `.skip(`, `//nolint`, `eslint-disable`, `@ts-ignore`, `@ts-expect-error`, `any` ajouté sans justification écrite.
7. **Doc** : glossaire (termes nouveaux), `docs/spec-v1.md` et `docs/dev/` si un contrat, une convention ou la stack a changé, `CLAUDE.md` du dossier si une commande a changé ; `make check-docs` vert.
8. **Backlog** : la story du change est marquée terminée dans `docs/backlog.md` (au post-archive).
9. **Git** : branche `<type>/<change-id>`, commits en Conventional Commits anglais avec corps, aucune mention de Claude, rien de sensible dans le diff (gitleaks passé par le hook).
10. **Revue** : la dernière passe `code-reviewer` est sans 🛑 ni 🔴 ouvert.
11. **QA** : si le proposal dit « QA utilisateur : oui », la checklist de QA est prête pour Ghislain, et la fusion attend son retour.

## Sortie

```
DoD <change-id> : ❌ 2 points
✅ 1. make check — vert (backend, app, api, schemas, docs, tooling)
❌ 4. scenario "rejects a private item" sans test — backend/internal/analysis/
…
```
