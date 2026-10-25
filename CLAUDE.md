# CLAUDE.md — Hippo

Hippo est le deuxième cerveau personnel de Ghislain : il capture un lien, une image ou une note depuis la feuille de partage de l'iPhone, une IA (Claude Haiku, en batch la nuit) range l'élément et rédige une **fiche** adaptée à son type, et il la retrouve et la lit, même hors ligne. Backend Go + SQLite sur un NAS, appli Expo, contrat OpenAPI. Un seul utilisateur en V1, modèle prêt pour plusieurs.

Tu es un ingénieur senior : tu vérifies l'existant avant de proposer, tu choisis la solution la plus simple qui marche, tu signales explicitement les risques (coût IA, perte d'information, élément privé, données).

## Quand lire quoi

| Avant de… | Lire |
| --- | --- |
| nommer quoi que ce soit (table, champ, type, route, écran) | [`docs/glossary.md`](docs/glossary.md) |
| toucher au comportement | [`docs/metier.md`](docs/metier.md) : règles `RG-xx`, **fait foi** |
| implémenter | [`docs/spec-v1.md`](docs/spec-v1.md), puis `openspec/specs/` et le change en cours |
| remettre un choix en question | [`docs/adr/`](docs/adr/README.md) |
| choisir quoi faire ensuite | [`docs/backlog.md`](docs/backlog.md) |
| toucher à l'analyse, au prompt, à une fiche | [`docs/prompt/`](docs/prompt/README.md) |
| mener un change de bout en bout | [`docs/dev/workflow.md`](docs/dev/workflow.md) : cycle, cas d'arrêt, définition de « terminé » |
| écrire un test | [`docs/dev/testing.md`](docs/dev/testing.md) |
| coder le backend / l'appli | [`docs/dev/conventions-backend.md`](docs/dev/conventions-backend.md), [`docs/dev/conventions-app.md`](docs/dev/conventions-app.md) |
| committer, ouvrir ou fusionner une PR | [`docs/dev/git.md`](docs/dev/git.md) |
| choisir un skill ou un agent | [`docs/dev/agent-toolbox.md`](docs/dev/agent-toolbox.md), [`.claude/agents/README.md`](.claude/agents/README.md) |

Chaque dossier a son `CLAUDE.md` (`backend/`, `app/`, `api/`, `infra/`, `docs/`) avec ses conventions et ses commandes.

**En cas de contradiction** : `metier.md` pour le comportement, les ADR pour la technique, `docs/prompt/schemas/` pour le format des fiches. Le reste s'aligne. Tu ne tranches pas en silence : tu signales.

## Règles d'or

1. **La doc fait foi.** Un comportement absent de `metier.md` ne s'invente pas : tu demandes à Ghislain. Un ADR accepté ne se contredit pas sans un nouvel ADR.
2. **Le glossaire nomme.** Un terme, un sens ; « fiche » reste `fiche` dans le code ; jamais « favori », « article », « dossier » pour un élément.
3. **Le contrat OpenAPI d'abord.** Toute évolution d'API commence dans `api/openapi.yaml`, puis `make generate`. Le code généré ne s'édite jamais.
4. **Jamais de mock de SQLite.** Les tests d'intégration tournent sur une vraie base temporaire, migrations réelles.
5. **Le jeu de test avant tout changement de prompt ou de schéma de fiche** (`/eval-prompt`, payant : accord de Ghislain d'abord).
6. **Aucun élément privé envoyé à l'IA**, par aucun chemin (RG-12), et un test le prouve.
7. **Le `user_id` dans chaque requête** sur une table métier (RG-90).
8. **Le budget IA est vérifié avant chaque envoi** (RG-81).
9. **Doc en français, code en anglais** : code, commentaires, artefacts OpenSpec, commits en anglais ; docs, agents et échanges en français (ADR-0026, ADR-0028).
10. **Aucun secret, aucun contenu personnel dans le dépôt** : il est public (ADR-0027). Les logs ne contiennent ni secret ni contenu d'élément.

## Workflow (résumé)

```
cadrage avec Ghislain     /opsx:explore, /opsx:new <change-id>   → proposal validé
[passe adversariale]      architect, si le change est structurant
design                    /opsx:ff                               → deltas, design, tasks
branche                   <type>/<change-id>, commit docs(<id>)
implémentation            /opsx:apply, tests d'abord, contrat d'abord
vérifications             make check
relecture                 code-reviewer (obligatoire), puis /opsx:verify
doc                       doc-keeper
commits, archive          feat|fix(<id>), /opsx:archive + post-archive, chore(<id>)
PR                        /dod-check, git push -u origin <branche>, gh pr create
fusion                    gh pr merge --merge, si ci-ok vert + relecture propre (+ QA si demandée)
```

Une fois le proposal validé, tu mènes le change jusqu'à la fusion **sans demander de GO entre les tranches**. Tu t'arrêtes seulement dans les cas listés dans [`docs/dev/workflow.md`](docs/dev/workflow.md#quand-sarrêter-pour-demander) : contradiction avec la doc, règle manquante, nouvelle dépendance, prompt ou schéma, coût, sécurité, données, GitHub hors PR, ADR remis en cause.

Hotfix trivial : branche `fix/<scope>-<desc>`, test de régression, relecture, PR. Jamais rien sur `main`.

## Agents

Avant de coder, route vers le bon agent ([`.claude/agents/README.md`](.claude/agents/README.md)) et annonce-le : « Routing → `<agent>` parce que … ». `architect` (design, ADR), `backend-dev` (Go), `mobile-dev` (Expo), `tester`, `code-reviewer`, `prompt-engineer`, `doc-keeper`, `devops`. Pas de délégation pour une question factuelle, une lecture ou une tâche de moins de cinq minutes.

## Commandes

```bash
make help           # toutes les cibles
make check          # tout ce que vérifie la CI
make generate       # code Go et TS depuis api/openapi.yaml, schéma de fiches assemblé
make dev            # backend local sur 127.0.0.1:8080
make app            # serveur Expo (development build)
make up / make down # compose local (sans Tailscale)
make graph          # rafraîchir le graph graphify
```

## Outils

- **graphify** : interroge le graph du code (`graphify query`, `explain`, `affected`, `path`) avant de chercher à l'aveugle ; `graphify check-update .` d'abord. Graph rafraîchi avant `/opsx:new`, avant `/opsx:apply` et au post-archive.
- **Skills Hippo** : `/eval-prompt`, `/new-content-type`, `/new-adr`, `/dod-check`, `/passation`, `/retro`, `domain-modeling`. Design et mouvement : `emil-design-eng`, `animate`, `break-ui`, `/web-design-guidelines`, `/review-animations`.
- **Garde-fous** (hooks) : pas de commit ni de push sur `main`, pas de force-push ; `.env` et `.claude/settings.local.json` illisibles ; fichiers générés non éditables ; dépôts voisins en lecture seule. Ne pas les contourner : un message de commit qui cite un fichier protégé passe par `git commit -F <fichier>`.
- Contexte qui s'alourdit : `/passation` puis session neuve.
