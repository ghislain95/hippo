# Hippo — Spec V1

Spécification technique de la V1. Elle décrit **comment** Hippo est construit ; le **quoi** et ses règles sont dans le [document métier](metier.md), le vocabulaire dans le [glossaire](glossary.md), le **pourquoi** dans les [ADR](adr/README.md).

> Ce fichier est la version de référence de la spec, issue de la séance de conception des 10 et 11 octobre 2026. En cas de contradiction, le document métier, les ADR et les schémas de [`prompt/`](prompt/README.md) font foi, et ce fichier est corrigé.

## Contexte et objectifs

Hippo (nom de code provisoire) est le deuxième cerveau de Ghislain : il y envoie tout ce qu'il veut retenir, une IA le range pendant la nuit, et il le retrouve en quelques secondes. La V1 couvre la capture et le classement ; la veille quotidienne (V2) réutilisera le même pipeline.

Domaines de départ, sans liste figée : dev, IA appliquée au dev, infra et DevOps, actualité tech, astuces du quotidien et de la maison, cuisine, plantes, jeux vidéo, voyages et sorties. D'autres domaines apparaîtront au fil des captures. Le profil complet de Ghislain transmis à l'IA est dans le [prompt](prompt/system-prompt.md).

**Objectifs mesurables**

- Capturer un lien, une image ou une note en moins de 5 secondes, depuis la feuille de partage iOS ou l'appli.
- Chaque élément est analysé et classé automatiquement au plus tard le lendemain matin.
- Retrouver un élément par mot-clé, tag, type ou catégorie en moins d'une seconde, même hors ligne.
- Aucune perte d'information technique utile dans les fiches (vérifié par le [jeu de test](prompt/eval-set.md)).
- Coût IA inférieur à 5 $ par mois, serveur sous 100 Mo de RAM.

Un seul utilisateur en V1 ; le modèle de données prévoit plusieurs utilisateurs ([ADR-0022](adr/0022-preparation-multi-utilisateur.md)).

## Périmètre

| Fonctionnalité | Version |
| --- | --- |
| Capture de liens via la feuille de partage iOS et dans l'appli | V1 |
| Captures d'écran et photos, lues par l'IA | V1 |
| Notes texte, dictées avec le clavier iOS | V1 |
| Option « Privé » (stocké, jamais envoyé à l'IA), décochée par défaut | V1 |
| Extraction des pages web et des transcriptions YouTube | V1 |
| Analyse de nuit : type, catégorie, tags, fiche structurée | V1 |
| 7 types de contenu, chacun avec son template, plus un template générique | V1 |
| Catégories et types proposés par l'IA, écran Revue | V1 |
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

Un seul binaire Go sur le NAS porte l'API, le stockage SQLite, l'extraction et le planificateur nocturne. L'appli iPhone lui parle via Tailscale. L'IA n'est appelée que par le backend, en batch la nuit ou à la demande.

```
iPhone ──HTTPS (Tailscale)──► service Go ──► SQLite (+ images sur disque)
 ├─ appli Expo                    │
 └─ extension de partage          ├──► sites web (extraction)
                                  └──► API Anthropic (Haiku 4.5, batch)
```

| Couche | Choix | ADR |
| --- | --- | --- |
| Backend | Go, librairie standard `net/http`, `log/slog` | [0003](adr/0003-backend-go.md) |
| Base de données | SQLite (WAL) + FTS5, driver `modernc.org/sqlite`, images sur disque | [0004](adr/0004-sqlite.md) |
| Extraction | go-readability ; transcriptions YouTube | [0008](adr/0008-extraction-et-reanalyse.md) |
| IA | Claude Haiku 4.5, API Message Batches, SDK `anthropic-sdk-go`, derrière une interface `Analyzer` | [0005](adr/0005-haiku-batch.md) |
| Sortie de l'IA | Outil `classify_item` forcé, validé par JSON Schema | [0006](adr/0006-sortie-structuree.md) |
| Contrat d'API | OpenAPI 3.1, `oapi-codegen` côté Go, client TypeScript généré | [0017](adr/0017-contrat-openapi.md) |
| Mobile | Expo (React Native), development build, extension de partage en Swift | [0012](adr/0012-appli-expo.md), [0014](adr/0014-extension-de-partage-autonome.md), [0036](adr/0036-extension-de-partage-swift.md) |
| Installation iOS | Compte Apple gratuit + SideStore | [0013](adr/0013-distribution-ios-gratuite.md) |
| Réseau | Tailscale, HTTPS via `tailscale serve` | [0015](adr/0015-reseau-tailscale.md) |
| Authentification | Token d'API statique (Bearer) | [0016](adr/0016-authentification-token.md) |
| Déploiement | Image distroless, Docker Compose, GHCR | [0019](adr/0019-deploiement-docker-nas.md) |

## Modèle de données

La fiche est stockée telle que l'IA l'a renvoyée, en JSON validé : c'est la source de vérité de l'affichage. Le Markdown n'est jamais stocké, il est produit à l'export ([ADR-0007](adr/0007-fiche-json-source-de-verite.md)). Chaque table métier porte un `user_id` ; toutes les tables synchronisées vers l'appli (éléments, catégories, types de contenu, tags) portent `updated_at` et une **séquence de modification** `change_seq`, entier croissant commun à toute la base (un seul écrivain), incrémenté à chaque écriture, qui ordonne la synchronisation ; les suppressions laissent une trace `deleted_at` pendant 30 jours, sans aucun contenu ([ADR-0018](adr/0018-lecture-hors-ligne.md), [ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)).

Identifiants ([ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)) : ULID (`TEXT`) pour les éléments, les images et les batchs ; entiers pour les catégories et les tags ; les types de contenu sont désignés par leur clé dans l'API. Les dates sont en ISO 8601 UTC.

**users** — `id`, `name`, `api_token_hash`, `created_at`. Une ligne en V1, créée au premier démarrage.

**items** — un élément.

| Champ | Type | Rôle |
| --- | --- | --- |
| id | TEXT (ULID) | Identifiant triable par date |
| user_id | TEXT | Propriétaire |
| kind | TEXT | `link`, `image`, `note` |
| source | TEXT | `share`, `manual` (V2 : `rss`) ; pas de `CHECK` en base, la valeur est vérifiée par le domaine |
| url | TEXT, nullable | URL d'origine, telle que capturée |
| normalized_url | TEXT, nullable | URL normalisée (RG-05) d'un lien, unique par utilisateur ; nulle pour une image, une note et un lien capturé avant E2-02 |
| site_name | TEXT, nullable | Nom du site, issu de l'extraction |
| user_note | TEXT, nullable | Note de capture (contenu de l'élément pour `kind = note`) |
| is_private | INTEGER (0/1) | 1 = jamais envoyé à l'IA ; 0 par défaut |
| status | TEXT | `queued`, `extracted`, `submitted`, `analyzed`, `failed` |
| extraction_mode | TEXT, nullable | `full`, `fallback`, `none` |
| content_text | TEXT, nullable | Contenu extrait, conservé pour la réanalyse (~120 000 caractères max) |
| content_truncated | INTEGER (0/1) | Le contenu dépassait la limite |
| reading_minutes | INTEGER, nullable | Temps de lecture de l'original, calculé à l'extraction (RG-25) |
| type_id | INTEGER, nullable | Type de contenu ; ajouté par la story qui crée `content_types` |
| category_id | INTEGER, nullable | Catégorie ; null = À trier ; ajouté par la story qui crée `categories` |
| title | TEXT | Titre affiché (titre d'origine avant analyse) |
| fiche_json | TEXT (JSON), nullable | Fiche générée, au format du type |
| search_text | TEXT | Fiche aplatie en texte, pour la recherche |
| content_ok | INTEGER (0/1), nullable | 0 = contenu pauvre (RG-37) |
| content_issue | TEXT, nullable | Ce qui manque, selon l'IA |
| incomplete_retried | INTEGER (0/1) | La seconde tentative avec le plafond doublé a déjà eu lieu (RG-36) |
| language | TEXT, nullable | Langue de l'original |
| freshness | TEXT, nullable | `dated` ou `timeless` |
| model, prompt_version, schema_version | TEXT, INTEGER, INTEGER | Traçabilité (RG-64) |
| user_edited | TEXT (JSON) | Liste des champs corrigés à la main (RG-60) |
| annotations | TEXT, nullable | Annotations de l'utilisateur |
| is_pinned | INTEGER (0/1) | Épingle |
| read_status | TEXT | `to_read`, `in_progress`, `read` |
| read_progress | REAL | 0 à 1 |
| attempts, last_error | INTEGER, TEXT | Suivi des échecs |
| batch_id | TEXT, nullable | Batch Anthropic en cours ; ajouté par la story qui crée `batches` |
| created_at, updated_at, extracted_at, analyzed_at, read_at, deleted_at | TEXT | Horodatage |
| change_seq | INTEGER | Séquence de modification, pour la synchronisation |

La table `items` naît avec E2-01 (migration `00002_items.sql`). Elle évolue ainsi :

| Évolution | Story | Raison |
| --- | --- | --- |
| Unicité de `(user_id, normalized_url)`, index partiel hors valeurs nulles | E2-02 (migration `00003_capture_ids.sql`) | Un lien n'existe qu'une fois par utilisateur (RG-04). Les liens capturés avant E2-02 gardent `normalized_url` nul et ne sont jamais reconnus comme doublons : aucune base réelle n'existait avant E6, donc aucun remplissage (une migration n'est que du SQL, [ADR-0032](adr/0032-migrations-goose.md), et RG-05 ne s'écrit pas en SQL) |
| Suppression de `client_id` et de son index | E2-02 (migration `00003_capture_ids.sql`) | Les identifiants de capture passent dans `capture_ids`, seule référence des rejeux ; les valeurs existantes y sont copiées avant la suppression |
| `type_id`, `category_id`, `batch_id` | celle qui crée `content_types`, `categories`, `batches` | Elles référencent ces tables ; ajoutées par `ALTER TABLE items ADD COLUMN … REFERENCES …` |

**capture_ids** — l'identifiant de chaque capture reçue (RG-08), créée par E2-02 (migration `00003_capture_ids.sql`).

| Champ | Type | Rôle |
| --- | --- | --- |
| user_id | TEXT | Propriétaire |
| client_id | TEXT | Identifiant de capture choisi par l'appareil |
| item_id | TEXT (ULID) | Élément auquel la capture a abouti : créé, fusionné (doublon, RG-04) ou rejoué |
| created_at | TEXT | Réception de la capture |

Clé primaire : `(user_id, client_id)`. Une capture fusionnée dans un doublon y est aussi enregistrée, pour que son rejeu n'ajoute pas sa note une seconde fois et ne rende pas l'élément privé à nouveau (RG-08 prime sur RG-04). La table n'est pas synchronisée vers l'appli : elle n'a ni `updated_at` ni `change_seq`.

**categories** — arborescence de 2 niveaux (RG-51).

| Champ | Type | Rôle |
| --- | --- | --- |
| id, user_id | INTEGER, TEXT | Clé et propriétaire |
| name, description | TEXT | La description aide l'IA à ranger |
| parent_id | INTEGER, nullable | Catégorie parente (racine si null) |
| status | TEXT | `active` ou `proposed` |
| proposal_reason | TEXT, nullable | Justification donnée par l'IA |
| color | TEXT, nullable | Couleur d'accent ; héritée du parent si null (RG-59) |
| created_at, updated_at, deleted_at, change_seq | TEXT, INTEGER | Horodatage et séquence de modification |

Unicité : `(user_id, parent_id, nom normalisé)`.

**content_types** — partagés entre utilisateurs ([ADR-0022](adr/0022-preparation-multi-utilisateur.md)). Clés de départ : `tech_article`, `tutorial`, `tip`, `recipe`, `place`, `resource`, `note` ([métier §7](metier.md#7-types-de-contenu)).

| Champ | Type | Rôle |
| --- | --- | --- |
| id, key, name | INTEGER, TEXT, TEXT | Identité ; clé en anglais, nom affiché en français |
| status | TEXT | `active` ou `proposed` |
| has_template | INTEGER (0/1) | 0 = fiche et template `generic` |
| proposal_reason | TEXT, nullable | Justification donnée par l'IA |
| updated_at, deleted_at, change_seq | TEXT, INTEGER | Synchronisés vers l'appli ; un type fusionné laisse une trace `deleted_at` (RG-72) |

**Autres tables**

- **tags** (`id` entier, `user_id`, `name` unique par utilisateur, `updated_at`, `deleted_at`, `change_seq` ; un tag fusionné laisse une trace) et **item_tags** (`item_id`, `tag_id`).
- **assets** : images (`id`, `item_id`, `user_id`, chemin relatif sur disque, type MIME, largeur, hauteur, taille).
- **batches** : `id` Anthropic, `user_id`, statut (`in_progress`, `ended`, `failed`), nombre d'éléments, tokens d'entrée, de sortie et de cache, `cost_usd`, dates. Les analyses immédiates sont enregistrées dans une table **analysis_calls** de même forme, pour le calcul du budget.
- **settings** : réglages clé-valeur par utilisateur, dont `last_review_at`.
- **sync_sequence** : une seule ligne (`id = 1`), la valeur courante de la séquence de modification. Elle n'avance que par une seule fonction du store, dans la transaction de l'écriture ([conventions du backend](dev/conventions-backend.md#stockage-sqlite)).
- **items_fts** : table virtuelle FTS5 sur `title`, `search_text`, `user_note`, `annotations` et les tags, tokenizer `unicode61 remove_diacritics 2`, tenue à jour par des triggers.

Les migrations sont versionnées et appliquées au démarrage.

## Pipeline d'analyse

Un élément passe par cinq étapes ; seule l'étape 3 appelle l'IA, une fois par nuit, en un seul batch. Le cycle de vie complet est dans le [document métier](metier.md#4-cycle-de-vie-dun-élément).

1. **Capture** — `POST /api/items` crée l'élément en `queued` et répond tout de suite (RG-01 à RG-06).
2. **Extraction** — en arrière-plan, juste après : page web via go-readability (timeout 15 s), transcription YouTube quand elle existe, rien pour une image ou une note. Repli sur les métadonnées `og:*` en cas d'échec ; l'élément passe toujours à `extracted` (RG-20 à RG-25). Une file en mémoire traite les extractions une par une ; au démarrage, les éléments restés `queued` sont repris.
3. **Envoi du batch** — à 2 h (`BATCH_SCHEDULE`), les éléments `extracted` **non privés** partent dans un seul batch, 200 au maximum, après vérification du budget (RG-30 à RG-32, RG-81). Statut `submitted`.
4. **Suivi** — interrogation toutes les 15 minutes jusqu'à la fin du batch (24 h au maximum), y compris après un redémarrage.
5. **Résultats** — validation puis écriture, création des propositions, normalisation des tags, gestion des échecs et des réponses coupées par le plafond de sortie (RG-34 à RG-37, RG-53 à RG-58). Statut `analyzed`.

L'**analyse immédiate** (`POST /api/items/{id}/analyze`) envoie la même requête en appel direct.

Le format exact des requêtes, le prompt, les schémas des fiches et l'ordre de validation sont dans [`prompt/`](prompt/README.md). Ils ne sont volontairement pas recopiés ici.

**Budget de tokens**

- Entrée : le contenu extrait seul, jamais le HTML, jusqu'à ~120 000 caractères ; images redimensionnées à 1 568 px sur leur plus grand côté avant envoi.
- Sortie : plafond de 4 000 tokens, 8 000 pour la seconde tentative d'une réponse coupée (RG-36) ; la longueur réelle suit la richesse du contenu.
- Cache de prompt sur la partie fixe, le référentiel et l'outil.

## API REST

Le contrat fait foi : `api/openapi.yaml` ([ADR-0017](adr/0017-contrat-openapi.md), [ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)) ; il porte aussi le catalogue des types d'erreur. Toutes les routes sont sous `/api` et exigent `Authorization: Bearer <token>`, sauf `/healthz`. JSON en `snake_case`, erreurs au format problem+json, pagination par curseur. Une route déclarée mais pas encore implémentée répond `501`. Le corps JSON d'une opération implémentée est strict : un membre non déclaré ou des données après la valeur répondent `400` ; un corps JSON de plus de 1 Mio répond `413` ([conventions du backend](dev/conventions-backend.md#contrat-dapi)). Le tableau ci-dessous résume le contrat ; en cas d'écart, le contrat gagne.

| Méthode | Route | Rôle |
| --- | --- | --- |
| POST | /api/items | Capturer : JSON `{url ou note, user_note?, is_private?, client_id?, source?}`, ou multipart avec une image ; `source` facultatif dans les deux formats, `share` par défaut, ou `manual` ; `201` nouvel élément, `200` doublon ou capture rejouée avec l'élément existant (RG-04, RG-07, RG-08) ; un doublon ajoute sa note datée (RG-04) et ne fait que resserrer la confidentialité : il peut rendre l'élément privé, jamais public (RG-15) |
| GET | /api/items | Lister : `q`, `type` (clé), `category` (id), `tag` (id, répétable), `status`, `read_status`, `pinned`, `unsorted` ; curseur |
| GET | /api/items/{item_id} | Détail complet avec la fiche et les images |
| PATCH | /api/items/{item_id} | Corriger type, catégorie, tags ; épingle, annotations, note, privé (`null` efface) |
| PUT | /api/items/{item_id}/progress | Position et statut de lecture |
| DELETE | /api/items/{item_id} | Supprimer |
| POST | /api/items/{item_id}/analyze | Analyse immédiate |
| POST | /api/items/{item_id}/retry | Remettre un élément `failed` en file |
| GET | /api/assets/{asset_id} | Récupérer une image |
| GET | /api/review | Éléments analysés depuis la dernière revue, propositions |
| POST | /api/review/done | Marquer la revue comme faite |
| GET, POST | /api/categories | Arborescence ; créer une catégorie |
| PATCH, DELETE | /api/categories/{category_id} | Renommer, déplacer, recolorer, accepter une proposition ; supprimer une catégorie vide |
| POST | /api/categories/{category_id}/merge | Fusionner une catégorie dans une autre |
| GET | /api/content-types | Types de contenu |
| PATCH | /api/content-types/{type_key} | Renommer ou accepter un type proposé |
| POST | /api/content-types/{type_key}/merge | Fusionner un type proposé dans un type existant |
| GET | /api/tags | Tags avec compteurs |
| PATCH | /api/tags/{tag_id} | Renommer un tag (nom déjà pris refusé, RG-74) |
| POST | /api/tags/{tag_id}/merge | Fusionner deux tags |
| GET | /api/sync | Changements depuis un `sync_token`, pour le cache hors ligne ; `410` si le jeton a plus de 30 jours ([ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)) |
| GET | /api/export | Archive Markdown |
| GET | /api/stats | Éléments par statut, file, échecs, coût du mois |
| GET | /healthz | État du service, sans authentification |

## Application mobile

Une appli de lecture avant tout : six écrans, une extension de partage, et un template par type de contenu.

| Écran | Contenu |
| --- | --- |
| Bibliothèque (accueil) | Recherche, filtres (type, catégorie, À lire, épinglés, À trier), cartes avec titre, site, type, pastille de couleur, badge « en attente » ; bandeau « Reprendre la lecture » |
| Fiche | Template du type, barre de progression, reprise de position, mentions (partiel, tronqué), épingle, annotations, type, catégorie et tags modifiables, ouvrir l'original, réanalyser, supprimer |
| Capture | URL, note (dictée iOS possible), image depuis la photothèque, interrupteur « Privé » décoché |
| Revue | Éléments analysés depuis la dernière revue, propositions en évidence ; accepter, renommer, fusionner |
| Explorer | Arborescence des catégories, types, nuage de tags, avec compteurs |
| Réglages | Thème, taille du texte, état du serveur, file d'attente, coût du mois, échecs à relancer, export |

**Templates** : code coloré et copiable (`tech_article`, `tutorial`), versions en badges ; étapes dépliables (`tutorial`) ; ingrédients à cocher et mode pas à pas écran allumé (`recipe`) ; bouton « Ouvrir dans Plans » (`place`) ; template `generic` pour les types proposés.

**Hors ligne** : copie locale dans `expo-sqlite`, synchronisée par `/api/sync`, écritures mises en file et rejouées ([ADR-0018](adr/0018-lecture-hors-ligne.md)).

**Direction artistique** : style éditorial et sobre, typographie soignée, beaucoup d'air ; thèmes clair et sombre (automatique, forçable) ; couleur d'accent par catégorie ; animations discrètes (transition carte → fiche, retour haptique) ; taille du texte réglable en respectant iOS. Maquettes avant le code.

**Extension de partage** ([ADR-0014](adr/0014-extension-de-partage-autonome.md)) : URL, texte et images ; mini formulaire (aperçu, note, Privé, Enregistrer) ; envoie elle-même au NAS ; URL et token injectés au build ; message clair et « Copier le lien » si le serveur est injoignable.

**Librairies prévues** : expo-router, TanStack Query, expo-sqlite, react-native-reanimated, expo-haptics, une librairie de coloration syntaxique. Extension de partage en Swift, générée par `@bacons/apple-targets` ([ADR-0036](adr/0036-extension-de-partage-swift.md)).

## Déploiement sur le NAS

Deux conteneurs : le service Go et un sidecar Tailscale qui le publie en HTTPS sur le réseau privé ([ADR-0015](adr/0015-reseau-tailscale.md), [ADR-0019](adr/0019-deploiement-docker-nas.md)).

- Images construites par GitHub Actions, publiées sur GHCR (privé) ; mise à jour par `docker compose pull && docker compose up -d`.
- Sauvegarde nocturne à 3 h par `VACUUM INTO` et copie des images, 14 jours conservés, plus une copie chiffrée externe ([ADR-0020](adr/0020-sauvegardes.md)).
- Logs JSON sur la sortie standard, sans secret ni contenu d'élément.
- Limite mémoire à 128 Mo.

```yaml
# infra/docker-compose.yml (ébauche)
services:
  tailscale:
    image: tailscale/tailscale:latest
    hostname: hippo
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
      - TS_SERVE_CONFIG=/config/serve.json
      - TS_STATE_DIR=/var/lib/tailscale
    volumes:
      - ./tailscale/state:/var/lib/tailscale
      - ./tailscale/config:/config
    restart: unless-stopped

  hippo:
    image: ghcr.io/<owner>/hippo:latest
    network_mode: service:tailscale
    environment:
      - API_TOKEN=${API_TOKEN}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - ANALYSIS_MODEL=claude-haiku-4-5
      - BATCH_SCHEDULE=0 2 * * *
      - BACKUP_SCHEDULE=0 3 * * *
      - MONTHLY_BUDGET_USD=5
      - TZ=Europe/Paris
    volumes:
      - ./data:/data
    mem_limit: 128m
    restart: unless-stopped
```

## Coûts et garde-fous

À 20 éléments par jour, la V1 coûte environ 2 à 4 $ par mois selon la part de contenus techniques ([ADR-0021](adr/0021-garde-fous-couts.md)).

Tarifs de Haiku 4.5 relevés en octobre 2026 : 1 $ / 5 $ par million de tokens (entrée / sortie), moitié prix via l'API Batch. À garder en configuration et à vérifier sur la page de tarifs d'Anthropic.

| Cas | Hypothèse | Coût par élément (batch) |
| --- | --- | --- |
| Astuce, recette, lieu, note | 4 500 tokens entrée, 500 sortie | ~0,0035 $ |
| Article technique | 6 000 tokens entrée, 2 000 sortie | ~0,008 $ |
| Très long contenu | 30 000 tokens entrée, 4 000 sortie | ~0,025 $ |
| Capture d'écran | image + prompt, 800 sortie | ~0,004 $ |
| Analyse immédiate (hors batch) | contenu moyen | ~0,01 $ |

**Garde-fous** : crédits prépayés sans recharge automatique ; budget mensuel applicatif (`MONTHLY_BUDGET_USD`, 5 $) vérifié avant chaque envoi ; plafonds de 200 éléments par batch, ~120 000 caractères en entrée, 4 000 tokens en sortie. Clé d'API dédiée au projet.

## Risques et points à vérifier

| Risque | Parade | Quand vérifier |
| --- | --- | --- |
| L'extension de partage refuse de se signer avec un compte Apple gratuit | Config injectée au build, sans App Groups ; extension Swift ([ADR-0036](adr/0036-extension-de-partage-swift.md)) | ✅ Écarté par le prototype E1 (2026-10-27) |
| SideStore et Tailscale sont deux VPN, iOS n'en garde qu'un actif | Couper Tailscale le temps du rafraîchissement hebdomadaire ([runbook](runbook-ios.md)) | ✅ Écarté par le prototype E1 (2026-10-27) |
| Apple casse la connexion de SideStore (erreur `-80045` en octobre 2026) | Contournement par le serveur Anisette ([runbook](runbook-ios.md)) ; à défaut, réinstallation depuis Xcode chaque semaine (ADR-0013) | À chaque rafraîchissement |
| SideStore demande un premier appairage depuis un ordinateur | Fait une seule fois sur le Mac | Installation |
| Une fiche technique perd une information importante | Jeu de test avant chaque changement de prompt ; contenu original conservé, réanalyse possible | Avant la mise en service, puis à chaque changement |
| Contenus très longs coupés | Élément marqué « tronqué » ; découpage en sections en V1.5 | Premiers usages |
| Extraction impossible (paywalls, pages en JavaScript) | Repli sur les métadonnées et la note ; contenu partiel affiché | Premiers usages |
| L'arborescence part dans tous les sens | Dédoublonnage des propositions, revue, fusion en un geste, 2 niveaux (révisable) | Après un mois |
| Une page contient des instructions destinées à l'IA | Le prompt traite tout le contenu comme des données ; cas dédié dans le jeu de test | Jeu de test |
| Reels Instagram (V1.5) : compte banni, cookies expirés, yt-dlp cassé | Compte secondaire uniquement ; repli automatique | Lancement de la V1.5 |
| Disques du NAS pas encore livrés | Développement et tests sur le Mac avec le même compose | Dès maintenant |

## Organisation du développement

Tout le code est écrit avec des agents ; la première étape est de leur préparer un environnement propre, en reprenant ce qui marche sur l'autre projet de Ghislain.

- **Monorepo** : `backend/`, `app/`, `api/`, `infra/`, `docs/` ([ADR-0002](adr/0002-monorepo.md)).
- **Contexte des agents** : `CLAUDE.md` racine avec table de routage vers [`docs/`](.), un `CLAUDE.md` par dossier, ADR, OpenSpec pour les évolutions.
- **Agents, skills et commandes** repris de l'autre projet et adaptés. Commandes prévues : `/eval-prompt`, `/new-content-type`.
- **Tests** : unitaires et intégration sur une vraie base SQLite côté Go ; tests front (dont les templates de fiches) ; jeu de test du prompt ; stratégie alignée sur celle de l'autre projet.
- **CI GitHub Actions** : lint, tests back et front, vérification du code généré, build et publication de l'image Docker.

Le détail des étapes est dans la [backlog](backlog.md).

## V2 : décisions déjà prises

- **Sujets** déclarés en langage naturel ; l'appli propose des sources de départ (RSS, blogs officiels, releases GitHub, Hacker News filtré) à valider ; ajout manuel possible.
- **Digest du matin** : un élément à la une puis les autres par pertinence, temps de lecture affiché, notification locale. Les 30 minutes sont un repère, jamais un quota : un article n'entre que s'il dépasse un seuil de pertinence.
- **L'essentiel du jour** : 5 à 10 points factuels, sources de lignes éditoriales variées, chaque point lié à ses sources.
- **Importance** : Info, Notable, Important, Majeur, pour les actualités uniquement ; « Majeur » rare et sans dramatisation ; combinée à la pertinence. Pas d'alerte en temps réel.
- **Regroupement** : dix articles sur la même annonce donnent une seule fiche avec toutes les sources ; un même sujet ne prend pas plus de deux places.
- **Rétention** : contenu des articles non gardés supprimé après 30 jours ; trace légère conservée (lien, sujet, ouvert ou ignoré).
- **Plus tard** : suggestions de sujets une fois par semaine (3 au maximum, avec leur raison), rubrique « Ça monte », radar techno personnel.

## Points ouverts

- **Nom définitif** : Hippo est provisoire ; autres pistes Engram, Mnemo, Cortex, Synapse.
- **Questions posées à la base** : en local sur le NAS si des modèles plus légers et performants sortent, sinon via Haiku.
- **Profondeur des catégories** : 2 niveaux aujourd'hui, peut-être 3 ou 4 si le classement le demande ou devient plus cohérent ainsi.
- **Médias pour l'actualité générale** : liste à définir, lignes éditoriales variées.
- **Service de sauvegarde externe** à choisir.
- **Reels Instagram** : surveiller les outils qui simplifieraient l'extraction d'ici la V1.5.
- **Compte Apple Developer payant** : seulement si d'autres projets iOS le justifient.
