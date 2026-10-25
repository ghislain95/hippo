# Hippo — Spec V1

Oct 10, 2026 · @Ghislain

## Contexte et objectifs

Hippo (nom de code provisoire) est le deuxième cerveau de Ghislain : il y envoie tout ce qu'il veut retenir, une IA le range pendant la nuit, et il le retrouve en quelques secondes. La V1 couvre la capture et le classement ; la veille quotidienne (V2) réutilisera le même pipeline.

Domaines de départ, sans liste figée : dev (Java, Spring Boot, PostgreSQL, React), IA appliquée au dev, infra et DevOps (montée en compétences visée, c.f prompt), actualité tech, astuces du quotidien et de la maison, cuisine, plantes, jeux vidéo, voyages et sorties (lieux à visiter à Lille, par exemple). D'autres domaines apparaîtront au fil des captures.

Objectifs mesurables :

- Capturer un lien, une image ou une note en moins de 5 secondes, depuis la feuille de partage iOS ou l'appli.
- Chaque élément est analysé et classé automatiquement au plus tard le lendemain matin.
- Retrouver un élément par mot-clé, tag, type ou catégorie en moins d'une seconde, même hors ligne.
- Aucune perte d'information technique utile dans les fiches.
- Coût LLM inférieur à 5 $ par mois, serveur sous 100 Mo de RAM.

Un seul utilisateur en V1, mais le modèle de données prévoit dès maintenant plusieurs utilisateurs (`user_id`), au cas où l'appli serait commercialisée.

## Périmètre

La V1 couvre la capture, l'analyse de nuit, la lecture et la recherche par mot-clé ; tout ce qui demande des embeddings, de l'audio ou des sources automatiques vient ensuite.

| Fonctionnalité | Version |
| --- | --- |
| Capture de liens via la feuille de partage iOS et dans l'appli | V1 |
| Captures d'écran et photos, lues par Haiku | V1 |
| Notes texte, dictées avec le clavier iOS | V1 |
| Option « Privé » (stocké, jamais envoyé à l'IA), décochée par défaut | V1 |
| Extraction des pages web et des transcriptions YouTube | V1 |
| Analyse de nuit : type, catégorie, tags, fiche structurée | V1 |
| 7 types de contenu, chacun avec son template, plus un template générique | V1 |
| Catégories et types proposés par Haiku, écran Revue | V1 |
| Statut et position de lecture, épingle, annotations | V1 |
| Recherche plein texte et filtres | V1 |
| Lecture hors ligne, thèmes clair et sombre | V1 |
| Export Markdown compatible Obsidian | V1 |
| Extraction des reels Instagram (compte secondaire) | V1.5 |
| Recherche par le sens et « Éléments liés » | V1.5 |
| Découpage des contenus très longs | V1.5 |
| Mémo vocal transcrit | V1.5 ou plus tard |
| Questions posées à la base | Plus tard |
| Veille, digest du matin, actualité générale | V2 |
| Surlignage de passages | V2 |
| Questions de révision, badge « peut-être obsolète » | V3 |
| Avis de pertinence, serveur MCP | V3 |
| Suggestions de sujets, radar techno, widget iOS | Après V2 |

## Architecture et stack

Un seul binaire Go sur le NAS porte l'API, le stockage SQLite et le job nocturne ; l'appli iPhone lui parle via Tailscale, et Haiku n'est appelé que par batch.

&#91;embedded content: architecture V1 · 3 zones\]

L'iPhone ne parle qu'au service Go ; seul le service Go sort sur Internet, pour lire les pages et appeler Haiku.

| Couche | Choix | Pourquoi |
| --- | --- | --- |
| Backend | Go, librairie standard `net/http` | Binaire unique, quelques dizaines de Mo de RAM, code lisible |
| Base de données | SQLite (mode WAL) + FTS5, images sur disque | Un fichier, zéro service, recherche plein texte intégrée |
| Extraction de contenu | go-readability | Envoie l'article seul à Haiku, pas le HTML complet ; transcriptions YouTube récupérées à part |
| LLM | Claude Haiku 4.5 via l'API Message Batches, SDK Anthropic Go | -50 % sur le prix, sortie JSON structurée, lit aussi les images |
| Planification | Cron interne au binaire | Pas de dépendance externe |
| Mobile | Expo (React Native) + extension de partage iOS | Stack connue, partage natif |
| Installation iOS | Compte Apple gratuit + SideStore | Pas d'abonnement à 99 $/an |
| Réseau | Tailscale sur le NAS et l'iPhone | Aucun port ouvert, pas de nom de domaine |
| Authentification | Token d'API statique (Bearer) | Utilisateur unique, réseau privé |
| Déploiement | Image Docker distroless + docker compose | Image d'environ 20 Mo, mise à jour simple |

## Modèle de données

La fiche générée est stockée telle que Haiku l'a renvoyée, en JSON validé : c'est la source de vérité de l'affichage. Le Markdown n'est jamais stocké, il est produit à l'export. Chaque table métier porte un `user_id`.

**items** — un élément capturé.

| Champ | Type | Rôle |
| --- | --- | --- |
| id | TEXT (ULID) | Identifiant triable par date |
| user\_id | TEXT | Propriétaire |
| source | TEXT | `share`, `manual` (V2 : `rss`) |
| url | TEXT, nullable | Unique par utilisateur quand présente |
| user\_note | TEXT | Note ajoutée à la capture |
| is\_private | INTEGER (0/1) | 1 = jamais envoyé à l'IA ; 0 par défaut |
| status | TEXT | `queued`, `extracted`, `submitted`, `analyzed`, `failed` |
| content\_text | TEXT | Texte extrait, gardé pour pouvoir réanalyser ; jusqu'à environ 120 000 caractères |
| content\_truncated | INTEGER (0/1) | Le contenu dépassait la limite |
| type\_id, category\_id | INTEGER | Type de contenu et catégorie |
| title | TEXT | Titre affiché |
| fiche\_json | TEXT (JSON) | Fiche générée, au format du type |
| search\_text | TEXT | Fiche aplatie en texte pour la recherche |
| fiche\_incomplete | INTEGER (0/1) | Le plafond de sortie a été atteint |
| freshness | TEXT | `dated` ou `timeless`, pour le futur badge « peut-être obsolète » |
| model, prompt\_version, schema\_version | TEXT, INTEGER | Traçabilité et réanalyse ciblée |
| user\_edited | TEXT (JSON) | Champs corrigés à la main, jamais écrasés par une réanalyse |
| annotations | TEXT | Tes notes sur la fiche |
| is\_pinned | INTEGER (0/1) | Épingle « à ne pas oublier » |
| read\_status | TEXT | `to_read`, `in_progress`, `read` |
| read\_progress | REAL | 0 à 1, position de reprise |
| language, reading\_minutes | TEXT, INTEGER | Langue source, temps de lecture |
| attempts, last\_error, batch\_id | INTEGER, TEXT, TEXT | Suivi des échecs et du batch |
| created\_at, analyzed\_at, read\_at | TEXT (ISO 8601) | Horodatage |

**categories** — arborescence de 2 niveaux maximum. Haiku peut en proposer, jamais en modifier, supprimer ni déplacer.

| Champ | Type | Rôle |
| --- | --- | --- |
| id, user\_id | INTEGER, TEXT | Clé et propriétaire |
| name, description | TEXT | La description aide Haiku à bien ranger |
| parent\_id | INTEGER, nullable | Catégorie parente |
| status | TEXT | `active` ou `proposed` (en attente de revue) |
| color | TEXT | Couleur d'accent dans l'appli |

**content\_types** — les 7 types de départ (`article_tech`, `tuto`, `astuce`, `recette`, `lieu`, `ressource`, `note`) ; Haiku peut en proposer un nouveau, affiché avec le template générique.

| Champ | Type | Rôle |
| --- | --- | --- |
| id, key, name | INTEGER, TEXT, TEXT | Identité du type |
| status | TEXT | `active` ou `proposed` |
| has\_template | INTEGER (0/1) | 0 = template générique |
| schema\_version | INTEGER | Version du schéma JSON de la fiche |

**Autres tables**

- **tags** et **item\_tags** : tags en minuscules, uniques, et leur jointure.
- **assets** : images capturées (chemin sur disque, type, dimensions, poids), rattachées à un élément.
- **batches** : identifiant Anthropic, statut, nombre d'éléments, tokens consommés, coût calculé, dates.
- **settings** : réglages clé-valeur, dont la date de la dernière revue.
- **items\_fts** : index FTS5 sur titre, `search_text`, tags et note, tokenizer `unicode61 remove_diacritics 2` pour ignorer les accents, tenu à jour par des triggers.

## Pipeline d'analyse

Un élément passe par cinq étapes ; seule l'étape 3 appelle Haiku, une fois par nuit, en un seul batch.

1. **Capture** — `POST /api/items` enregistre un lien, une note ou une image en `queued` et répond tout de suite. Une URL déjà présente renvoie l'élément existant. Un élément privé ne part jamais à l'IA : il reste dans « À trier » jusqu'à ce que tu le ranges.
2. **Extraction** — en arrière-plan : page web via go-readability (timeout 15 s), transcription pour YouTube quand elle existe, rien à extraire pour une image. En cas d'échec (paywall, Instagram, page en JavaScript), on garde `og:title`, `og:description` et ta note. Texte conservé jusqu'à environ 120 000 caractères ; au-delà, coupé et marqué.
3. **Envoi du batch** — chaque nuit à 2 h, tous les éléments `extracted` partent dans un seul batch, une requête par élément. Rien à envoyer, aucun appel.
4. **Suivi** — vérification toutes les 15 minutes jusqu'à la fin du batch (souvent moins d'une heure, 24 h au maximum).
5. **Résultats** — la réponse est validée contre le schéma du type, puis écrite. Les catégories et types proposés sont créés en `proposed`. Si Haiku a atteint son plafond de sortie, la fiche est marquée incomplète et relancée la nuit suivante avec un plafond doublé. En cas d'erreur : 3 essais, puis `failed` avec un bouton « Relancer ».

Un bouton « Analyser maintenant » court-circuite la nuit pour un élément : appel direct, plein tarif, environ un centime.

**Règles de classement**

- Haiku reçoit l'arborescence des catégories avec leurs descriptions, la liste des types et les tags existants.
- Il range dans l'existant en priorité. Si rien ne convient, il propose une catégorie (2 niveaux maximum) ou un type, avec une justification d'une ligne.
- Il ne peut ni renommer, ni supprimer, ni déplacer : toute proposition passe par la revue.
- Une seule catégorie par élément, celle du sujet principal ; le transversal passe par les tags.

**Structure des fiches par type**

| Type | Structure de la fiche |
| --- | --- |
| Article / actu technique | TL;DR en quelques lignes, ce qui change, détails techniques (versions, chiffres, commandes, code), limites et pièges, liens |
| Tuto / how-to | Objectif, prérequis, étapes numérotées avec commandes ou code, résultat attendu |
| Astuce | L'astuce en une phrase, étapes courtes, matériel éventuel |
| Recette | Portions, temps, ingrédients avec quantités, étapes |
| Lieu | Nom, ville, adresse, ce qu'on y fait, infos pratiques connues, meilleur moment |
| Ressource | Ce que c'est, à quoi ça sert, quand l'utiliser, lien, alternatives citées |
| Note perso | Idée reformulée proprement, points clés, pistes à creuser |
| Générique (type proposé) | Titre, sections libres, points clés |

**Budget de tokens**

- Entrée : le texte extrait seul, jamais le HTML, jusqu'à environ 120 000 caractères.
- Sortie : longueur proportionnelle au contenu, plafond de sécurité à 4 000 tokens ; environ 2 000 suffisent pour la plupart des articles.
- Réponse forcée via un outil (`tool_choice`) : JSON garanti, pas de texte superflu.
- Images : redimensionnées avant envoi pour limiter les tokens.

**Prompt système (brouillon v2)**

```
Tu ranges les contenus du deuxième cerveau de Ghislain.

Qui il est : développeur à prédominance back-end depuis 5 ans 
(Java, Spring Boot, API Rest, microservices, PostgreSQL, un peu de MongoDB, 
Kafka, Redis, SQL, modélisation et optimisation, ReactJS, Javascript, 
Cucumber / Gherkin, Docker, Kubernetes, Github Actions, GCP, et autres). 
Il suit de près l'IA, surtout appliquée au développement, 
et veut monter en compétences 
dans certains domaines comme l'IA, infra, DevOps...
Mais l'appli couvre toute sa vie : cuisine,
maison, plantes, jeux vidéo, voyages, actualité, et tout ce qu'il
veut retenir. Ne suppose jamais qu'un contenu doit être technique.

Pour le contenu fourni :
1. Choisis le type dans la liste. Si aucun ne convient, propose-en un.
2. Choisis UNE catégorie dans l'arborescence, d'après le sujet
   principal. Si aucune ne convient, propose-en une (2 niveaux
   maximum) et explique pourquoi en une ligne. Ne renomme, ne déplace
   et ne supprime jamais rien.
3. Donne 2 à 5 tags. Réutilise en priorité les tags existants.
   Nouveau tag : minuscules, mots séparés par des tirets, sans accent.
4. Rédige la fiche au format du type, en français, simplement, comme
   à un collègue. Garde en anglais les termes techniques, les noms
   d'API et le code.
5. Contenu technique : ne perds aucune information utile. Recopie tels
   quels versions, chiffres, commandes et extraits de code. La
   longueur suit la richesse du contenu.
6. Indique si le contenu est daté (actualité, version) ou intemporel.
7. Si le contenu est trop pauvre (paywall, page vide), mets
   content_ok à false et résume seulement ce que tu sais.
   N'invente rien.

Types : {{types}}
Catégories : {{categories}}
Tags existants : {{tags}}
```

**Format de sortie** (schéma de l'outil `classer_element`, exemple avec une catégorie proposée)

```json
{
  "type": { "key": "article_tech" },
  "category": {
    "proposed": { "name": "Kubernetes", "parent_id": 4, "reason": "Premier contenu sur l'orchestration" }
  },
  "tags": ["kubernetes", "devops", "helm"],
  "title": "Helm 4 : ce qui change pour les charts",
  "fiche": { "tldr": "…", "changes": ["…"], "technical_details": ["…"], "pitfalls": ["…"], "links": ["…"] },
  "language": "en",
  "reading_minutes": 9,
  "freshness": "dated",
  "content_ok": true
}
```

Pour une catégorie existante, `category` vaut `{ "id": 12 }` ; même logique pour `type`.

**Contrôle qualité** — un jeu de test d'une vingtaine de vrais contenus variés (articles techniques, recette, astuce, lieu à Lille, reel, capture d'écran) passe dans le prompt avant toute modification, via la commande `/eval-prompt`. On vérifie le classement et surtout qu'aucune information technique n'a été perdue.

## API REST

Le contrat est écrit en OpenAPI et sert de source de vérité : le serveur Go et le client TypeScript de l'appli en sont générés. Toutes les routes sont sous `/api` et protégées par `Authorization: Bearer <token>`, sauf `/healthz`.

| Méthode | Route | Rôle |
| --- | --- | --- |
| POST | /api/items | Capturer : JSON `{url?, note?, user_note?, is_private?}`, ou multipart avec une image |
| GET | /api/items | Lister : `q` (plein texte), `type`, `category`, `tag`, `status`, `read_status`, `pinned` ; pagination par curseur |
| GET | /api/items/{id} | Détail complet avec la fiche |
| PATCH | /api/items/{id} | Corriger type, catégorie, tags ; épingle, annotations, note |
| PUT | /api/items/{id}/progress | Enregistrer la position et le statut de lecture |
| DELETE | /api/items/{id} | Supprimer |
| POST | /api/items/{id}/analyze | Analyse immédiate, hors batch |
| POST | /api/items/{id}/retry | Remettre un élément `failed` dans la file |
| GET | /api/assets/{id} | Récupérer une image |
| GET | /api/review | Éléments analysés depuis la dernière revue, catégories et types proposés |
| POST | /api/review/done | Marquer la revue comme faite |
| GET, POST, PATCH, DELETE | /api/categories | Gérer l'arborescence ; accepter ou renommer une proposition |
| POST | /api/categories/{id}/merge | Fusionner une catégorie dans une autre, éléments compris |
| GET, PATCH | /api/content-types | Accepter ou renommer un type proposé |
| GET | /api/tags | Tags avec leur nombre d'éléments |
| GET | /api/sync | Changements depuis une date, pour le cache hors ligne de l'appli |
| GET | /api/export | Archive Markdown, un fichier par fiche, rangés par catégorie |
| GET | /api/stats | Éléments par statut, coût du mois en cours |
| GET | /healthz | État du service, sans authentification |

Tes corrections manuelles sont enregistrées dans `user_edited` et ne sont jamais écrasées par une réanalyse.

## Application mobile

Une appli de lecture avant tout : six écrans, une extension de partage, et une fiche dessinée sur mesure pour chaque type de contenu.

| Écran | Contenu |
| --- | --- |
| Bibliothèque (accueil) | Recherche, filtres (type, catégorie, À lire, épinglés, À trier), cartes avec titre, source, type, pastille de couleur de la catégorie, badge « en attente » ; bandeau « Reprendre la lecture » |
| Fiche | Template du type, barre de progression, reprise à la bonne position, épingle, annotations, type, catégorie et tags modifiables, ouvrir l'original, réanalyser |
| Capture | URL, note (dictée iOS possible), image depuis la photothèque, interrupteur « Privé » décoché |
| Revue | Ce qui a été analysé depuis la dernière revue, propositions de catégories et de types en évidence ; accepter, renommer ou fusionner |
| Explorer | Arborescence des catégories, types, nuage de tags avec compteurs |
| Réglages | Thème, taille du texte, état du serveur, file d'attente, coût du mois, échecs à relancer, export Markdown |

**Lecture**

- Statut automatique : « À lire » jamais ouverte, « En cours » entamée, « Lu » lue jusqu'au bout ou marquée d'un geste.
- La position de lecture est enregistrée ; un bandeau ramène à l'endroit exact.
- Hors ligne : les fiches sont copiées localement (expo-sqlite) et synchronisées via `/api/sync` ; une capture faite dans l'appli sans réseau attend son retour.

**Templates par type**

- Article technique : code coloré avec bouton copier, versions en badges.
- Tuto : étapes numérotées et dépliables.
- Recette : ingrédients à cocher, mode pas-à-pas avec l'écran qui reste allumé.
- Lieu : adresse, bouton « Ouvrir dans Plans », infos pratiques.
- Générique : sections et points clés, pour les types proposés par Haiku en attendant leur design.

**Direction artistique**

Style éditorial et sobre, typographie soignée, beaucoup d'air. Thèmes clair et sombre, automatique par défaut et forçable. Couleur d'accent par catégorie, animations discrètes (transition fluide de la carte vers la fiche, retour haptique sur les actions), taille du texte réglable en respectant celle d'iOS. Maquettes avant le code ; le gros travail d'UI viendra plus tard.

**Extension de partage**

- Apparaît dans la feuille de partage iOS pour les URL, le texte et les images.
- Mini formulaire : contenu détecté, note optionnelle, interrupteur « Privé », bouton « Enregistrer ».
- Envoie elle-même la capture au NAS : l'appli principale n'a pas besoin d'être ouverte.
- L'URL du serveur et le token sont injectés au build dans les deux cibles, pour ne pas dépendre des App Groups, incertains avec un compte Apple gratuit.
- Hors réseau ou Tailscale coupé : message clair et bouton pour copier le lien.

**Librairies prévues** : expo-router, TanStack Query, expo-sqlite, react-native-reanimated, expo-haptics, expo-share-extension, plus une librairie de coloration syntaxique. Repli pour l'extension si besoin : une extension native Swift minimale.

## Déploiement sur le NAS

Deux conteneurs dans un docker compose : le service Go et un sidecar Tailscale qui lui donne une adresse HTTPS privée sur ton réseau Tailscale.

- **HTTPS via `tailscale serve`** : certificat `*.ts.net` fourni par Tailscale, ce qui évite de désactiver la protection HTTP d'iOS (App Transport Security).
- **SQLite en Go pur** (driver modernc.org/sqlite, sans CGO) : FTS5 inclus, compilation croisée Mac → NAS (amd64) sans douleur.
- **Images** construites par GitHub Actions et poussées sur GitHub Container Registry (privé) ; mise à jour sur le NAS par `docker compose pull && docker compose up -d`.
- **Sauvegarde** : chaque nuit à 3 h, copie cohérente de la base (`VACUUM INTO`) vers un autre dossier du NAS, 14 jours conservés, plus une copie chiffrée de la base et des images vers un stockage externe (service à choisir). Le miroir RAID protège d'une panne de disque, pas d'un vol, d'un incendie ou d'une erreur.
- **Logs** JSON sur la sortie standard (`log/slog`), lisibles depuis l'interface Docker du NAS.
- **Garde-fou mémoire** : limite du conteneur à 128 Mo.

```yaml
services:
  tailscale:
    image: tailscale/tailscale:latest
    hostname: brain
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
      - TS_SERVE_CONFIG=/config/serve.json
      - TS_STATE_DIR=/var/lib/tailscale
    volumes:
      - ./tailscale/state:/var/lib/tailscale
      - ./tailscale/config:/config
    restart: unless-stopped

  brain:
    image: ghcr.io/<toi>/brain:latest
    network_mode: service:tailscale
    environment:
      - API_TOKEN=${API_TOKEN}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - BATCH_SCHEDULE=0 2 * * *
      - TZ=Europe/Paris
    volumes:
      - ./data:/data
    mem_limit: 128m
    restart: unless-stopped
```

## Coûts et garde-fous

À 20 éléments par jour, la V1 coûte environ 2 à 4 $ par mois selon la part de contenus techniques ; trois verrous empêchent toute mauvaise surprise.

Tarifs Haiku 4.5 : 1 $ / 5 $ par million de tokens (entrée / sortie), deux fois moins via l'API Batch ([source](https://developer.puter.com/tutorials/claude-api-pricing)).

| Cas | Hypothèse | Coût par élément (batch) |
| --- | --- | --- |
| Astuce, recette, lieu, note | 4 500 tokens entrée, 500 sortie | \~0,0035 $ |
| Article technique | 6 000 tokens entrée, 2 000 sortie | \~0,008 $ |
| Très long contenu | 30 000 tokens entrée, 4 000 sortie | \~0,025 $ |
| Capture d'écran | image + prompt, 800 sortie | \~0,004 $ |
| Analyse immédiate (hors batch) | Contenu moyen | \~0,01 $ |

**Garde-fous**

1. Crédits prépayés sur la console Anthropic, recharge automatique désactivée : crédit épuisé, les appels s'arrêtent.
2. Budget mensuel dans l'appli (`MONTHLY_BUDGET_USD`, 5 $ par défaut) calculé depuis la table `batches` : budget atteint, le batch de la nuit est sauté et Réglages affiche une alerte.
3. Plafond de 200 éléments par batch, pour qu'un import massif ne parte pas d'un coup.

Une clé d'API dédiée au projet permet de suivre sa consommation à part et de la révoquer sans toucher au reste.

## Risques et points à vérifier

Deux risques dominent : l'extension de partage côté iOS, à valider en tout premier, et la fidélité des fiches techniques, à surveiller en continu.

| Risque | Parade | Quand vérifier |
| --- | --- | --- |
| L'extension de partage refuse de se signer avec un compte Apple gratuit | Config injectée au build, sans App Groups ; repli sur une extension Swift minimale | Prototype n°1 |
| SideStore et Tailscale sont deux VPN, et iOS n'en garde qu'un actif à la fois | Couper Tailscale le temps du rafraîchissement hebdomadaire, puis le rallumer | Prototype n°1 |
| SideStore demande un premier appairage depuis un ordinateur | Fait une seule fois sur le Mac | Installation |
| Une fiche technique perd une information importante | Jeu de test `/eval-prompt` avant chaque changement de prompt ; texte original conservé, réanalyse possible | Avant la mise en service, puis à chaque changement |
| Contenus très longs coupés | Limite à environ 120 000 caractères, élément marqué « tronqué » ; découpage en sections en V1.5 | Premiers usages |
| Extraction impossible sur certains sites (paywalls, pages en JavaScript) | Repli sur titre, description et ta note ; `content_ok = false` affiché | Premiers usages |
| Trop de catégories proposées, arborescence qui part dans tous les sens (challengeable : aujourd'hui 2 niveaux maximum, mais peut-être qu'à terme, un troisième niveau peut être intéressant pour mieux classer, voire un quatrième) | Revue quotidienne, fusion en un geste, 2 niveaux maximum&#32; | Après un mois |
| Reels Instagram (V1.5) : compte secondaire banni, cookies expirés, yt-dlp cassé par une mise à jour | Compte secondaire uniquement ; repli automatique sur lien + note + capture | Au lancement de la V1.5 |
| Disques du NAS pas encore livrés | Développement et tests en local sur le Mac avec le même docker compose | Dès maintenant |

## Organisation du développement

Tout le code est écrit avec des agents ; la première étape du projet est donc de leur préparer un environnement propre, en s'inspirant de l'autre projet déjà outillé.

- **Monorepo** GitHub privé : `backend/` (Go), `app/` (Expo), `infra/` (docker compose), `docs/`.
- **Contrat d'API** en OpenAPI, source de vérité ; serveur Go et client TypeScript générés.
- **Contexte des agents** : `CLAUDE.md` racine avec table de routage, un `CLAUDE.md` par dossier, `docs/adr/` avec une décision par fichier (celles de cette spec en premier), OpenSpec pour les évolutions.
- **Agents, skills et commandes** repris de l'autre projet et adaptés : rôles précis, règles d'architecture, conventions. Commandes prévues : `/eval-prompt`, `/new-content-type`.
- **Tests** : unitaires et intégration sur une vraie base SQLite côté Go, tests front (dont les templates de fiches), jeu de test du prompt ; stratégie alignée sur celle de l'autre projet.
- **CI GitHub Actions** : lint, tests back et front, build et publication de l'image Docker.

## Suite

La V1 se construit en six étapes, le setup et le risque iOS d'abord ; les versions suivantes branchent de nouvelles sources ou de nouveaux usages sur le même pipeline.

1. Setup du projet : monorepo, fichiers de contexte pour les agents, stratégie de test et CI, en reprenant ce qui marche sur l'autre projet.
2. Prototype iOS jetable : extension de partage + SideStore + Tailscale, qui envoie une URL à un serveur bidon.
3. Backend Go : capture, extraction, SQLite, recherche, API générée depuis l'OpenAPI, testé en local sur le Mac.
4. Prompt et batch Haiku : schémas par type, jeu de test d'une vingtaine de contenus, suivi des coûts.
5. Appli Expo : maquettes, puis écrans et templates de fiches.
6. Mise en production sur le NAS dès réception des disques.

&#91;embedded content: feuille de route · 4 versions\]

Le cadre en couleur marque la version en cours ; chaque étape suivante ne change ni la base ni le pipeline, elle les étend.

## V2 : décisions déjà prises

La veille réutilise le pipeline de la V1 ; ces règles sont fixées dès maintenant pour que le modèle de données les anticipe.

- **Sujets** déclarés en langage naturel ; l'appli propose des sources de départ (RSS, blogs officiels, releases GitHub, Hacker News filtré) que tu valides ; ajout manuel possible.
- **Digest du matin** : un article à la une puis les autres par pertinence, temps de lecture affiché, notification locale à l'heure choisie. Les 30 minutes sont un repère, jamais un quota : un article n'entre que s'il dépasse un seuil de pertinence.
- **Actualité générale** : un bloc « L'essentiel du jour » en 5 à 10 points factuels, sources de lignes éditoriales variées, chaque point lié à ses sources.
- **Importance** : quatre niveaux pour les actus (Info, Notable, Important, Majeur), « Majeur » rare et sans dramatisation, combinés à la pertinence pour toi. Pas d'alerte en temps réel : un élément majeur passe en tête du digest.
- **Regroupement** : dix articles sur la même annonce donnent une seule fiche avec toutes les sources ; un même sujet ne prend pas plus de deux places, pour laisser la place aux autres.
- **Rétention** : le contenu des articles non gardés est supprimé après 30 jours ; seule une trace légère reste (lien, sujet, ouvert ou ignoré) pour apprendre tes goûts.
- **Plus tard** : suggestions de sujets une fois par semaine (3 au maximum, avec leur raison), rubrique « Ça monte », radar techno personnel.

## Points ouverts

Ces sujets sont volontairement laissés pour plus tard ; ils seront tranchés le moment venu.

- **Nom définitif** : Hippo est provisoire ; autres pistes Engram, Mnemo, Cortex, Synapse.
- **Questions posées à la base** : en local sur le NAS si des modèles plus légers et performants sortent d'ici là, sinon via Haiku.
- **Profondeur des catégories** : 2 niveaux aujourd'hui, peut-être 3 ou 4 si le classement le demande ou si on peut avoir un classement plus cohérent.
- **Médias pour l'actualité générale** : liste à définir ensemble, lignes éditoriales variées.
- **Service de sauvegarde externe** à choisir.
- **Reels Instagram** : rester attentif aux outils qui pourraient simplifier l'extraction d'ici la V1.5.
- **Compte Apple Developer payant** : seulement si d'autres projets iOS le justifient.
