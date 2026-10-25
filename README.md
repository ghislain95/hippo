# Hippo

> Un deuxième cerveau personnel : on capture dans la journée, Hippo range pendant la nuit.

*Hippo est un nom de code provisoire, en référence à l'hippocampe, la zone du cerveau qui consolide les souvenirs pendant le sommeil.*

## Statut

**Setup terminé, développement des fonctionnalités pas encore commencé** (25 octobre 2026). La documentation décrit la V1 à construire ; le squelette du code (backend Go, appli Expo, contrat OpenAPI, CI) compile et passe ses vérifications. Prochaine étape : le contrat complet de l'API (story E0-05), premier change OpenSpec.

## Ce que fait Hippo

1. **Capturer en un geste** : un lien, une capture d'écran ou une note, depuis la feuille de partage de l'iPhone, en moins de 5 secondes.
2. **Ranger pendant la nuit** : une IA (Claude Haiku) choisit le type de contenu et la catégorie, ajoute des tags et rédige une **fiche** adaptée (article technique, tuto, astuce, recette, lieu, ressource, note), sans perdre d'information technique.
3. **Retrouver et lire** : recherche plein texte, filtres, lecture confortable avec reprise de la position, même hors ligne.
4. *(V2)* **Se tenir au courant** : un digest du matin construit à partir des sujets suivis.

Le tout tourne sur un NAS domestique, pour quelques dollars d'IA par mois.

## Architecture

```
iPhone (appli Expo + extension de partage)
        │  HTTPS, réseau privé Tailscale
        ▼
NAS · Docker
  └── service Go ── SQLite (base + recherche FTS5, images sur disque)
        │
        ├── sites web        (extraction du contenu, sans IA)
        └── API Anthropic    (Claude Haiku 4.5, en batch la nuit)
```

| Couche | Choix |
| --- | --- |
| Backend | Go, librairie standard, binaire unique |
| Données | SQLite (WAL, FTS5), driver Go pur |
| IA | Claude Haiku 4.5 via l'API Message Batches, sortie structurée |
| Mobile | Expo (React Native), extension de partage iOS |
| Contrat d'API | OpenAPI 3.1, serveur et client générés |
| Réseau | Tailscale, HTTPS via `tailscale serve`, aucun port ouvert |
| Déploiement | Docker Compose sur le NAS, image distroless publiée sur GHCR |
| Distribution iOS | Compte Apple gratuit + SideStore |

Chaque choix est justifié dans un [ADR](docs/adr/README.md).

## Structure du dépôt

```
hippo/
├── backend/   service Go (API, SQLite, migrations, schéma de fiches assemblé)
├── app/       appli Expo (l'extension de partage arrive avec E1)
├── api/       contrat OpenAPI et son outillage (lint, types TypeScript)
├── infra/     docker compose (NAS avec Tailscale, Mac sans), .env.example
├── openspec/  specs et changes OpenSpec
├── docs/      documentation (dont docs/dev/ : workflow, tests, conventions)
└── .claude/   agents, skills, hooks et permissions de Claude Code
```

## Documentation

| Document | Contenu | Quand le lire |
| --- | --- | --- |
| [`docs/glossary.md`](docs/glossary.md) | Vocabulaire du domaine et identifiants de code | Avant de nommer quoi que ce soit |
| [`docs/metier.md`](docs/metier.md) | Vision, cycle de vie d'un élément, règles métier `RG-xx` | Avant de toucher au comportement |
| [`docs/spec-v1.md`](docs/spec-v1.md) | Spécification technique de la V1 : données, pipeline, API, appli, déploiement, coûts | Pour implémenter |
| [`docs/adr/`](docs/adr/README.md) | Décisions d'architecture et alternatives écartées | Avant de remettre un choix en question |
| [`docs/backlog.md`](docs/backlog.md) | Epics et user stories avec critères d'acceptation, dans l'ordre | Pour savoir quoi faire ensuite |
| [`docs/prompt/`](docs/prompt/README.md) | Prompt, schémas JSON des fiches, jeu de test | Avant de toucher à l'analyse |
| [`docs/dev/`](docs/dev/workflow.md) | Workflow de développement, tests, conventions, git, outils des agents | Pour développer |

**En cas de contradiction** : le [document métier](docs/metier.md) fait foi pour le comportement, les [ADR](docs/adr/README.md) pour les choix techniques, les [schémas](docs/prompt/schemas/) pour le format des fiches. La spec est mise à jour pour s'y aligner.

## Conventions

- Documentation et interface en **français**, code en **anglais** ([ADR-0026](docs/adr/0026-langues-doc-et-code.md)). Correspondance des termes dans le [glossaire](docs/glossary.md).
- Toute évolution d'API commence par le contrat OpenAPI ([ADR-0017](docs/adr/0017-contrat-openapi.md)).
- Toute modification du prompt ou d'un schéma de fiche passe le [jeu de test](docs/prompt/eval-set.md).
- Une décision structurante nouvelle ou remise en cause donne lieu à un ADR ([ADR-0001](docs/adr/0001-enregistrer-les-decisions.md)).

## Feuille de route

| Version | Contenu |
| --- | --- |
| **V1 — Capture** | Capture, analyse de nuit, fiches par type, revue, lecture, recherche, hors ligne, export |
| V1.5 — Recherche | Recherche par le sens, éléments liés, reels Instagram, contenus très longs, mémo vocal |
| V2 — Veille | Sujets suivis, digest du matin, actualité générale |
| V3 — Boucle | Avis de pertinence, questions de révision, serveur MCP |

## Démarrage

### Prérequis

| Outil | Version | Installation |
| --- | --- | --- |
| Go | 1.27 | `brew install go` |
| Node et npm | 22 | déjà sur le Mac (`app/.nvmrc`) |
| Docker Desktop | 28 ou plus | déjà sur le Mac |
| Outils de qualité | golangci-lint 2.15, lefthook, gitleaks, lychee, shellcheck, actionlint | `brew install golangci-lint lefthook gitleaks lychee shellcheck actionlint` |
| OpenSpec CLI | 1.14.0 | `npm install -g @fission-ai/openspec@1.14.0` |
| graphify (facultatif) | 0.9 | `uv tool install "graphifyy[sql]"` |
| Xcode, CocoaPods | dernière version | App Store, puis `sudo xcode-select -s /Applications/Xcode.app/Contents/Developer` et `brew install cocoapods watchman` (uniquement pour compiler l'appli) |

### Premier lancement

```bash
make setup                          # hooks git (lefthook) et dépendances npm de api/ et app/
cp infra/.env.example infra/.env    # puis renseigner API_TOKEN (openssl rand -hex 32)
make check                          # tout ce que vérifie la CI
```

### Backend

```bash
make dev                                      # backend local sur 127.0.0.1:8080, base dans data/dev/
make up                                       # ou : l'image Docker, compose local sans Tailscale
curl -fsS http://127.0.0.1:8080/healthz      # {"status":"ok","version":"…"}
make down
```

Sur le NAS : `infra/docker-compose.yml` (sidecar Tailscale, image `ghcr.io/ghislain95/hippo`), mis à jour par `docker compose pull && docker compose up -d`.

### Appli

```bash
cd app && HIPPO_API_URL=http://127.0.0.1:8080 HIPPO_API_TOKEN=<API_TOKEN> npx expo run:ios   # development build (Xcode)
make app                                      # serveur de développement Expo
```

L'installation sur l'iPhone avec SideStore et le compte Apple gratuit est décrite par l'epic E1 (`docs/runbook-ios.md`, à écrire).

### Toutes les commandes

`make help`. Le cycle de développement d'un change est dans [`docs/dev/workflow.md`](docs/dev/workflow.md).
