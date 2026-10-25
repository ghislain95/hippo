# Boîte à outils des agents

Ce dont disposent les agents de Hippo, et pourquoi. Repris du catalogue de wifisense, adapté le 25 octobre 2026. L'équipe d'agents et le routage sont dans [`.claude/agents/README.md`](../../.claude/agents/README.md).

## Principes

- **OpenSpec est le process.** Aucun pack de skills « cycle complet » ne le double.
- **Peu de skills.** Chaque description de skill entre dans le contexte de chaque session et peut se déclencher hors de propos ; un skill qui ne sert pas est retiré.
- **Lire avant d'installer.** Un `SKILL.md` devient des instructions : on le lit en entier, on vérifie l'absence de scripts ou d'effets de bord, puis on le copie dans `.claude/skills/<nom>/` avec `LICENSE` et `SOURCE.md` (origine, date, modifié ou non).
- **Une règle mécanique devient un contrôle** (hook, règle de lint, job de CI), pas une consigne. Les consignes écrites sont réservées aux jugements.
- **Peu d'outils bien configurés** plutôt que beaucoup d'outils empilés ; le `Makefile` est le seul point d'entrée.

## Skills et commandes

| Outil | Origine | Quand |
| --- | --- | --- |
| `/opsx:*` et skills `openspec-*` | OpenSpec 1.14.0 (`openspec init`) | Le cycle d'un change ([workflow.md](workflow.md)) |
| `/eval-prompt` | Hippo | Avant de livrer un changement de prompt ou de schéma ; **payant, sur accord** |
| `/new-content-type` | Hippo | Ajouter un type de fiche, ou donner un template à un type proposé |
| `/new-adr` | Hippo | Décision structurante, ou ADR remis en cause |
| `/dod-check` | Hippo | Dernière tâche de chaque change, avant chaque fusion |
| `/passation` | wifisense (adapté) | Avant `/clear` ou une session neuve |
| `/retro` | wifisense, d'après mattpocock (adapté) | Après un change difficile : améliorer l'environnement |
| `domain-modeling` | wifisense, d'après mattpocock (adapté) | Terme flou ou en conflit pendant un cadrage ; mise à jour du glossaire |
| `/improve-codebase-architecture` | wifisense, d'après mattpocock (adapté) | Tous les cinq changes environ, jamais en cours de change |
| `graphify` | graphify 0.9.62 | Carte du code : interroger avant de chercher à l'aveugle (ci-dessous) |
| `emil-design-eng`, `animate`, `/review-animations`, `improve-animations` | emilkowalski/skills, via wifisense | Finition d'un écran, construction ou revue d'une animation (E5) |
| `break-ui` | emilkowalski/skills, via wifisense | Écran de données face aux cas extrêmes (titres longs, zéro élément, beaucoup de tags) |
| `/web-design-guidelines` | vercel-labs/agent-skills, via wifisense | Revue d'accessibilité et d'UX d'un diff d'interface |

Les skills de design visent d'abord le web ; on en applique les principes (hiérarchie, mouvement, cas extrêmes, accessibilité) aux composants React Native. Disponibles aussi sans être vendorisés : `superpowers` (debug systématique, TDD, vérification avant de conclure), `frontend-design`, `ui-ux-pro-max`.

## Garde-fous

| Outil | Rôle |
| --- | --- |
| `block-git-writes.sh` | Règles git ([git.md](git.md)) ; 343 cas de test |
| `protect-files.sh` | Secrets, fichiers générés, dépôts voisins, `rm -r` hors du dépôt ; 53 cas de test |
| `format-on-edit.sh` | Formatage Go et TypeScript après chaque édition |
| `lefthook.yml` | Pré-commit : gitleaks, format, lint rapide |
| `.claude/settings.json` | Permissions autorisées et interdites |

## graphify

Graph du **code** (le Markdown et `.claude/` en sont exclus par `.graphifyignore`) dans `graphify-out/`, local et non versionné. Outil de lecture, jamais une source de vérité : la doc et `openspec/specs/` font foi.

- Installation, une fois par machine : `uv tool install "graphifyy[sql]"`.
- Construire ou rafraîchir : `make graph` (`graphify update .`, analyse syntaxique locale, sans coût).
- Interroger : `graphify query "<question>"`, `graphify explain "<symbole>"`, `graphify affected "<symbole>"`, `graphify path "<A>" "<B>"`.
- **Fraîcheur, portée par la session principale** : avant le recon de `/opsx:new` (`graphify update .`), avant `/opsx:apply` (`graphify check-update .`), au post-archive (`graphify update . --force` si des fichiers ont été supprimés ou renommés). Un graph périmé induit en erreur.
- Quand la session dispatche un agent sur du code, elle lui rappelle d'interroger le graph.
- Le graph ne traverse pas la frontière HTTP entre `app/` et `backend/` : pour ça, le contrat OpenAPI.

## Différés et écartés

| Outil | Statut | Raison |
| --- | --- | --- |
| Agents `product-owner`, `full-stack-dev` | Écartés | Le cadrage se fait avec Ghislain ; la session principale enchaîne `backend-dev` et `mobile-dev` |
| Agent `rendering-dev` (geomni) | Écarté | Pas de 3D ni de carto |
| Commandes `/ticket`, `/review`, `/migrate` (geomni) | Écartées | Couvertes par le workflow, `code-reviewer` et la CI des migrations |
| Gherkin (Cucumber, godog) | Écarté | [ADR-0033](../adr/0033-strategie-de-test.md) |
| Build iOS en CI | Différé | Runner macOS lent et Xcode requis ; à revoir si l'appli casse souvent à la compilation |
| Types Go et TypeScript générés depuis les schémas de fiches | Différé | Avec le premier change qui écrit ou affiche une fiche (E3-02 ou E5-04) |
| Tests de bout en bout de l'appli (Maestro) | Différé | À évaluer avec E5 |
| rtk | Écarté | Retiré de geomni et wifisense : coût supérieur, sorties tronquées |

## Modèles

Tous les agents tournent en `model: opus`, écrit dans chaque fichier : une session lancée sur un modèle moins cher ne dégrade pas un agent en silence.
