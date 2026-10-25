# Plan du setup (phase 2)

Plan de la phase 3, établi le 25 octobre 2026 à partir de l'[inventaire](setup-inventory.md) et de ses [arbitrages](setup-inventory.md#7-arbitrages-25-octobre-2026). Il liste les fichiers à créer, leur rôle et l'ordre des commits sur la branche `chore/setup`.

Règles du plan :

- Chaque commit laisse le dépôt vert : tout ce qui existe compile et passe `make check`.
- Conventional Commits en anglais, sujet et corps, aucune mention de Claude (règles détaillées dans `docs/dev/git.md`, créé au commit 7). Hors change OpenSpec, le scope est le module : `tooling`, `docs`, `api`, `backend`, `app`, `infra`, `ci`.
- Aucune fonctionnalité métier : pas de route `/api/items`, pas d'extraction, pas d'analyse, pas d'écran autre que l'état du serveur.

## 1. Décisions à valider avant de commencer

Ces points ne sont couverts ni par la doc ni par tes réponses. Pour chacun, ma recommandation ; ils deviendront des ADR.

| # | Décision | Recommandation | Alternative écartée |
| --- | --- | --- | --- |
| P1 | **Outil de migrations SQLite** | `pressly/goose` v3, utilisé comme librairie : fichiers SQL embarqués dans le binaire (`embed`), appliqués au démarrage, compatible `modernc.org/sqlite` | Migrateur maison (~60 lignes, zéro dépendance, mais à maintenir et à tester nous-mêmes) ; `golang-migrate` (plus lourd, pilotes séparés) |
| P2 | **CI : un workflow avec une porte unique** | Un seul `ci.yml` dont les jobs (backend, app, api, schémas, docs, secrets, image…) sont filtrés par chemin, plus un job final `ci-ok` qui agrège les résultats. La protection de `main` n'exige que `ci-ok` | Un workflow par domaine (comme wifisense) : sur un dépôt public, la protection de branche est disponible, mais un check obligatoire qui ne tourne pas (filtré par chemin) bloque la fusion. La porte unique règle ça |
| P3 | **Visibilité de l'image GHCR** | Publique, comme le dépôt : elle ne contient aucun secret, et le NAS la tire sans identifiant | Privée (ADR-0019) : il faudrait un token de lecture sur le NAS |
| P4 | **Langue des fichiers d'agents** | `CLAUDE.md`, `docs/dev/`, définitions d'agents, skills et commandes **en français** (ce sont des docs que tu lis) ; artefacts OpenSpec, code, commentaires, messages de commit et titres de PR **en anglais** ; description de PR en français (comme wifisense) | Agents et skills en anglais |
| P5 | **QA avant fusion** | Le proposal d'un change déclare « QA utilisateur : oui / non » au cadrage. Si oui, l'agent s'arrête avant la fusion et te donne une checklist de QA ; sinon il fusionne seul | QA systématique avant fusion (bloque l'autonomie) ou toujours après (risque d'un écran cassé sur `main`) |
| P6 | **Exemples de fiches** | Créer un exemple par type (8 fichiers) dans `docs/prompt/schemas/examples/` : des données de test fictives, pas des règles. Ils servent à valider les schémas, et plus tard aux tests de rendu des templates et de l'export Markdown | Pas d'exemples : le script de validation n'aurait rien à valider |

## 2. Ce que le plan ajoute ou retire par rapport à ta consigne

**Ajouté**

- `docs/dev/setup-plan.md` (ce fichier) et `docs/dev/agent-toolbox.md` : catalogue des skills et des agents, avec ce qui est différé ou écarté et pourquoi (repris de wifisense).
- Agent `devops`, et la suite de tests du hook git, lancée en CI (wifisense).
- Un sous-commande `healthcheck` dans le binaire : une image distroless n'a ni `curl` ni `wget`, et le healthcheck du compose doit pouvoir appeler `/healthz`.
- La porte `ci-ok` (P2) et les exemples de fiches (P6).
- Variables techniques absentes de la spec mais nécessaires au squelette : `HTTP_ADDR` (`:8080`), `DATA_DIR` (`/data`), `LOG_LEVEL` (`info`). Documentées dans `.env.example`.

**Retiré ou reporté**

- Pas de build iOS en CI (runner macOS lent, Xcode requis) : le development build se compile sur le Mac.
- Pas de types Go et TypeScript générés depuis les schémas de fiches (ADR-0006 les prévoit) : ils arrivent avec le premier change qui écrit ou affiche une fiche (E3-02 ou E5-04). Le setup pose seulement l'assemblage et la validation.
- Pas de seuil de couverture bloquant : la couverture est mesurée et affichée.

## 3. Commits et fichiers

### Commit 1 — `docs(tooling): record the setup inventory and plan`

| Fichier | Rôle |
| --- | --- |
| `docs/dev/setup-inventory.md` | Inventaire de la phase 1 et arbitrages |
| `docs/dev/setup-plan.md` | Ce plan |

### Commit 2 — `chore(tooling): add repository hygiene files`

| Fichier | Rôle |
| --- | --- |
| `.gitignore` | Go (`bin/`, `*.test`, `coverage.*`), Node (`node_modules/`), Expo (`.expo/`, `app/ios/`, `app/android/`, `dist/`), macOS, `.env*` sauf `.env.example`, `data/`, état local de Claude Code (`settings.local.json`, `passation/`, `worktrees/`), `graphify-out/` |
| `.gitattributes` | LF partout ; `linguist-generated` sur le code généré (replié dans les diffs de PR) |
| `.editorconfig` | UTF-8, LF, tabulations pour Go et Makefile, 2 espaces ailleurs |
| `.worktreeinclude` | Copie `settings.local.json` (et donc `GH_TOKEN`) dans les worktrees |
| `Makefile` | Point d'entrée unique. Cibles : `help`, `setup` (hooks lefthook, `npm ci`), `fmt`, `lint`, `test`, `generate`, `check-generated`, `check` (tout ce que la CI vérifie), `dev` (backend local), `app` (Expo), `up` / `down` / `logs` (compose local), `image`, `graph`. Au départ, seules les cibles des dossiers existants font quelque chose |
| `lefthook.yml` | Pré-commit sur les fichiers indexés : gitleaks, gofmt/goimports, Prettier, ESLint rapide ; `--no-verify` reste interdit aux agents par le hook |
| `.gitleaks.toml` | Règles par défaut + exceptions documentées (`.env.example`, exemples de tokens factices dans les tests) |

### Commit 3 — `chore(tooling): configure Claude Code permissions and hooks`

| Fichier | Rôle |
| --- | --- |
| `.claude/settings.json` | `allow` : `make *`, `go *`, `npm`/`npx` dans `app/` et `api/`, `openspec *`, `graphify *`, `docker compose *`, `docker build *`, git (lecture, `add` par chemins, `commit`, `switch`, `push -u origin <branche>`), `gh pr *`, `gh run *`, lecture de tout le dépôt. `deny` : lecture et édition de `.env*` et de `settings.local.json`, `git push --force*`, `rm -rf` hors du dépôt, Edit/Write dans `../geomni` et `../wifisense`. Enregistrement des hooks ci-dessous |
| `.claude/hooks/block-git-writes.sh`, `.pl`, `.test.sh` | Garde-fou git de wifisense (commit et push sur `main`, force-push, réécriture, staging en masse, `--no-verify`, fusion autre qu'en merge commit), messages renvoyant à `docs/dev/git.md` |
| `.claude/hooks/protect-files.sh` | `PreToolUse` Read/Edit/Write/Bash : refuse `.env*` (sauf `.env.example`) et `settings.local.json` ; refuse l'édition du code généré avec « régénère : `make generate` » |
| `.claude/hooks/format-on-edit.sh` | `PostToolUse` Edit/Write : `gofmt` + `goimports` sur un `.go`, Prettier sur un fichier de `app/` ou `api/` ; silencieux si l'outil manque |
| `.claude/hooks/protect-files.test.sh` | Cas bloqués et permis, lancés en CI |
| `.claude/launch.json` | Backend local (`make dev`) et Expo (`make app`) pour le panneau de preview |

### Commit 4 — `chore(tooling): add the agent team`

Tous en `model: opus`, rédigés en français (P4), chacun avec mission, lectures obligatoires, règles dures, livrables, anti-patterns et sa place dans le cycle OpenSpec.

| Fichier | Rôle |
| --- | --- |
| `.claude/agents/README.md` | Tableau des rôles et règles de routage |
| `architect.md` | Design OpenSpec (deltas, `design.md`, `tasks.md`), passe adversariale, ADR au format d'ADR-0001, décisions sourcées à la version de `go.mod`/`package.json` |
| `backend-dev.md` | Go : `net/http`, `slog`, SQLite, migrations, contrat d'abord, `user_id`, interface `Analyzer` |
| `mobile-dev.md` | Expo : expo-router, client généré, TanStack Query, templates de fiches, thèmes |
| `tester.md` | Tests Go sur vraie SQLite, jest-expo, rendu des templates, un scénario = un test, preuve par mutation |
| `code-reviewer.md` | Une ligne par constat, sévérités, baisse de qualité masquée, conformité aux `RG-xx`, aux ADR, au change et à la définition de « terminé » |
| `prompt-engineer.md` | Prompt, schémas, versions, jeu de test ; ne lance `/eval-prompt` qu'après ton accord (coût) |
| `doc-keeper.md` | Glossaire, ADR, backlog, spec, README, liens ; détecte les contradictions entre doc et code |
| `devops.md` | Dockerfile, compose, Tailscale, workflows, Dependabot |

### Commit 5 — `chore(tooling): add skills and commands`

| Fichier | Rôle |
| --- | --- |
| `.claude/skills/eval-prompt/SKILL.md` | Lance le jeu de test (`docs/prompt/eval-set.md`) en appels directs, produit le rapport dans `backend/testdata/eval/reports/`, compare au précédent. Demande ton accord avant tout appel payant. Tant qu'E3-09 n'est pas fait, s'arrête en le disant |
| `.claude/skills/new-content-type/SKILL.md` | Guide l'ajout d'un type (ADR-0011) : schéma, prompt et version, template de l'appli, export Markdown, exemples, tests, glossaire |
| `.claude/skills/new-adr/SKILL.md` | Crée l'ADR suivant au format d'ADR-0001 et met à jour l'index |
| `.claude/skills/dod-check/SKILL.md` | Vérifie la définition de « terminé » avant la PR : `make check` vert, tâches cochées, `RG-xx` et ADR cités, doc et backlog à jour, aucun fichier généré édité à la main, aucune baisse de qualité masquée |
| `.claude/skills/passation/SKILL.md` | Repris de wifisense, chemins et mode de travail de Hippo |
| `.claude/skills/retro/` | Repris de wifisense (avec `LICENSE`, `SOURCE.md`) |
| `.claude/skills/domain-modeling/` | Repris de wifisense, branché sur `docs/glossary.md` |
| `.claude/skills/graphify/` | Repris de wifisense |
| `.claude/skills/{emil-design-eng,animate,review-animations,improve-animations,break-ui,web-design-guidelines,improve-codebase-architecture}/` | Repris de wifisense tels quels (avec `LICENSE`, `SOURCE.md`), relus avant copie |
| `.graphifyignore` | Code seulement (exclut `.claude/`, `**/*.md`, code généré) |

### Commit 6 — `chore(tooling): initialise OpenSpec`

| Fichier | Rôle |
| --- | --- |
| `openspec/config.yaml` | `schema: spec-driven` ; contexte : produit, stack, ADR clés, glossaire, `metier.md` qui fait foi ; règles : proposal (non-objectifs, story, `RG-xx`, ADR, impacts coût IA / privé / données / prompt, « QA utilisateur : oui/non »), specs (anglais, observable, cas d'erreur, `RG-xx` cités), design (packages, migration, contrat OpenAPI, prompt/schéma, sources), tasks (contrat d'abord, tests en premier, doc, revue, `/dod-check` en dernier) |
| `openspec/specs/`, `openspec/changes/` | Vides (aucun change fonctionnel) |
| `.claude/commands/opsx/*`, `.claude/skills/openspec-*` | Générés par `openspec init`, non édités |

### Commit 7 — `docs(tooling): document the development workflow`

| Fichier | Rôle |
| --- | --- |
| `docs/dev/workflow.md` | **Le document central.** Cycle autonome d'un change (cadrage avec toi → proposal, design et tasks validés → branche → tâches en commençant par les tests → vérifications locales → `code-reviewer` → `/opsx:verify` → doc → commits → archive et post-archive → PR → fusion par l'agent si CI verte et revue propre, QA si demandée → nettoyage) ; hotfix ; **quand s'arrêter pour demander** (contradiction avec la doc, règle métier manquante, nouvelle dépendance, changement de prompt ou de schéma, coût, sécurité, données, action GitHub hors PR, ADR remis en cause) ; **définition de « terminé »** ; principes de la boîte à outils |
| `docs/dev/testing.md` | Pyramide ; tests unitaires et d'intégration Go sur vraie SQLite (`t.TempDir()`, migrations réelles, `httptest`) ; aucun réseau en test (pages HTML et réponses de l'IA enregistrées) ; fausse implémentation d'`Analyzer` ; tests front (jest-expo, RNTL, rendu des templates sur les exemples de fiches) ; jeu de test du prompt ; fixtures et `testdata/` ; ce qui bloque en CI ; couverture affichée ; anti-patterns |
| `docs/dev/conventions-backend.md` | Découpage en packages (`cmd/hippo`, `internal/config`, `internal/httpapi`, `internal/store`, puis un package par domaine), erreurs (wrapping, problem+json), logs `slog` (jamais de secret ni de contenu d'élément), nommage d'après le glossaire, `user_id` partout, contexte, migrations, code généré |
| `docs/dev/conventions-app.md` | Arborescence (`app/` routes expo-router, `src/api`, `src/features/<feature>`, `src/templates`), client généré seul point d'accès HTTP, TanStack Query pour l'état serveur, configuration injectée au build, textes de l'UI en français, accessibilité |
| `docs/dev/git.md` | Branches, commits, PR, revue, conditions de fusion, ce que le hook impose (repris de `CONTRIBUTING.md` de wifisense) |
| `docs/dev/agent-toolbox.md` | Skills et agents retenus, différés, écartés ; graphify et ses points de rafraîchissement |
| `docs/adr/0027-depot-public.md` | Dépôt et image GHCR publics ; amende ADR-0002 et ADR-0019 sur la visibilité |
| `docs/adr/0028-langues-du-developpement.md` | Complète ADR-0026 : artefacts OpenSpec, commits et titres de PR en anglais ; docs, agents et échanges en français |
| `docs/adr/0029-workflow-git-autonome.md` | Les agents committent, poussent et fusionnent sous conditions ; hook, protection de `main`, porte `ci-ok` (P2), merge commit |
| `docs/adr/0030-outillage-qualite.md` | Makefile, golangci-lint, lefthook, gitleaks, ESLint et Prettier, lychee, Dependabot |
| `docs/adr/0031-generation-depuis-les-contrats.md` | oapi-codegen (serveur strict, `net/http`), openapi-typescript + openapi-fetch, Redocly pour le lint, assemblage des schémas de fiches en Go ; contrôles « à jour » |
| `docs/adr/0032-migrations-goose.md` | P1 |
| `docs/adr/0033-strategie-de-test.md` | Tests Go standard sur vraie SQLite, jest-expo ; Gherkin écarté |
| `docs/adr/README.md` | Index mis à jour |

### Commit 8 — `docs(tooling): add agent context files`

| Fichier | Rôle |
| --- | --- |
| `CLAUDE.md` | ~100 lignes : ce qu'est Hippo, table de routage (quand lire quoi), règles d'or, cycle OpenSpec résumé, routage des agents, liens vers `docs/dev/` |
| `backend/CLAUDE.md`, `app/CLAUDE.md`, `api/CLAUDE.md`, `infra/CLAUDE.md`, `docs/CLAUDE.md` | Conventions locales et commandes utiles (une vingtaine de lignes chacun) |

### Commit 9 — `build(api): add the OpenAPI contract skeleton`

| Fichier | Rôle |
| --- | --- |
| `api/openapi.yaml` | OpenAPI 3.1 : `info`, serveur, sécurité Bearer globale, schéma `Problem` (RFC 9457), paramètres et enveloppe de pagination par curseur (`cursor`, `limit`, `next_cursor`), route `GET /healthz` (sans sécurité) |
| `api/redocly.yaml` | Règles de lint (recommandées + `snake_case` des propriétés) |
| `api/package.json`, `api/package-lock.json` | `@redocly/cli` épinglé |

### Commit 10 — `build(backend): add the Go walking skeleton`

| Fichier | Rôle |
| --- | --- |
| `backend/go.mod`, `go.sum` | Module `github.com/ghislain95/hippo/backend`, Go 1.27 ; `tool` : oapi-codegen |
| `backend/cmd/hippo/main.go` | Démarrage : config, logger, base, migrations, serveur, arrêt propre ; sous-commande `healthcheck` |
| `backend/internal/config/` | Lecture et validation des variables d'environnement (échec au démarrage si `API_TOKEN` manque ou fait moins de 32 octets) + tests |
| `backend/internal/httpapi/` | Serveur `net/http`, routeur généré, `/healthz`, middleware d'authentification (Bearer, comparaison en temps constant, `user_id` dans le contexte), réponses problem+json, logs de requête sans secret + tests |
| `backend/internal/httpapi/api.gen.go` | Généré par oapi-codegen (`go generate`), jamais édité |
| `backend/internal/store/` | Ouverture SQLite (`modernc.org/sqlite`, WAL, `foreign_keys`, `busy_timeout`), migrations embarquées, création de l'utilisateur unique au premier démarrage (spec : table `users`) + test d'intégration sur vraie base |
| `backend/internal/store/migrations/00001_init.sql` | Table `users` seulement |
| `backend/.golangci.yml` | Linters (dont `depguard` pour les règles d'architecture) et formateurs gofumpt/goimports |
| `backend/Dockerfile` | Multi-étapes : `golang:1.27` → `gcr.io/distroless/static-debian12:nonroot`, binaire statique, `CGO_ENABLED=0`, `linux/amd64` et `arm64` |

Tests : un test unitaire (config), un test d'intégration (serveur + vraie SQLite : `/healthz` 200 sans token, route protégée de test 401 sans token et 200 avec).

### Commit 11 — `build(prompt): assemble and validate the fiche schemas`

| Fichier | Rôle |
| --- | --- |
| `backend/cmd/schemabundle/` | Assemble `classify-item.schema.json` et ses `$ref` en un schéma autonome ; valide les exemples (`santhosh-tekuri/jsonschema`) ; option `-check` qui échoue si le fichier assemblé n'est pas à jour + tests |
| `backend/internal/analysis/schema/classify-item.bundled.json` | Généré, embarqué dans le binaire (il servira d'`input_schema` et de validation, ADR-0006) |
| `docs/prompt/schemas/examples/*.json` | Un exemple de réponse `classify_item` par type de fiche (P6) |
| `docs/prompt/README.md` | Section « Assemblage » complétée (commande, emplacement du fichier assemblé) |

### Commit 12 — `build(app): add the Expo walking skeleton`

| Fichier | Rôle |
| --- | --- |
| `app/package.json`, lockfile, `.nvmrc` | Expo SDK 58, TypeScript strict, `expo-dev-client`, `expo-router`, TanStack Query, openapi-fetch ; scripts `lint`, `typecheck`, `test`, `generate`, `format` |
| `app/app.config.ts` | Nom, bundle id, URL du serveur injectée au build (`HIPPO_API_URL`) |
| `app/app/_layout.tsx`, `app/app/index.tsx` | Un écran : état du serveur (`/healthz`) |
| `app/src/api/schema.gen.ts` | Généré par openapi-typescript, jamais édité |
| `app/src/api/client.ts` | Client openapi-fetch, seul point d'accès HTTP |
| `app/src/features/health/` | Hook et composant de l'écran + test jest-expo / RNTL |
| `app/eslint.config.js`, `.prettierrc`, `tsconfig.json`, `jest.config.js` | Lint, format, types, tests |

Le development build compile avec `npx expo run:ios` dès qu'Xcode est installé ; pas d'extension de partage (E1).

### Commit 13 — `build(infra): add the compose files`

| Fichier | Rôle |
| --- | --- |
| `infra/docker-compose.yml` | Conforme à la spec : sidecar `tailscale` + `hippo` dans son réseau, volume `./data`, `mem_limit: 128m`, healthcheck par `hippo healthcheck` |
| `infra/docker-compose.local.yml` | Variante Mac sans Tailscale : image construite depuis `../backend`, port `127.0.0.1:8080`, même volume |
| `infra/tailscale/config/serve.json` | Configuration de `tailscale serve` (HTTPS → `127.0.0.1:8080`) |
| `infra/.env.example` | Toutes les variables, commentées, avec valeurs factices |

### Commit 14 — `ci: add the GitHub Actions workflows`

| Fichier | Rôle |
| --- | --- |
| `.github/workflows/ci.yml` | Filtres par chemin et porte `ci-ok` (P2). Jobs : **backend** (golangci-lint, `go test -race`, couverture, build) ; **app** (`npm ci`, lint, types, tests, `expo-doctor`) ; **api** (lint Redocly, code Go et TS générés à jour) ; **schemas** (validation, assemblage à jour) ; **openspec** (`validate --all` épinglé, garde « specs modifiées à la main ») ; **migrations** (aucune migration existante modifiée, supprimée ou renommée) ; **docs** (liens internes, lychee hors ligne) ; **tooling** (tests des hooks, actionlint, shellcheck) ; **secrets** (gitleaks) ; **image** (build Docker) |
| `.github/workflows/image.yml` | Sur `main` : build multi-arch et publication sur GHCR (`latest` + `sha-<commit>`) avec `GITHUB_TOKEN` |
| `.github/dependabot.yml` | gomod, npm (`app/`, `api/`), github-actions, docker ; hebdomadaire, groupé |
| `.github/pull_request_template.md` | Contexte, change OpenSpec, `RG-xx` et ADR, tests, checklist de « terminé », QA |

### Commit 15 — `docs: close the E0 setup`

| Fichier | Rôle |
| --- | --- |
| `docs/backlog.md` | E0-05 reformulée (squelette du contrat et génération branchée ; le contrat complet des routes devient le premier change OpenSpec) ; stories E0 terminées marquées |
| `README.md` | Statut, structure du dépôt, section « Démarrage » (prérequis, `make setup`, `.env`, `make up`, `make app`, installation SideStore renvoyée à E1) |
| `docs/dev/setup-report.md` | Rapport final (phase 4) |

## 4. Vérification (3.8)

Sur le Mac, avant de pousser : `make check` (format, lint, tests, génération à jour, assemblage à jour, OpenSpec, liens, hooks), `make image`, `make up` puis `curl -fsS http://127.0.0.1:8080/healthz`, et `npx expo run:ios` si Xcode est prêt. Ensuite : push de `chore/setup`, PR, CI verte, revue `code-reviewer`, fusion (P2, ADR-0029).

## 5. Actions manuelles prévues pour toi

- Poser le token dans `.claude/settings.local.json` (déjà créé, gitignoré) : *fine-grained token* limité à `ghislain95/hippo`, permissions **Contents**, **Pull requests** et **Workflows** en lecture et écriture, **Actions** et **Metadata** en lecture.
- Installer Xcode (App Store, identifiant Apple requis), puis `sudo xcode-select -s /Applications/Xcode.app/Contents/Developer` et `sudo xcodebuild -license accept`.
- Après la première CI : protection de `main` (PR obligatoire, check `ci-ok` obligatoire, pas de force-push, merge commit seulement). Je peux le faire avec `gh api` une fois le token posé, si tu me le confirmes.
