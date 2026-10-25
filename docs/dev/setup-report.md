# Rapport du setup

Phase 4 du setup de Hippo, 25 octobre 2026, branche `chore/setup`. Il fait suite à l'[inventaire](setup-inventory.md) (phase 1) et au [plan](setup-plan.md) (phase 2, validé avec ses six recommandations). Aucune fonctionnalité métier n'a été développée.

## 1. Ce qui a été mis en place

Dix-huit commits atomiques ; après chacun, tout ce qui existait compilait et passait ses vérifications.

| Groupe | Fichiers | Contenu |
| --- | --- | --- |
| Hygiène du dépôt | `.gitignore`, `.gitattributes`, `.editorconfig`, `.worktreeinclude`, `.gitleaks.toml`, `lefthook.yml`, `Makefile` | Exclusions (secrets, données, natif Expo, état local de Claude Code) ; fichiers générés repliés dans les diffs ; pré-commit (gitleaks, format Go, ESLint, Prettier) ; **`make` comme point d'entrée unique** (`make help`, `make check` = la CI) |
| Claude Code | `.claude/settings.json`, `.claude/hooks/`, `.claude/launch.json` | Permissions partagées (autorisé : make, go, npm, openspec, docker compose, git, gh pr… ; interdit : `.env`, fichier de settings local, dépôts voisins, force-push, push sur `main`, `rm -rf` hors du dépôt, sudo). Hooks : garde-fou git de wifisense (343 cas de test), `protect-files` (secrets, fichiers générés, dépôts voisins, `rm -r` ; 60 cas), formatage après édition |
| Agents | `.claude/agents/` | `architect`, `backend-dev`, `mobile-dev`, `tester`, `code-reviewer`, `prompt-engineer`, `doc-keeper`, `devops`, tous en `opus`, et le tableau de routage |
| Skills | `.claude/skills/` | Hippo : `/eval-prompt`, `/new-content-type`, `/new-adr`, `/dod-check`, `/passation`. Repris de wifisense : `retro`, `domain-modeling`, `improve-codebase-architecture` (adaptés), `graphify`, skills de design et d'animation (inchangés, avec licence et source) |
| OpenSpec | `openspec/`, `.claude/commands/opsx/`, `.claude/skills/openspec-*` | `openspec init` (CLI 1.14.0) ; `config.yaml` : sources de vérité, stack, règles d'or, langue, règles par artefact (story, `RG-xx`, ADR, impacts, « User QA », un test par scénario, contrat d'abord, `/dod-check` en dernière tâche) |
| Documentation de dev | `docs/dev/workflow.md`, `testing.md`, `conventions-backend.md`, `conventions-app.md`, `git.md`, `agent-toolbox.md` | Cycle autonome en dix étapes, cas d'arrêt, définition de « terminé », garde-fous ; stratégie de test ; conventions ; git |
| ADR | `docs/adr/0027` à `0033` | Voir §2 |
| Contexte des agents | `CLAUDE.md` et un `CLAUDE.md` par dossier | Racine : identité, « quand lire quoi », dix règles d'or, cycle, routage, commandes, garde-fous |
| Contrat | `api/openapi.yaml`, `api/redocly.yaml`, `api/package.json` | OpenAPI 3.1 : sécurité Bearer, problem+json, pagination par curseur, `GET /healthz` ; lint Redocly (+ `snake_case`) ; openapi-typescript |
| Backend | `backend/` | Module Go 1.27 : config validée, `slog` JSON, SQLite (WAL, `STRICT`, goose embarqué, table `users`), utilisateur unique créé au démarrage, serveur strict oapi-codegen, middleware Bearer, problem+json, sous-commande `healthcheck` ; 6 packages testés (dont intégration sur vraie SQLite) ; golangci-lint avec règles `depguard` ; Dockerfile distroless (**23,7 Mo**) |
| Schémas de fiches | `backend/cmd/schemabundle`, `backend/internal/analysis/schema/`, `docs/prompt/schemas/examples/` | Assemblage en un schéma autonome embarqué, 8 exemples fictifs validés, contrôle « à jour » |
| Appli | `app/` | Expo SDK 58, TypeScript strict, development build, expo-router (`src/app/`), client openapi-fetch typé, TanStack Query, écran d'état du serveur, 3 tests jest-expo, ESLint, Prettier, expo-doctor (20/20) |
| Infra | `infra/` | Compose de production (sidecar Tailscale, `serve.json`, image GHCR), variante locale sans Tailscale sur `127.0.0.1:8080`, `.env.example` complet |
| CI | `.github/workflows/ci.yml`, `image.yml`, `dependabot.yml`, `pull_request_template.md` | Un workflow, filtres par chemin, porte `ci-ok` ; publication multi-arch sur GHCR depuis `main` ; Dependabot hebdomadaire groupé ; template de PR avec la checklist de « terminé » |
| Clôture | `docs/backlog.md`, `README.md`, ce rapport | Stories E0, section « Démarrage » |

### Vérification locale (3.8)

| Vérification | Résultat |
| --- | --- |
| `make check` (secrets, liens, hooks, OpenSpec, contrat, schémas, backend, appli, compose, code généré) | ✅ vert, arbre propre après génération |
| Contrôle « code généré à jour » | ✅ échoue bien quand le contrat change sans régénération |
| Tests Go sans cache, avec `-race` | ✅ 6 packages |
| Tests de l'appli | ✅ 3 tests, couverture 100 % des lignes hors routes |
| `make image` | ✅ 23,7 Mo |
| `make up`, `/healthz`, `/api/items` | ✅ `healthy`, 200 `{"status":"ok"}`, 401 sans token, 404 avec token, **23 Mio** de RAM pour une limite de 128 Mo, logs sans token |
| `make graph` | ✅ graph du code construit |
| Development build iOS | ⏸ non compilé : Xcode n'est pas installé (§4) |
| CI GitHub | ⏸ tournera sur la PR |

## 2. Décisions prises

### Devenues des ADR

| ADR | Décision |
| --- | --- |
| [0027](../adr/0027-depot-public.md) | Dépôt et image publics ; amende 0002 et 0019 sur la visibilité |
| [0028](../adr/0028-langues-du-developpement.md) | OpenSpec, commits, titres de PR en anglais ; docs, agents, descriptions de PR en français |
| [0029](../adr/0029-workflow-git-autonome.md) | Les agents committent, poussent et fusionnent sous conditions ; porte unique `ci-ok` |
| [0030](../adr/0030-outillage-qualite.md) | Makefile, golangci-lint, ESLint/Prettier, Redocly, lefthook, gitleaks, lychee, shellcheck, actionlint, Dependabot |
| [0031](../adr/0031-generation-depuis-les-contrats.md) | oapi-codegen (serveur strict `net/http`), openapi-typescript + openapi-fetch, `schemabundle` en Go |
| [0032](../adr/0032-migrations-goose.md) | goose embarqué, migrations SQL ascendantes seulement |
| [0033](../adr/0033-strategie-de-test.md) | Tests Go standard sur vraie SQLite, jest-expo ; Gherkin écarté |

### Décisions de moindre portée (notées ici et dans la doc concernée)

| Décision | Où c'est écrit | Pourquoi |
| --- | --- | --- |
| Types de problème métier `urn:hippo:problem:<slug>`, `about:blank` sinon | `api/openapi.yaml`, conventions backend | RFC 9457 ; un identifiant stable que l'appli peut reconnaître sans URL hébergée |
| Tables SQLite `STRICT` | conventions backend | Refuser une valeur du mauvais type plutôt que la convertir |
| Utilisateur unique nommé `owner`, hash du token remplacé si `API_TOKEN` change | `store/users.go` | La rotation du token ne demande qu'un redémarrage |
| `ANTHROPIC_API_KEY` facultative tant que l'analyse n'existe pas | `config.go`, `.env.example` | Le squelette démarre sans clé ; elle deviendra obligatoire avec E3 |
| Variables techniques `HTTP_ADDR`, `DATA_DIR`, `LOG_LEVEL`, `HIPPO_IMAGE_TAG` ; `HIPPO_API_URL`, `HIPPO_API_TOKEN` pour le build de l'appli | `.env.example` | Absentes de la spec, nécessaires au squelette ; aucune règle métier |
| openapi-typescript dans `api/` et non dans `app/` | ADR-0031, conventions de l'appli | Il exige TypeScript 5, Expo 58 est en TypeScript 6 |
| Routes de l'appli dans `app/src/app/` | conventions de l'appli | Convention d'Expo SDK 58 |
| `protect-files` lit les commandes comme le shell (lecteur de wifisense) | le hook et ses tests | Un heredoc ou un message de commit qui cite `.env` n'est pas une lecture |
| gitleaks autorise les séquences `0123456789abcdef` et `fedcba9876543210` | `.gitleaks.toml` | Tokens de test manifestement factices ; un vrai secret ne les contient pas |
| Image multi-arch (amd64 pour le NAS, arm64 pour le Mac) | `image.yml` | La compilation croisée Go est gratuite |
| Dependabot n'ouvre pas de PR pour les paquets du SDK Expo | `dependabot.yml` | Ils se mettent à jour ensemble, par `npx expo install --fix`, lors d'un change de montée de SDK |
| Plugin Claude officiel d'Expo (`expo@claude-plugins-official`) non activé | — | `create-expo-app` le propose ; il ajoute des skills. À évaluer au début d'E1 ou d'E5, selon le principe « peu de skills » |

### Points à surveiller, signalés sans être tranchés

- **Nullabilité OpenAPI 3.1** : oapi-codegen 2.8.1 traduit `type: [string, "null"]` (`NextCursor`) en `string` non nullable. Le premier change du contrat devra vérifier ce rendu sur ses schémas nullables (le contrat de la spec en a beaucoup) et choisir la parade (`x-go-type-skip-optional-pointer`, pointeurs, ou rendu accepté).
- **Mots-clés non standard dans l'`input_schema` de l'outil** : le schéma assemblé garde `x-schema-version` et les `$defs`. Leur acceptation par l'API Anthropic est à vérifier au premier appel réel (E3-01 ou E3-09), avant de compter dessus.
- **Volume `./data` sur le NAS** : l'image tourne en utilisateur non root (uid 65532). Sur le Mac, Docker Desktop gère les droits ; sur le NAS, le dossier devra appartenir à cet utilisateur (E6-01, `docs/runbook-nas.md`).
- **Compte `gh`** : `gh` est connecté en `ghislain-sartorius-adeo` ; les PR doivent partir de `ghislain95` (token, §4).

## 3. Mise à jour de la backlog et du README

- [`backlog.md`](../backlog.md) : colonne « Statut » pour E0. E0-01 à E0-04, E0-06 et E0-07 sont terminées par le setup (`/eval-prompt` attend le harnais d'E3-09). **E0-05 est reformulée** : le squelette du contrat et la génération sont en place ; le contrat complet des routes de la spec est le premier change OpenSpec.
- [`README.md`](../../README.md) : statut, structure du dépôt, ligne `docs/dev/` dans la table de la doc, section « Démarrage » (prérequis, premier lancement, backend, appli, commandes).

## 4. Actions manuelles qui te restent

Par ordre d'urgence.

1. **Token GitHub pour les agents** : créer un *fine-grained token* limité à `ghislain95/hippo` (Contents, Pull requests et Workflows en lecture et écriture ; Actions et Metadata en lecture) et remplacer la valeur de `GH_TOKEN` dans `.claude/settings.local.json` (fichier déjà créé, gitignoré, illisible par les agents). Sans lui, `gh` agit au nom de `ghislain-sartorius-adeo`.
2. **Xcode** : `sudo mas install 497799835` (ou l'App Store), puis `sudo xcode-select -s /Applications/Xcode.app/Contents/Developer` et `sudo xcodebuild -license accept`. Ensuite `cd app && npx expo run:ios` compile le development build sur le simulateur. CocoaPods et watchman sont déjà installés.
3. **`infra/.env`** : `cp infra/.env.example infra/.env`, puis `API_TOKEN` (`openssl rand -hex 32`). Les agents ne peuvent ni le créer ni le lire.
4. **Après la fusion de la PR du setup, réglages du dépôt** (je peux les faire avec `gh api` une fois le token posé, sur ton accord) :
   - *Rulesets* sur `main` : PR obligatoire, check `ci-ok` obligatoire, pas de force-push, pas de suppression, aucune approbation exigée ;
   - *General* : merge commit seulement (désactiver squash et rebase), suppression automatique des branches fusionnées ;
   - *Actions* : `GITHUB_TOKEN` en lecture par défaut (les workflows demandent eux-mêmes `packages: write`).
5. **GHCR** : après la première publication par `image.yml`, passer le paquet `hippo` en **public** et le relier au dépôt (*Package settings*), conformément à ADR-0027.
6. **Anthropic** (avant E3) : compte API, crédits prépayés **sans recharge automatique**, clé dédiée à Hippo, à poser dans `infra/.env` (ADR-0021).
7. **Tailscale** (E1 et E6) : compte, Tailscale sur l'iPhone, clé d'authentification du nœud `hippo` pour `TS_AUTHKEY`, HTTPS activé sur le tailnet.
8. **Apple et SideStore** (E1) : identifiant Apple gratuit, premier appairage de SideStore depuis le Mac.
9. **NAS** (E6) : dossier de données appartenant à l'uid 65532, service de sauvegarde externe à choisir (ADR-0020).

Aucune action ne coûte d'argent à ce stade.

## 5. Premier change OpenSpec proposé

**`api-contract-v1`** (story E0-05) : écrire dans `api/openapi.yaml` le contrat complet des routes de la [spec](../spec-v1.md#api-rest), sans les implémenter, pour que E2 à E5 partent d'un contrat stable. Il donnera aussi la première spec OpenSpec (`openspec/specs/api-conventions/`).

Questions à cadrer ensemble :

1. **Périmètre** : toutes les routes de la spec en une fois, ou seulement celles d'E2 (capture, détail, images) pour commencer ?
2. **Typage de la fiche** : ADR-0017 dit que le contrat référence les schémas de fiches. Référencer le schéma assemblé, copier les schémas dans `components`, ou exposer `fiche` comme objet libre typé seulement côté appli ?
3. **Capture** : `POST /api/items` en JSON et en multipart sur la même opération, ou deux opérations distinctes ?
4. **Catalogue des types de problème** : les `urn:hippo:problem:<slug>` connus dès maintenant (`budget-exceeded`, `private-item`, `invalid-url`…) ?
5. **Synchronisation** (`GET /api/sync`) : forme des suppressions (identifiants seuls ou objets avec `deleted_at`) et format de `since`.
6. **Nullabilité** : la parade à la limite d'oapi-codegen sur les types nullables d'OpenAPI 3.1 (§2).
