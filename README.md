# Hippo

> Un deuxième cerveau personnel : on capture dans la journée, Hippo range pendant la nuit.

*Hippo est un nom de code provisoire, en référence à l'hippocampe, la zone du cerveau qui consolide les souvenirs pendant le sommeil.*

## Statut

**Phase de conception terminée, développement pas encore commencé.** La documentation ci-dessous décrit la V1 à construire. Les commandes de développement seront ajoutées lors du setup du projet.

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
├── backend/   service Go                         (à créer)
├── app/       appli Expo et extension de partage  (à créer)
├── api/       contrat OpenAPI                     (à créer)
├── infra/     docker compose, Tailscale           (à créer)
└── docs/      documentation
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

*À compléter lors du setup du projet* : prérequis (Go, Node, Xcode, Docker), variables d'environnement, lancement local du backend avec Docker Compose, build de l'appli, installation via SideStore.
