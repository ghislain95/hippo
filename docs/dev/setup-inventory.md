# Inventaire du setup : ce qu'on reprend de geomni et wifisense

Phase 1 du setup de Hippo (25 octobre 2026). Ce document recense tout ce qui relève du **workflow de développement** dans `../geomni` et `../wifisense` (lus sans rien modifier), et propose pour chaque élément ce que Hippo en fait.

- **Reprendre** : copié tel quel, au nom près.
- **Adapter** : l'idée est gardée, le contenu change (stack, règles de Hippo).
- **Ne pas reprendre** : écarté, avec la raison.
- **Nouveau** : absent des deux projets, utile pour un développement autonome.

En cas de conflit, la doc de Hippo gagne. Les conflits trouvés sont listés en [§5](#5-conflits-avec-la-doc-de-hippo) et attendent un arbitrage.

## 0. Environnement du Mac (phase 0)

Vérifié le 25 octobre 2026 sur macOS 27.0.1, Apple Silicon.

| Outil | État | Version | Commentaire |
| --- | --- | --- | --- |
| git | ✅ | 2.50.0 | `user.email` local : adresse perso ; remote SSH `github.com-ghislain95:ghislain95/hippo.git` |
| gh | ✅, ⚠️ compte | 2.101.0 | Connecté en **`ghislain-sartorius-adeo`**, pas en `ghislain95` (propriétaire du dépôt). Voir [Q8](#6-questions) |
| Go | ❌ | — | Indispensable (backend, génération, outils) |
| Node | ✅ | 22.17.0 | Compatible Expo SDK récent (Node ≥ 20.19) |
| npm / pnpm / yarn | ✅ | 10.9.2 / 10.26.1 / 1.22.22 | Choix du gestionnaire : [D5](#2-divergences-à-arbitrer) |
| Xcode | ❌ | — | Seuls les Command Line Tools sont installés (`xcode-select -p` → `/Library/Developer/CommandLineTools`). Nécessaire pour compiler le development build iOS et le simulateur |
| CocoaPods | ❌ | — | Requis par `expo run:ios` |
| watchman | ❌ | — | Recommandé par React Native, facultatif |
| Docker / Compose | ✅ | 28.3.2 / 2.38.2 | Démon actif |
| OpenSpec CLI | ✅ | 1.14.0 | Même version que celle épinglée dans la CI de wifisense |
| make | ✅ | GNU Make 3.81 | Version Apple, ancienne : éviter les fonctions de GNU Make 4 |
| jq | ✅ | 1.7.1 | Requis par le hook git repris de wifisense |
| perl | ✅ | 5.34 | Requis par le même hook |
| sqlite3 | ✅ | 3.54.0 | Pratique pour inspecter la base, pas requis |
| Python 3 | ✅ | 3.13.5 | Pas requis |
| golangci-lint | ❌ | — | Lint Go |
| lefthook | ❌ | — | Hooks pré-commit (aucun des deux projets n'en a) |
| gitleaks | ❌ | — | Détection de secrets |
| actionlint, shellcheck | ❌ | — | Facultatifs (lint des workflows et des scripts de hook) |
| Tailscale, EAS CLI | ❌ | — | Inutiles pour le setup (Tailscale sert en E1 et E6, EAS n'est pas utilisé) |
| graphify | ❌ | — | Utilisé par geomni et wifisense, proposé « plus tard » ([§1.4](#14-commandes-et-skills)) |

### À installer (rien n'est installé sans ton accord)

Indispensable pour la phase 3 :

```bash
brew install go
```

Pour le development build iOS (nécessaire à la compilation de l'appli, pas au lint, aux types ni aux tests) :

```bash
xcodes install --latest   # ou depuis l'App Store : Xcode (~15 Go), puis :
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
brew install cocoapods watchman
```

Outils de qualité, deux options :

- **Option A, globale** : `brew install golangci-lint lefthook gitleaks` (versions Homebrew du jour : 2.15.0, 2.2.2, 8.30.1).
- **Option B, épinglée dans le dépôt** (ma recommandation) : les trois sont des binaires Go. On les déclare dans un module d'outils (`tools/go.mod`, directive `tool` de Go ≥ 1.24) et on les lance par `go tool …` via le Makefile. Même version sur le Mac et en CI, aucune installation globale au-delà de Go. Pour lefthook, un `make setup` exécute `go tool lefthook install` une fois par clone.

## 1. Inventaire par domaine

### 1.1 Contexte des agents

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| Fichier racine | `AGENTS.md` (règles « cross-tool », en anglais) + `.claude/CLAUDE.md` qui l'importe (`@../AGENTS.md`) et ajoute le workflow et le routage | `CLAUDE.md` racine de 300 lignes (posture, cycle OpenSpec, règles, git, routage, graphify, boîte à outils) + `AGENTS.md` qui décrit l'équipe d'agents | **Adapter** : un seul `CLAUDE.md` racine court (~100 lignes) : identité, table de routage vers `docs/`, règles d'or, cycle en résumé, liens vers `docs/dev/` | Le CLAUDE.md de wifisense est chargé à chaque session et duplique `CONTRIBUTING.md`. Ta consigne : le détail va dans `docs/dev/`. Pas d'`AGENTS.md` : un seul outil d'agent, et l'équipe est décrite dans `.claude/agents/README.md` |
| Table de routage des docs | Tableau « Need → Read » | Liste de liens en tête | **Adapter** : tableau « quand lire quoi », repris du `README.md` de Hippo et complété par `docs/dev/` | Le README de Hippo a déjà ce tableau ; on l'étend, on ne le réécrit pas |
| Table de routage des agents | Demande → agent | Demande → agent, règles de routage, « quand ne pas déléguer », annonce « Routing → agent » | **Reprendre** (wifisense), avec les agents de Hippo | Le « quand ne pas déléguer » évite de dispatcher pour une lecture de fichier |
| Règles non négociables | Liste courte par couche | Liste longue par thème + anti-patterns à signaler | **Adapter** : les règles d'or de ta consigne (doc qui fait foi, glossaire, contrat d'abord, pas de mock de SQLite, jeu de test avant le prompt, privé jamais envoyé, `user_id`, langues) + les anti-patterns propres à Go et Expo | Les règles de Spring (Lombok, `@Transactional`…) n'ont pas d'équivalent |
| `CLAUDE.md` par sous-dossier | `backend/`, `frontend/` : règles critiques en 20 lignes, renvoi à `/project:ticket` | `wifisense-backend/`, `wifisense-frontend/` | **Reprendre** le format court, pour `backend/`, `app/`, `api/`, `infra/`, `docs/` | Ta consigne |
| Fichiers à ne pas lire automatiquement | Liste explicite (gros fichiers produit) | — | **Adapter** : exclure le code généré et `app/ios/` du contexte (lecture inutile) | Pas de gros document produit à exclure chez Hippo |
| Posture de l'agent | — | « ingénieur full-stack senior, la solution la plus simple qui marche » | **Reprendre** | Court et utile |
| Langue | Anglais partout sauf contenu produit | Prose en français, code et commits en anglais, **artefacts OpenSpec en anglais** | **Adapter** selon ADR-0026 | Conflit sur la langue des artefacts OpenSpec : [C2](#5-conflits-avec-la-doc-de-hippo) |

### 1.2 Configuration de Claude Code (`.claude/`)

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| `settings.json` partagé | Uniquement le hook git | Uniquement le hook git | **Nouveau** : liste `allow` choisie (make, go, npm/npx, openspec, git en lecture et en écriture locale, gh en lecture et `gh pr create`, docker compose…) et liste `deny` (lecture et édition de `.env*` et de secrets, `git push --force`, push sur `main`, `rm -rf` hors du repo, écriture dans `../geomni` et `../wifisense`) | Les deux projets accumulent les permissions au fil de l'eau dans `settings.local.json` (277 entrées pour geomni, avec des chemins absolus). Une liste partagée et revue évite d'être sollicité à chaque commande |
| `settings.local.json` | 277 permissions + `GH_TOKEN` | Permissions + `GH_TOKEN` (token GitHub limité au dépôt) | **Reprendre** le principe du `GH_TOKEN` local, gitignoré | Résout l'accès `gh` au bon compte (voir [Q8](#6-questions)). Valeur jamais commitée ni lue par les agents |
| `.worktreeinclude` | — | Copie `settings.local.json` dans chaque worktree | **Reprendre** | Les sessions parallèles de l'appli travaillent en worktree |
| `launch.json` | Serveurs de preview | Serveurs Vite console et mobile | **Adapter** : backend local (`go run`) et Expo (`expo start`) | Pour l'outil de preview de l'appli |
| Hook garde-fou git | `block-git-writes.sh` simple (33 lignes) : bloque **tout** `commit` et `push` (commit manuel) | `block-git-writes.sh` (387 lignes) + lecteur shell `block-git-writes.pl` + suite de tests : autorise commit et push de branche, bloque commit et push sur `main`, force-push, réécriture d'historique, staging en masse, `--no-verify`, `gh pr merge` hors `--merge` | **Reprendre** la version wifisense (messages et renvois adaptés à Hippo) | Arbitrage du 25 octobre : l'agent committe, pousse, ouvre et fusionne la PR, comme dans wifisense. La version wifisense est éprouvée et testée en CI |
| Hook de formatage après édition | — | — | **Nouveau** : `PostToolUse` sur Edit/Write → `gofmt`/`goimports` pour `.go`, Prettier pour `.ts/.tsx/.json/.yaml` de `app/` | Ta consigne. Supprime toute une classe d'aller-retours de lint |
| Hook de blocage des fichiers générés | — | — | **Nouveau** : `PreToolUse` sur Edit/Write → refus sur `*.gen.go`, client TS généré, schéma assemblé, avec le message « régénère : `make generate` » | Ta consigne. Il n'y a pas de code généré dans les deux autres projets |
| Hook de blocage de `.env` | — | — | **Nouveau** : `PreToolUse` sur Read/Edit/Bash → refus sur `.env*` (hors `.env.example`) ; doublé par les `deny` de `settings.json` | Ta consigne. Les `deny` seuls ne couvrent pas `cat .env` dans Bash |
| Passations | `.claude/passation/` gitignoré | Idem + skill `/passation` | **Reprendre** | Voir [§1.4](#14-commandes-et-skills) |
| Worktrees de l'appli | `.claude/worktrees/` | Idem, gitignoré | **Reprendre** l'entrée `.gitignore` | — |

### 1.3 Agents (`.claude/agents/`)

Les deux projets ont le même jeu de base (wifisense dérive de geomni) ; wifisense a des prompts plus riches et un `README.md`. Tous en `model: opus`, écrit explicitement dans chaque fichier.

| Agent | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| `product-owner` | Cadrage, user story | Idem + « ne jamais demander à l'utilisateur ce que le code ou les specs disent » | **Ne pas reprendre** | Le cadrage se fait avec toi, dans la session principale, et la backlog contient déjà les stories avec leurs critères. La règle « les faits sont le travail de l'agent » passe dans `workflow.md` |
| `architect` | Design, deltas, passe adversariale | Idem + décisions sourcées à la version du dépôt, découpage « ajouter → migrer → retirer », format d'ADR | **Adapter** : design OpenSpec, passe adversariale, ADR au format d'ADR-0001 | Les changes structurants (contrat, schéma, prompt) en ont besoin. Format d'ADR : [C3](#5-conflits-avec-la-doc-de-hippo) |
| `backend-dev` | Java / Spring / Flyway | Idem, plus riche | **Adapter** → `backend-dev` Go : `net/http`, `slog`, SQLite, migrations, oapi-codegen, interface `Analyzer` | Stack différente, même rôle |
| `frontend-dev` | React web | React console + PWA | **Adapter** → `mobile-dev` Expo : expo-router, client généré, TanStack Query, templates de fiches | Stack différente |
| `full-stack-dev` | Petite feature back + front | Idem | **Ne pas reprendre** | La session principale enchaîne `backend-dev` puis `mobile-dev` ; un agent de plus brouille le routage |
| `rendering-dev` | Babylon.js, MapLibre | — | **Ne pas reprendre** | Pas de 3D ni de carto |
| `tester` | Cucumber, JUnit, Vitest | Idem + « l'ordre se teste selon le contrat », preuve par mutation | **Adapter** : tests Go sur vraie SQLite, jest-expo, rendu des templates, cas du jeu de test | Ta consigne |
| `code-reviewer` | Checklist par couche | Format « une ligne par constat » avec sévérité, **baisse de qualité masquée** (test sauté, assertion affaiblie, `catch` muet = BLOQUANT), conformité au change | **Adapter** : même format et mêmes sévérités, plus la vérification contre les `RG-xx`, les ADR et la définition de « terminé » | Ta consigne. La détection de baisse de qualité masquée est le meilleur garde-fou d'un dev autonome |
| `doc-writer` | Doc, READMEs | Idem + glossaire, aucune duplication | **Adapter** → `doc-keeper` (gardien de la doc) : glossaire, ADR, backlog, spec, cohérence README | Ta consigne : le rôle va au-delà de l'écriture, il vérifie la cohérence |
| `devops` | docker-compose, CI | Idem | **Adapter** : Dockerfile distroless, compose et Tailscale, workflows, Dependabot | Infra petite mais récurrente (E6) |
| *(nouveau)* `prompt-engineer` | — | — | **Nouveau** : prompt, schémas de fiches, jeu de test, `/eval-prompt`, versions | Ta consigne. Le prompt est un contrat à trois parties (prompt, backend, appli) |
| `README.md` des agents | — | Tableau des rôles | **Reprendre** | Remplace l'`AGENTS.md` |

### 1.4 Commandes et skills

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| `/opsx:*` + skills `openspec-*` | Générés par `openspec init` | Idem | **Reprendre** (générés par `openspec init`, non édités) | Le cycle OpenSpec |
| `/ticket` (hotfix) | Oui | Écarté (couvert par les agents) | **Ne pas reprendre** | wifisense l'a retiré pour la même raison ; le hotfix est décrit dans `workflow.md` |
| `/review` (self-review en deux passes) | Oui | Écarté (agent `code-reviewer`) | **Ne pas reprendre** tel quel ; idée reprise dans `/dod-check` | Voir ci-dessous |
| `/migrate` (Flyway) | Oui | Écarté | **Ne pas reprendre** | Le mécanisme de migration est décrit dans `conventions-backend.md` et garanti par la CI |
| `/passation` | — (dossier seulement) | Skill maison | **Reprendre** (chemins et règles adaptés) | Repartir d'un contexte propre entre deux sessions |
| `/retro` | — | Skill adapté de mattpocock | **Reprendre** | Faire évoluer l'environnement des agents après un change difficile |
| `domain-modeling` | Skill (glossaire) | Skill (glossaire) | **Adapter** sur `docs/glossary.md` | Le glossaire de Hippo est une règle d'or |
| `improve-codebase-architecture` | Oui | Oui | **Ne pas reprendre** maintenant | Utile une fois qu'il y a du code (après E2-E3) |
| Skills de design et d'animation (`emil-design-eng`, `animate`, `break-ui`, `web-design-guidelines`…) | Oui | Oui | **Ne pas reprendre** maintenant | À évaluer au début d'E5 (maquettes, puis code) ; le web n'est pas notre cible |
| graphify | Skill + graph local + rafraîchissement dans le cycle | Idem | **Ne pas reprendre** maintenant | Aucun code à cartographier ; un graph périmé induit en erreur. À reconsidérer quand le backend aura quelques milliers de lignes |
| Catalogue des outils (`AGENT_TOOLBOX.md`) | Oui | Oui (principes, retenus, différés, écartés) | **Reprendre** la section « principes » dans `workflow.md`, sans document séparé | « Peu de skills », « une règle mécanique devient un contrôle » : utiles, et tiennent en dix lignes |
| *(nouveau)* `/eval-prompt` | — | — | **Nouveau** (spécifié par `docs/prompt/eval-set.md`) | Ta consigne. Coûte de l'argent : l'agent demande avant chaque passage réel |
| *(nouveau)* `/new-content-type` | — | — | **Nouveau** (ADR-0011 : schéma, prompt, template, Markdown, tests) | Ta consigne |
| *(nouveau)* `/new-adr` | — | — | **Nouveau** (format d'ADR-0001, numéro suivant, index) | Ta consigne |
| *(nouveau)* `/dod-check` | — | — | **Nouveau** : vérifie la définition de « terminé » avant la PR (CI locale verte, doc, backlog, RG cités, aucun fichier généré édité à la main) | Ta consigne ; reprend l'idée du `/review` de geomni |

### 1.5 OpenSpec

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| `openspec/config.yaml` | `schema: spec-driven`, contexte (stack, architecture, règles universelles), règles par artefact | Idem, plus précis (SHALL/MUST en première ligne, décisions sourcées, dernière tâche = vérifications de la CI) | **Adapter** : contexte tiré de la doc de Hippo ; règles : citer les `RG-xx` et les ADR, tâches qui finissent par les tests et la doc, dernière tâche = `/dod-check` | Ta consigne |
| Règle *proposal* | Section « Non-goals », ligne de backlog citée, impacts quota/auth/données | Idem, epic de roadmap cité, impacts fiabilité/latence | **Adapter** : « Non-objectifs », story de la backlog (`E2-01`…), `RG-xx` et ADR cités, impacts **coût IA, privé, données persistées, prompt** signalés | Les risques de Hippo sont le coût, la confidentialité et la perte d'information |
| Règle *specs* | Given/When/Then traduisibles en Gherkin, comportement observable, cas d'erreur | Idem + SHALL/MUST en première ligne | **Adapter** : comportement observable (routes, codes problem+json), cas d'erreur, un scénario = un test | Pas de Gherkin chez Hippo (voir [D13](#2-divergences-à-arbitrer)) |
| Règle *design* | Ordre hexagonal, Flyway oui/non | Packages racine fixes, Flyway oui/non, sources citées | **Adapter** : packages Go concernés, migration oui/non, contrat OpenAPI modifié oui/non, prompt ou schéma modifié oui/non, sources citées à la version de `go.mod`/`package.json` | — |
| Règle *tasks* | Groupes terminés par des tests, dernière tâche = self-review | Idem + revue `code-reviewer`, dernière tâche = vérifications de la CI | **Adapter** : contrat d'abord, tests en premier, doc à la fin, revue, `/dod-check` | Ta consigne |
| Garde CI « specs modifiées à la main » | Oui | Oui (avec `--no-renames`) | **Reprendre** (version wifisense) | Simple et efficace |
| `openspec validate --all` en CI | CLI `@latest` | CLI épinglée 1.14.0 | **Reprendre** (version épinglée) | Un `@latest` a déjà cassé la CI de wifisense |
| Post-archive | Statut backlog, impact doc, graph | Roadmap, impact doc, `## Purpose`, graph | **Adapter** : story cochée dans `backlog.md`, impact doc (`doc-keeper`), glossaire, `## Purpose` | Sans graphify |
| Guide OpenSpec | `Geomni_OpenSpec_Guide.md` | `OPENSPEC_GUIDE.md` (« j'ai envie de… → quoi faire », pièges) | **Adapter** dans `docs/dev/workflow.md` | Un seul document pour le cycle |
| Change = branche | Branche de change | `<type>/<change-id>` | **Reprendre** | Voir [§1.7](#17-conventions-git) |

### 1.6 Documentation de dev

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| Stratégie de test | Playbook de 1 558 lignes (Cucumber + Testcontainers, nettoyage, WireMock, Awaitility) | Playbook de 1 043 lignes, même structure | **Adapter** → `docs/dev/testing.md`, beaucoup plus court | La mécanique Spring ne se transpose pas ; les principes oui : pyramide (intégration d'abord), jamais de base embarquée à la place de la vraie (ici : jamais de mock de SQLite), pas de `sleep`, réseau interdit en test, ordre testé selon le contrat, fixtures sur disque, anti-patterns |
| Architecture du code | `Geomni_Backend_Architecture.md`, `…_Frontend_Architecture.md` | `CODING_RULES.md`, `MODULES.md`, `ARCHITECTURE.md` par module | **Adapter** → `conventions-backend.md`, `conventions-app.md` | Ta consigne |
| Conventions git | Dans `AGENTS.md` (une ligne) | `CONTRIBUTING.md` §2, très complet | **Adapter** → `docs/dev/git.md` | Voir [§1.7](#17-conventions-git) |
| Définition de « terminé » | Implicite (self-review, tests) | Implicite (conditions de fusion) | **Nouveau** : section explicite dans `workflow.md`, reprise dans le template de PR et `/dod-check` | La backlog de Hippo en donne déjà le noyau (tests, CI, doc) |
| ADR | `docs/particulier/…ADR…` (D1–D13) | `docs/adr/ADR-NNN-…` + template dans l'agent `architect` | **Reprendre** ceux de Hippo (`docs/adr/`, ADR-0001) | Déjà en place |
| Variables d'environnement | `Geomni_Environment_Variables.md` | `CONFIGURATION.md` | **Adapter** : `.env.example` commenté, qui fait office de référence | Une seule source |
| Glossaire | `Geomni_Glossary.md` | `GLOSSARY.md` | Déjà en place (`docs/glossary.md`) | — |

### 1.7 Conventions git

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| Commits | Conventional Commits sur une ligne, anglais ; **jamais par l'agent** | Conventional Commits, **sujet + corps** toujours, anglais, scope = id du change sinon module ; commit par l'agent | **Reprendre** (wifisense) ; scopes hors change : `backend`, `app`, `api`, `infra`, `docs`, `prompt`, `tooling` | ADR-0026 renvoie au setup « en cohérence avec l'autre projet » |
| Attribution Claude dans les commits | — | **Interdite** (ni `Co-Authored-By`, ni « Generated with Claude Code ») | À arbitrer | [D2](#2-divergences-à-arbitrer) |
| Branches | — | `<type>/<change-id>`, hors change `<type>/<scope>-<desc>` | **Reprendre** | — |
| Ordre des commits d'un change | — | `docs(<id>)` cadrage → `feat|fix(<id>)` par tranche → `chore(<id>)` archive | **Reprendre** | Lisible en revue |
| Règles d'un commit | — | Staging par chemins explicites, pas d'`--amend`, de `rebase` ni de force-push, `git commit -F` pour les messages longs | **Reprendre** | Imposé par le hook |
| PR | — | Titre = sujet, description en français, 1 PR = 1 sujet, CI verte ; **fusion par l'agent** (`--merge`) | **Adapter** : PR ouverte par l'agent, **fusion par toi** | Ta consigne |
| Template de PR | — | — | **Nouveau** : `.github/pull_request_template.md` avec la checklist de « terminé » | Ta consigne |
| Hooks pré-commit | — | — | **Nouveau** : lefthook (format, lint rapide, gitleaks sur les fichiers indexés) | Ta consigne ; absent des deux projets |

### 1.8 CI/CD (GitHub Actions)

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| Filtres par chemin | `paths:` sur chaque workflow | Idem, **en incluant le fichier du workflow lui-même** | **Reprendre** (wifisense) | Modifier un workflow le fait tourner |
| Hygiène des workflows | — | `permissions: contents: read`, `timeout-minutes`, en-tête qui explique le workflow | **Reprendre** | Moindre privilège, pas de job bloqué |
| Backend | Compile, JUnit, Cucumber, `verify` | `./mvnw -B test` (JUnit + ArchUnit + Cucumber) | **Adapter** : golangci-lint, `go test -race ./...` (unitaires + intégration SQLite), `go build` | — |
| Frontend | `tsc --noEmit`, Vitest, build | `pnpm install --frozen-lockfile`, type-check, lint, test | **Adapter** : installation verrouillée, `tsc --noEmit`, ESLint, jest-expo, `expo-doctor` | Pas de build iOS en CI (Xcode sur runner macOS : coûteux et lent) |
| Garde d'architecture | `grep` en CI | ArchUnit dans les tests | **Adapter** : règles `depguard` de golangci-lint (ex. : seul `internal/analyzer` importe le SDK Anthropic, le domaine n'importe pas `net/http`) | Équivalent Go d'ArchUnit, sans outil de plus |
| OpenSpec | validate + garde | Idem, CLI épinglée | **Reprendre** | — |
| Migrations immuables | Garde « aucune migration modifiée » | Garde « aucune migration modifiée, supprimée ni renommée » | **Reprendre** (wifisense), sur les migrations SQL de Hippo | Même risque avec n'importe quel outil de migration |
| Messages de commit | Job qui affiche les messages (ne vérifie rien) | — | **Ne pas reprendre** | Ne contrôle rien |
| Garde CORS | Oui | — | **Ne pas reprendre** | Propre à geomni |
| Tests du hook git | — | `tooling.yml` | **Reprendre** | Le hook est le seul verrou côté agent |
| Déploiement | Cloud Run automatique depuis `main` | Aucun | **Adapter** : image construite sur chaque PR, publiée sur GHCR depuis `main` seulement ; aucun déploiement automatique (ADR-0019) | Ta consigne |
| Contrat OpenAPI | — | — | **Nouveau** : lint + « code généré à jour » | ADR-0017 |
| Schémas de fiches | — | — | **Nouveau** : validation + « assemblage à jour » | `docs/prompt/README.md` |
| Liens de la doc | — | — | **Nouveau** : vérification des liens internes | Ta consigne |
| Secrets | — | — | **Nouveau** : gitleaks en CI | Défense en profondeur avec le hook pré-commit |
| Dependabot / Renovate | — | — | **Nouveau** : Dependabot (gomod, npm, github-actions, docker), hebdomadaire, groupé | Natif GitHub, zéro configuration externe |

### 1.9 Outillage local

| Élément | geomni | wifisense | Hippo | Pourquoi |
| --- | --- | --- | --- | --- |
| Point d'entrée unique | — (commandes dans `AGENTS.md`) | — (scripts pnpm, `./mvnw`) | **Nouveau** : `Makefile` racine (`make setup`, `check`, `test`, `lint`, `fmt`, `generate`, `dev`, `up`…) | Ta consigne. Make est déjà sur le Mac ; un task runner (Task, just) demanderait une installation de plus |
| `.editorconfig` | Oui | — | **Reprendre** (geomni), avec les tabulations pour Go et Makefile | — |
| `.gitattributes` | `backend/` seulement | `backend/` seulement | **Nouveau** à la racine : fins de ligne LF, `linguist-generated` sur le code généré (replié dans les diffs de PR) | — |
| `.gitignore` | Racine + sous-dossiers | Racine riche (secrets, Claude, graph) | **Adapter** : Go, Node, Expo, iOS, macOS, `.env`, `data/`, `.claude/passation/`, `.claude/worktrees/`, `settings.local.json` | Ta consigne |
| `.env.example` | `infra/.env.example` | — | **Adapter** : `infra/.env.example`, toutes les variables de la spec, commentées | Ta consigne |
| Version des outils | — | `packageManager` dans `package.json` | **Adapter** : version de Go dans `go.mod`, de Node dans `.nvmrc` et `engines`, du gestionnaire dans `packageManager` | CI et Mac alignés |

## 2. Divergences à arbitrer

| # | Sujet | geomni | wifisense | Ma proposition |
| --- | --- | --- | --- | --- |
| D1 | Autonomie git de l'agent | Jamais de commit par l'agent | L'agent committe, pousse, ouvre **et fusionne** la PR | Le modèle wifisense **sans la fusion** : c'est ce que ta consigne décrit. À confirmer |
| D2 | Attribution Claude dans les commits et PR | Sans objet (commits manuels) | Interdite | À trancher ([Q4](#6-questions)). L'outil ajoute par défaut `Co-Authored-By: Claude …` ; je suivrai ta règle |
| D3 | Format des commits | Une ligne | Sujet + corps obligatoires | wifisense : le corps explique les décisions, utile pour un relecteur |
| D4 | Stratégie de fusion | — | Merge commit (`--merge`) | Merge commit : garde les commits atomiques d'un change. À régler dans GitHub si tu le souhaites (action manuelle) |
| D5 | Gestionnaire de paquets JS | npm | pnpm | **npm** pour `app/` : un seul paquet (pas de workspace), et c'est le chemin le moins risqué avec Expo, l'autolinking et les config plugins (dont l'extension de partage d'E1). À arbitrer ([Q6](#6-questions)) |
| D6 | Fichier d'instructions racine | `AGENTS.md` + `.claude/CLAUDE.md` | `CLAUDE.md` + `AGENTS.md` | `CLAUDE.md` seul (ta consigne) |
| D7 | Garde d'architecture | `grep` en CI | ArchUnit | `depguard` dans golangci-lint |
| D8 | Auto-relecture | Commande `/review` | Agent `code-reviewer` obligatoire | Agent `code-reviewer` + `/dod-check` |
| D9 | CLI OpenSpec en CI | `@latest` | Épinglée | Épinglée (1.14.0) |
| D10 | Hygiène des workflows | Minimale | Permissions, timeouts, chemins | wifisense |
| D11 | `.editorconfig` | Oui | Non | Oui |
| D12 | graphify | Utilisé | Utilisé | Plus tard (aucun code) |
| D13 | Couche de test principale | Cucumber (Gherkin) | Cucumber (Gherkin) | Tests Go standard (`testing` + `httptest`) sur vraie SQLite, chaque test nommé d'après son scénario OpenSpec. Alternative : `godog` (Gherkin en Go). À arbitrer ([Q5](#6-questions)) |

## 3. Lacunes communes

Ce qui manque dans les deux projets et servira un développement autonome. Tout est intégré au plan de la phase 2.

1. **Permissions partagées et revues** dans `settings.json` (aujourd'hui : accumulation dans `settings.local.json`).
2. **Interdiction de lire `.env` et les secrets**, par `deny` et par hook.
3. **Formatage automatique après édition** (hook).
4. **Protection du code généré** contre l'édition à la main (hook + CI « à jour »).
5. **Hooks pré-commit** (format, lint rapide, secrets).
6. **Détection de secrets en CI.**
7. **Mise à jour des dépendances** (Dependabot).
8. **Définition de « terminé » écrite**, reprise dans un template de PR et une commande de vérification.
9. **Point d'entrée unique** pour les commandes (`Makefile`) : un agent et la CI appellent les mêmes cibles.
10. **Liste explicite des cas où l'agent s'arrête** pour demander (wifisense n'a qu'un paragraphe « Restent à l'utilisateur »).
11. **Vérification des liens de la doc.**
12. **Mesure de couverture** (affichée, sans seuil bloquant au départ).
13. **Image Docker construite sur chaque PR** (wifisense : aucune ; geomni : seulement sur `main`).
14. **Lint des workflows et des scripts de hook** (actionlint, shellcheck), facultatif.

## 4. Adaptations imposées par la stack de Hippo

| Sujet | Dans geomni / wifisense | Chez Hippo |
| --- | --- | --- |
| Backend | Spring Boot, Maven, Lombok, hexagonal | Go, librairie standard (ADR-0003) : packages `internal/…`, `log/slog`, erreurs explicites, pas de framework. Lint : golangci-lint avec gofumpt/goimports en formateurs |
| Base de données | Postgres + Testcontainers | SQLite fichier (ADR-0004) : chaque test d'intégration ouvre une **vraie base temporaire** (`t.TempDir()`), migrations appliquées. Rien à démarrer, donc rapide. Jamais de mock du stockage |
| Migrations | Flyway | À choisir en phase 2 (outil léger compatible `modernc.org/sqlite`, ou petit migrateur maison sur `embed`). Garde CI d'immuabilité reprise |
| Contrat d'API | Contrôleurs écrits à la main | OpenAPI 3.1 d'abord (ADR-0017) : `oapi-codegen` (serveur strict, `net/http`), client TypeScript généré, lint du contrat, contrôle « généré à jour » |
| Schémas de fiches | — | Assemblage des `$ref` en un schéma autonome, validation des exemples, contrôle « assemblage à jour ». **Aucun exemple de fiche n'existe encore** : il en faudra un par type (ils serviront aussi aux tests de rendu des templates et de l'export Markdown) |
| Front | React web, Vite, Vitest, Playwright | Expo (ADR-0012) : development build, **jest-expo + React Native Testing Library** (Vitest est mal adapté à React Native), ESLint `eslint-config-expo`, `tsc`, `expo-doctor`. Pas d'E2E au départ |
| IA | — | Le prompt est un contrat : tout changement passe le jeu de test (`/eval-prompt`, **payant**, l'agent demande avant). En test, on remplace l'appel réseau par une fausse implémentation de l'interface `Analyzer` (ADR-0005), jamais la base |
| Confidentialité | Rôles, JWT | RG-12 : test obligatoire « aucun élément privé dans une requête vers l'IA » (E3-01) ; règle de relecture |
| Multi-utilisateur | Rôles | RG-90 : `user_id` dans chaque requête ; règle de relecture + test d'isolation à deux utilisateurs (E4-12) |
| Déploiement | Cloud Run (geomni) | Image distroless sur GHCR, compose avec sidecar Tailscale ; variante locale sans Tailscale, port publié sur `localhost` |
| Langues | geomni : anglais partout | Doc en français, code en anglais (ADR-0026) ; agents et commandes rédigés en français (ce sont des docs pour agents) |

## 5. Conflits avec la doc de Hippo

Hippo gagne ; je ne tranche pas en silence.

| # | Conflit | Proposition |
| --- | --- | --- |
| C1 | **Source de vérité du comportement.** Dans geomni et wifisense, `openspec/specs/` fait foi et rien ne s'implémente hors spec. Chez Hippo, `metier.md` (règles `RG-xx`) fait foi pour le comportement, la spec V1 et les ADR pour le technique. Deux sources concurrentes dériveraient. | `openspec/specs/` décrit le **comportement observable détaillé** (routes, codes, scénarios) et chaque exigence cite les `RG-xx` qu'elle met en œuvre. En cas de contradiction, `metier.md` gagne et la spec OpenSpec est corrigée. Une règle métier nouvelle naît dans `metier.md` (avec ton accord), jamais dans une spec OpenSpec. **À valider** ([Q1](#6-questions)) |
| C2 | **Langue des artefacts OpenSpec.** geomni et wifisense : anglais. ADR-0026 : la documentation est en français. Le validateur d'OpenSpec 1.14.0 exige `SHALL` ou `MUST` dans chaque exigence (vérifié : `/\b(SHALL\|MUST)\b/`). | OpenSpec 1.14.0 prévoit ce cas : avec une langue configurée à l'init, les artefacts sont rédigés dans cette langue, et seuls les titres structurels (`## ADDED Requirements`, `#### Scenario:`…) et les mots-clés `SHALL`/`MUST` restent en anglais (« Le backend MUST refuser… »). Je propose le français, conforme à ADR-0026. Alternative : tout en anglais, ce qui demande un nouvel ADR qui amende ADR-0026. **À trancher** ([Q2](#6-questions)) |
| C3 | **Format d'ADR.** L'agent `architect` de wifisense impose son propre modèle (modules impactés, contrats…). | Le format d'ADR-0001 s'applique. |
| C4 | **Visibilité du dépôt.** ADR-0002 dit « un seul dépôt GitHub **privé** » et ADR-0019 « GHCR (privé) ». Le dépôt `ghislain95/hippo` est **public** (vérifié avec `gh repo view`). Le prompt système contient ton profil, et le jeu de test contiendra des contenus que tu choisis. | Soit passer le dépôt en privé (mais en offre gratuite, pas de protection de branche côté serveur sur un dépôt privé : le hook reste le seul verrou, comme chez wifisense), soit rester public et amender ADR-0002 et ADR-0019 par un nouvel ADR. **À trancher** ([Q3](#6-questions)) |

## 6. Questions

1. **Q1 (C1)** : `openspec/specs/` pour le détail observable, `metier.md` qui gagne, chaque exigence qui cite ses `RG-xx` : d'accord ?
2. **Q2 (C2)** : artefacts OpenSpec en français (titres structurels et `MUST` en anglais, prévu par OpenSpec), ou tout en anglais avec un ADR qui amende ADR-0026 ?
3. **Q3 (C4)** : dépôt public ou privé ? L'ADR à écrire dépend de la réponse.
4. **Q4 (D2)** : commits et PR sans aucune mention de Claude (règle de wifisense), ou avec le trailer `Co-Authored-By` ?
5. **Q5 (D13)** : tests d'intégration Go standard, ou Gherkin avec `godog` comme dans tes deux autres projets ?
6. **Q6 (D5)** : npm (ma recommandation) ou pnpm pour l'appli ?
7. **Q7** : installations : Go (indispensable) ; Xcode, CocoaPods et watchman maintenant ou au début d'E1 ; outils de qualité en global (option A) ou épinglés dans le dépôt via `go tool` (option B, ma recommandation) ?
8. **Q8** : `gh` est connecté en `ghislain-sartorius-adeo`. Pour que les PR soient ouvertes au nom de `ghislain95`, je propose le dispositif de wifisense : un token GitHub limité au dépôt, que **tu** poses en `GH_TOKEN` dans `.claude/settings.local.json` (gitignoré). Ça te va ?
9. **Q9** : jeu d'agents : `backend-dev`, `mobile-dev`, `tester`, `code-reviewer`, `prompt-engineer`, `doc-keeper`, `architect`, `devops` ; sans `product-owner` ni `full-stack-dev`. D'accord ?

## 7. Arbitrages (25 octobre 2026)

Réponses de Ghislain aux questions ci-dessus. Elles priment sur les propositions des sections précédentes.

| Sujet | Décision |
| --- | --- |
| Autonomie git (D1) | Comme wifisense : l'agent committe, pousse, ouvre **et fusionne** la PR, seulement si la CI est verte et la revue sans constat bloquant. Ghislain participe au cadrage et à la QA |
| graphify, skills de design et d'animation | **Repris** dès le setup (au lieu de « plus tard ») |
| Lacunes ([§3](#3-lacunes-communes)) | Toutes ajoutées |
| Q1 — Source de vérité | `metier.md` porte le métier global et gagne en cas de contradiction ; `openspec/specs/` porte le comportement détaillé pour le développement, avec le cycle OpenSpec complet. Chaque exigence cite ses `RG-xx` |
| Q2 — Langue | **Tout ce qui touche au code est en anglais**, artefacts OpenSpec compris. En français : la documentation et les échanges avec Ghislain |
| Q3 — Dépôt | **Public**. ADR-0002 et ADR-0019 sont amendés par un nouvel ADR |
| Q4 — Attribution | Aucune mention de Claude dans les commits ni les PR |
| Q5 — Tests | Tests Go standard sur une vraie SQLite |
| Q6 — Gestionnaire JS | npm |
| Q7 — Installations | Autorisées ; outils de qualité installés globalement (Homebrew) |
| Q8 — Accès GitHub | Ghislain pose un token en `GH_TOKEN` dans `.claude/settings.local.json` |
| Q9 — Agents | Jeu proposé validé, à compléter si un besoin apparaît |
