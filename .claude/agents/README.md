# Agents Claude Code — Hippo

Chaque fichier `*.md` de ce dossier définit un sous-agent spécialisé. La session principale orchestre le cycle d'un change (`docs/dev/workflow.md`) et délègue selon le tableau ci-dessous. Tous les agents tournent en `model: opus` (qualité avant coût, décision reprise de wifisense) ; le modèle est écrit dans chaque fichier pour qu'une session lancée sur un modèle moins cher ne dégrade pas un agent en silence.

| Agent | Rôle | Écrit |
| --- | --- | --- |
| [`architect`](architect.md) | Design d'un change OpenSpec (deltas, `design.md`, `tasks.md`), passe adversariale, ADR | `openspec/changes/<id>/`, `docs/adr/` |
| [`backend-dev`](backend-dev.md) | Implémentation Go : API, stockage SQLite, migrations, extraction, pipeline d'analyse | `backend/`, `api/` |
| [`mobile-dev`](mobile-dev.md) | Implémentation Expo : écrans, templates de fiches, client d'API, hors ligne | `app/` |
| [`tester`](tester.md) | Tests : Go sur vraie SQLite, jest-expo, rendu des templates, test de régression d'un bug | `backend/**/*_test.go`, `backend/testdata/`, `app/**/*.test.tsx` |
| [`code-reviewer`](code-reviewer.md) | Revue sévère d'un diff contre les `RG-xx`, les ADR, le change et la définition de « terminé » | rien (signale) |
| [`prompt-engineer`](prompt-engineer.md) | Prompt, schémas de fiches, jeu de test, `/eval-prompt` | `docs/prompt/`, `backend/testdata/eval/` |
| [`doc-keeper`](doc-keeper.md) | Cohérence de la doc : glossaire, ADR, backlog, spec, README, liens | `docs/`, `README.md`, `CLAUDE.md` |
| [`devops`](devops.md) | Dockerfile, compose, Tailscale, workflows GitHub Actions, Dependabot | `infra/`, `.github/`, `backend/Dockerfile` |

## Routage

| La demande porte sur… | Agent |
| --- | --- |
| comment faire un change, contrat d'API, schéma de base, choix transverse, ADR | `architect` |
| route, handler, stockage, migration, extraction, batch, budget | `backend-dev` |
| écran, composant, template de fiche, navigation, cache hors ligne, extension de partage | `mobile-dev` |
| écrire ou compléter des tests, reproduire un bug par un test | `tester` |
| « relis », « review », avant chaque commit de code | `code-reviewer` |
| prompt, schéma de fiche, jeu de test, nouveau type de contenu | `prompt-engineer` |
| doc à jour, glossaire, backlog, contradiction entre deux docs | `doc-keeper` |
| Docker, compose, CI, dépendances | `devops` |

- Plusieurs agents possibles : le plus spécialisé gagne.
- Un change qui touche back et front : `backend-dev` (contrat et serveur) **puis** `mobile-dev` (client et écran), jamais en parallèle sur le même contrat.
- Ne pas déléguer une question factuelle, une lecture de fichier ou une tâche de moins de cinq minutes.
- Annoncer le routage : « Routing → `<agent>` parce que <raison> ».
- Le prompt de dispatch rappelle : le change en cours (`openspec/changes/<id>/`), les `RG-xx` concernés, et d'interroger graphify avant de chercher à l'aveugle.

## Ajouter un agent

1. Créer `<nom>.md` avec le frontmatter `name`, `description` (« UTILISER QUAND … NE PAS UTILISER … »), `tools`, `model: opus`.
2. Décrire mission, lectures obligatoires, règles dures, méthode, livrable, anti-patterns, place dans le cycle OpenSpec.
3. Mettre à jour les deux tableaux ci-dessus et `docs/dev/agent-toolbox.md`.
