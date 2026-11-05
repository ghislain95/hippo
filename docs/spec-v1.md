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
| Extraction des pages web, des PDF et des métadonnées des vidéos YouTube | V1 |
| Analyse de nuit : type, catégorie, tags, fiche structurée | V1 |
| 7 types de contenu, chacun avec son template, plus un template générique | V1 |
| Catégories et types proposés par l'IA, écran Revue | V1 |
| Statut et position de lecture, épingle, annotations | V1 |
| Recherche plein texte et filtres | V1 |
| Lecture hors ligne, thèmes clair et sombre | V1 |
| Export Markdown compatible Obsidian | V1 |
| Extraction des reels Instagram (compte secondaire) | V1.5 |
| Transcription des vidéos YouTube ([ADR-0037](adr/0037-youtube-sans-transcription.md)) | V1.5 |
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
| Extraction | go-readability (fork Readeck, voir [Extraction d'un élément](#extraction-dun-élément)) et `golang.org/x/net/html` ; PDF par `github.com/ledongthuc/pdf` et une couche de mise en page maison ; métadonnées des vidéos YouTube lues dans leur page, sinon par oEmbed, sans transcription en V1 ([ADR-0037](adr/0037-youtube-sans-transcription.md)) | [0008](adr/0008-extraction-et-reanalyse.md) |
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
| extraction_mode | TEXT, nullable | `full`, `fallback`, `none` ; `none` pour une note ou une image seulement ([extraction](#extraction-dun-élément)) |
| content_text | TEXT, nullable | Contenu extrait, conservé pour la réanalyse (~120 000 caractères max) |
| content_truncated | INTEGER (0/1) | Le contenu dépassait la limite |
| reading_minutes | INTEGER, nullable | Temps de lecture de l'original, calculé à l'extraction (RG-25) |
| type_id | INTEGER, nullable | Type de contenu ; ajouté par la story qui crée `content_types` |
| category_id | INTEGER, nullable | Catégorie ; null = À trier ; ajouté par la story qui crée `categories` |
| title | TEXT | Titre affiché : avant analyse, titre provisoire (nom d'hôte d'un lien, première ligne d'une note, note de capture ou date d'une image, [RG-26](metier.md#extraction)), puis titre d'origine ou écrit par l'IA |
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

**assets** — les images rattachées à un élément, créée par E2-04 (migration `00004_assets.sql`). Le fichier est sur le disque, la ligne le référence ([ADR-0004](adr/0004-sqlite.md)).

| Champ | Type | Rôle |
| --- | --- | --- |
| id | TEXT (ULID) | Identifiant de l'image |
| item_id | TEXT (ULID) | Élément auquel l'image est rattachée |
| user_id | TEXT | Propriétaire, le même que celui de l'élément |
| path | TEXT | Chemin relatif à `DATA_DIR/assets`, de la forme `<user_id>/<asset_id>.<ext>`, construit uniquement à partir d'identifiants du serveur |
| mime_type | TEXT | `image/jpeg`, `image/png`, `image/heic` ou `image/webp` (`CHECK`), reconnu au contenu du fichier |
| width, height | INTEGER, nullable | Dimensions en pixels, remplies pour JPEG et PNG, nulles pour HEIC et WebP |
| size_bytes | INTEGER | Taille du fichier |
| created_at | TEXT | Réception de l'image |

- Clé étrangère `(item_id, user_id)` vers `items (id, user_id)` (index unique `items_id_user`) : une image n'appartient qu'à un élément du même utilisateur (RG-90). Index `assets_item` sur `item_id`.
- `assets` n'est pas réservée à `kind = image` : RG-23 prévoit une image d'aperçu pour un lien.
- La table n'est pas synchronisée seule : les images voyagent dans `Item` (`SyncPage.items`). Elles sont insérées dans la transaction qui avance le `change_seq` de l'élément ; toute modification ultérieure des images d'un élément doit l'avancer aussi.
- Le fichier est écrit et renommé **avant** la transaction, et supprimé si elle n'aboutit pas ([conventions du backend](dev/conventions-backend.md#fichiers-des-images)) : aucune ligne ne pointe vers un fichier absent. Un fichier orphelin reste possible après un crash entre l'écriture et la validation ; il est sans effet.

**extraction_attempts** — la tentative d'extraction en cours d'un lien, créée par E2-05 (migration `00005_extraction_attempts.sql`). Rôle : [marqueur de tentative](#extraction-dun-élément).

| Champ | Type | Rôle |
| --- | --- | --- |
| item_id | TEXT (ULID) | Clé primaire : le lien en cours d'extraction |
| user_id | TEXT | Propriétaire, le même que celui de l'élément |
| started_at | TEXT, nullable | Début du téléchargement en cours (ISO 8601 UTC) ; null quand aucun téléchargement n'est en cours (après un arrêt ou un report réseau) |
| network_retries | INTEGER | Nombre de reports comptés pour panne réseau (un report compte seulement si un autre lien a joint son site entre-temps) ; au 3e report compté, le lien passe en repli |
| outcome | TEXT, nullable | Plus écrit ni lu depuis E2-08 (2 novembre 2026). E2-05 y écrivait `deferred_pdf` (`CHECK`) pour un lien répondu en PDF ; ces lignes ont `started_at` à null et sont reprises comme tout lien en file. Colonne à supprimer avec la prochaine migration de la table |

- Clé étrangère `(item_id, user_id)` vers `items (id, user_id)` : une tentative n'appartient qu'à un élément du même utilisateur (RG-90). Table `STRICT`.
- `started_at` est renseigné et validé avant le téléchargement. Au démarrage, une ligne dont `started_at` est renseigné signale un téléchargement interrompu par un crash : le lien passe en repli sans être retéléchargé. Un arrêt ou un report réseau remet `started_at` à null et garde la ligne, avec son compteur. La ligne est supprimée dans la transaction qui écrit l'élément en `extracted`.
- La table n'est pas synchronisée vers l'appli : ni `updated_at` ni `change_seq`.

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
- **batches** : `id` Anthropic, `user_id`, statut (`in_progress`, `ended`, `failed`), nombre d'éléments, tokens d'entrée, de sortie et de cache, `cost_usd`, dates. Les analyses immédiates sont enregistrées dans une table **analysis_calls** de même forme, pour le calcul du budget.
- **settings** : réglages clé-valeur par utilisateur, dont `last_review_at`.
- **sync_sequence** : une seule ligne (`id = 1`), la valeur courante de la séquence de modification. Elle n'avance que par une seule fonction du store, dans la transaction de l'écriture ([conventions du backend](dev/conventions-backend.md#stockage-sqlite)).
- **items_fts** : table virtuelle FTS5 sur `title`, `search_text`, `user_note`, `annotations` et les tags, tokenizer `unicode61 remove_diacritics 2`, tenue à jour par des triggers.

Les migrations sont versionnées et appliquées au démarrage.

## Pipeline d'analyse

Un élément passe par cinq étapes ; seule l'étape 3 appelle l'IA, une fois par nuit, en un seul batch. Le cycle de vie complet est dans le [document métier](metier.md#4-cycle-de-vie-dun-élément).

1. **Capture** — `POST /api/items` crée l'élément en `queued` et répond tout de suite (RG-01 à RG-06).
2. **Extraction** — en arrière-plan, juste après : page web via go-readability, texte d'un PDF, titre, chaîne, durée et description d'une vidéo YouTube (sans transcription en V1), rien pour une image ou une note. Repli sur les métadonnées `og:*` (ou le titre du PDF) en cas d'échec ; l'élément passe toujours à `extracted` (RG-20 à RG-28). Détail ci-dessous, [Extraction d'un élément](#extraction-dun-élément).
3. **Envoi du batch** — à 2 h (`BATCH_SCHEDULE`), les éléments `extracted` **non privés** partent dans un seul batch, 200 au maximum, après vérification du budget (RG-30 à RG-32, RG-81). Statut `submitted`.
4. **Suivi** — interrogation toutes les 15 minutes jusqu'à la fin du batch (24 h au maximum), y compris après un redémarrage.
5. **Résultats** — validation puis écriture, création des propositions, normalisation des tags, gestion des échecs et des réponses coupées par le plafond de sortie (RG-34 à RG-37, RG-53 à RG-58). Statut `analyzed`.

L'**analyse immédiate** (`POST /api/items/{id}/analyze`) envoie la même requête en appel direct.

Le format exact des requêtes, le prompt, les schémas des fiches et l'ordre de validation sont dans [`prompt/`](prompt/README.md). Ils ne sont volontairement pas recopiés ici.

**Budget de tokens**

- Entrée : le contenu extrait seul, jamais le HTML, jusqu'à ~120 000 caractères ; images redimensionnées à 1 568 px sur leur plus grand côté avant envoi.
- Sortie : plafond de 4 000 tokens, 8 000 pour la seconde tentative d'une réponse coupée (RG-36) ; la longueur réelle suit la richesse du contenu.
- Cache de prompt sur la partie fixe, le référentiel et l'outil.

### Extraction d'un élément

Mise en œuvre de RG-20 à RG-28 : les pages web, livrées par E2-05 (change `extract-web-page`, 1er novembre 2026), les PDF, livrés par E2-08 (change `extract-pdf`, 2 novembre 2026), et les vidéos YouTube, livrées par E2-06 sans transcription (change `extract-youtube`, 5 novembre 2026, RG-22 modifiée, [ADR-0037](adr/0037-youtube-sans-transcription.md)).

**Déroulement**

- Une tâche de fond du binaire (package `internal/extraction`), sans appel à l'IA ni vérification de budget.
- Une extraction à la fois, utilisateur par utilisateur, la plus ancienne d'abord ; chaque requête filtre par `user_id` (RG-90).
- Réveil après chaque capture qui crée un élément, au démarrage, et toutes les 5 minutes en filet de sécurité.
- Une note ou une image passe à `extracted` tout de suite, mode `none`, sans réseau.
- Un lien est téléchargé une fois, lu par Readability (page HTML) ou par le lecteur de PDF (réponse PDF, voir « PDF » ci-dessous), puis écrit en mode `full` ou `fallback`. Un lien ne finit jamais en `none`, réservé aux notes et aux images.
- L'élément est écrit une fois, en une transaction : colonnes d'extraction seulement (jamais `user_note` ni `is_private`), `extracted_at`, `updated_at` et `change_seq` ([ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)). Un élément supprimé ou modifié entre-temps n'est pas écrit.

**Modes d'extraction**

| Mode | Quand | Ce qui est écrit |
| --- | --- | --- |
| `full` | Réponse 2xx en HTML, texte principal d'au moins 200 caractères | Titre : Readability, sinon `og:title`, sinon `<title>`, sinon le titre provisoire (RG-26). Site : `og:site_name`, sinon Readability, sinon l'hôte. Contenu coupé à ~120 000 caractères (RG-24). Temps de lecture à 230 mots par minute sur le texte avant coupe, 1 minute au moins (RG-25) |
| `fallback` | Délai dépassé (téléchargement ou lecture), statut d'erreur, type autre que HTML ou PDF, page trop grande ou trop lourde à lire, texte trop court, adresse refusée, échec du site (connexion refusée, TLS, boucle de redirections, domaine inexistant), lecture en échec | Sur une réponse 2xx seulement : `og:title` ou `<title>`, et `og:description` comme contenu, coupée à ~120 000 caractères et marquée tronquée au-delà, comme tout contenu (RG-24). Sinon le titre provisoire et l'hôte. Pas de temps de lecture (RG-23) |
| `full` (PDF) | Réponse 2xx en PDF d'au plus 50 Mio, texte d'au moins 200 caractères | Titre dans l'ordre de RG-28 (ci-dessous). Site : l'hôte. Contenu coupé à ~120 000 caractères (RG-24). Temps de lecture à 230 mots par minute sur **tout le document**, 1 minute au moins (RG-25, RG-28) ; une lecture arrêtée par son délai est marquée tronquée et estime les pages non lues au prorata des pages lues |
| `fallback` (PDF) | PDF scanné (moins de 200 caractères de texte), protégé par un mot de passe, chiffré en AES-256, abîmé (tronqué, table de références cassée, pas un PDF malgré son type, arbre de pages hors bornes), plus de 50 Mio, délai de 15 s dépassé, lecture trop lourde ou trop longue | Titre dans l'ordre de RG-28 avec ce qui a pu être obtenu (le titre des métadonnées seulement si le document s'est ouvert). Site : l'hôte. Ni contenu ni temps de lecture (RG-23) |
| `fallback` (vidéo YouTube) | Toujours, faute de transcription (RG-22) | Données du lecteur de la page : titre, chaîne comme site, description comme contenu (coupée à ~120 000 caractères, RG-24), durée comme temps de lecture. Sinon oEmbed : titre et chaîne, ni contenu ni temps de lecture. Sinon le titre provisoire et l'hôte. Voir « Vidéos YouTube » ci-dessous |
| `none` | Note, image | Rien |

L'image d'aperçu de RG-23 n'est pas récupérée : aucune image n'est créée pour un lien.

**Limites**

- 15 s pour tout le téléchargement : DNS, TLS, redirections, en-têtes et corps.
- 10 redirections au plus, `http` et `https` seulement.
- 2 Mo de corps décompressé au plus pour une page HTML (50 Mio pour un PDF, voir « PDF » ci-dessous) ; au-delà, repli. La limite prévue était 5 Mo. La mesure du 1er novembre 2026 l'a abaissée : un article de 5 Mo montait à 59 Mo de tas, 23 Mo à 2 Mo, dans un conteneur de 128 Mo ([ADR-0019](adr/0019-deploiement-docker-nas.md)).
- La page est lue dans un **processus enfant** du même binaire (`hippo extract-page`), borné à 48 Mio de mémoire et à 10 s : au-delà de la mémoire, il s'arrête et le lien passe en repli (`too_large`) ; au-delà du temps, il est tué (repli, `timeout`). La mémoire d'un arbre HTML suit le balisage et non la taille : des pages de 200 Ko construites pour cela montaient à 85-160 Mo, dans un conteneur de 128 Mo. Décidé avec Ghislain le 2 novembre 2026, à la place d'une estimation du nombre d'éléments, contournée en relecture.
- L'enfant ne reçoit que `GOMAXPROCS=2` comme environnement, jamais celui du service (token, clé d'API) ; il ne lit pas la configuration, n'ouvre pas la base et n'écrit pas de log.
- 20 000 éléments au plus pour Readability (`MaxElemsToParse`), qui borne son temps ; au-delà, repli.
- Titre et nom de site gardés de la page (Readability, `og:title`, `<title>`, `og:site_name`) : 1 000 caractères au plus (`MaxTitleLength`), coupés entre deux mots, sans points de suspension ni marque. Borne technique contre une page hostile ; aucun vrai titre n'en approche.
- Un processus enfant qui ne peut pas démarrer (binaire absent, `fork` refusé) laisse l'élément `queued` : la passe s'interrompt 5 minutes, comme pour une panne réseau.
- Mesures du 2 novembre 2026 : un article de 2 Mo est lu avec 34,5 Mo, 13,5 Mo sous la limite ; dans un conteneur de 128 Mo, le service au repos et un enfant à sa limite culminent à 98,6 Mo. Un lancement coûte environ 8 ms.

**PDF**

Un lien répondu en PDF (`application/pdf`, ou un corps qui commence par `%PDF-` quel que soit son type déclaré) est lu par l'enfant comme une page, avec ses limites de mémoire et de temps (E2-08, RG-28).

- **Taille** : 50 Mio au plus (52 428 800 octets ; les « 50 Mo » de la décision, comme les 2 Mo des pages sont 2 Mio), sur le flux décompressé. Un PDF annoncé plus gros par son `Content-Length` n'est pas lu du tout ; un corps plus long est arrêté à la limite. Repli `too_large`. Les pages HTML gardent 2 Mio (décidé avec Ghislain le 2 novembre 2026).
- **Délai** : les 15 s de téléchargement de RG-23 couvrent aussi le PDF ; 50 Mio en 15 s demandent environ 28 Mbit/s du site au NAS, un PDF plus lent passe en repli (`timeout`) avec le nom de son fichier pour titre. La lecture a ses propres 10 s ; l'enfant répond à 8 s (80 %) avec ce qu'il a lu, avant que le parent ne le tue.
- **Dossier temporaire** : le PDF est copié, sans passer par la mémoire du service, dans un fichier de `DATA_DIR/tmp` **dont le nom est supprimé avant le téléchargement**. Le service écrit par un descripteur, l'enfant lit par un second, en lecture seule, reçu comme descripteur 3. Les données disparaissent avec le dernier descripteur, crash compris ; un fichier laissé par un crash entre la création et la suppression du nom est supprimé au démarrage. Rien d'un PDF ne reste sur le NAS ni dans les sauvegardes. Règles : [conventions du backend](dev/conventions-backend.md#dossier-temporaire-de-lextraction).
- **Lecture** : `github.com/ledongthuc/pdf` lit la structure, les filtres, le chiffrement et les polices ; une couche de mise en page maison place les glyphes : largeurs des glyphes, espaces déduits des positions, retours à la ligne aux changements de ligne de base, `\n\n` entre les pages, ligatures (U+FB00 à U+FB06) développées, traits d'union conditionnels (U+00AD) retirés, mot coupé par un trait d'union en fin de ligne recollé avec son trait d'union (aucune lettre perdue), caractères de contrôle et glyphes indéchiffrables retirés. Le texte est gardé jusqu'à 120 000 caractères, même au milieu d'une page ; au-delà, la lecture compte encore les mots sans garder le texte.
- **Bornes** : arbre de pages parcouru par notre code, 32 niveaux et 40 000 nœuds au plus, chaque nœud une seule fois (au-delà, ou un nœud rencontré deux fois : PDF abîmé), 20 000 pages lues au plus (les suivantes ne sont ni lues ni comptées dans le temps de lecture) ; objets de formulaire suivis sur 4 niveaux ; une page illisible est sautée ; l'ouverture, le titre et chaque page sont lus sous `recover`. Un en-tête de version 2.0 est présenté à la librairie comme 1.7 (non vérifié sur de vrais PDF 2.0 ; l'AES-256 de 2.0 passe en repli).
- **Titre (RG-28)** : le titre des métadonnées (`/Info /Title`), sans caractères de contrôle (le bourrage AES compris) ni caractères invisibles de mise en forme (espace sans chasse, marque d'ordre des octets), espaces resserrés ; sauf un titre parasite : vide, commençant par `Microsoft Word - `, `Microsoft PowerPoint - ` ou `Microsoft Excel - `, égal à `Untitled`, `Sans titre`, `Document1` ou `PowerPoint Presentation` sans tenir compte de la casse, ou un nom de fichier (dernier segment après `/` ou `\` finissant, juste après un caractère qui n'est pas une espace, par une extension de document : `doc`, `docx`, `odt`, `rtf`, `txt`, `pdf`, `ps`, `tex`, `dvi`, `htm`, `html`, `xhtml`, `md`, `ppt`, `pptx`, `odp`, `key`, `xls`, `xlsx`, `ods`, `numbers`, `pages`, `indd`, `tif`, `tiff`, `jpg`, `jpeg`, `png`). À défaut, le dernier segment non vide du chemin de l'URL servie (après redirections) s'il finit par `.pdf` sans tenir compte de la casse, décodé (`%20`…) ou gardé tel quel s'il ne se décode pas en UTF-8 (`%E9t%E9`), sans `.pdf`. À défaut, le titre provisoire. Puis la borne de 1 000 caractères. Le nom du site reste l'hôte de l'URL capturée.
- **Liens laissés en file par E2-05** : les lignes `deferred_pdf` sont reprises comme tout lien en file, téléchargées une fois de plus, puis effacées avec leur résultat.
- Mesures de l'enfant du 2 novembre 2026 (darwin/arm64, mémoire tenue par le runtime) : article de 10 pages 6,4 Mio et 13 ms ; 400 pages 10,1 Mio et 57 ms ; PDF de 45 Mo 10,2 Mio et 25 ms ; page qui se décompresse en plus de 100 Mo de mots 10,6 Mio et 5,1 s (lue entière) ; chaîne de 100 Mo : arrêt à la limite de mémoire en 61 ms ; arbre de pages qui se contient : repli `damaged` en 8 ms ; boucle de sections de références : repli `timeout` au délai de 8 s.
- Mesure en conteneur du 2 novembre 2026 (image `linux/arm64`, `--memory 128m`, serveur en marche, PDF servi sur un réseau Docker de documentation `203.0.113.0/24` et capturé par l'API) : au repos, pic de 61,6 Mo ; un PDF de 44,7 Mo est lu en entier en 0,5 s avec un pic de 114,2 Mo ; un PDF de 51,7 Mo (juste sous 50 Mio), pic de 129,2 Mo (123 Mio sur 128), dont 52 Mo de cache de pages du fichier, récupérable, et 68,5 Mo de mémoire anonyme. Aucun processus tué (`oom_kill` 0), le contrôle de santé a toujours répondu, le dossier temporaire est vide après chaque PDF. Détail : `design.md` du change `extract-pdf`.

**Vidéos YouTube**

Une vidéo YouTube est lue pour ses métadonnées, sans transcription en V1 (E2-06, RG-22, [ADR-0037](adr/0037-youtube-sans-transcription.md)).

- **Reconnaissance** : hôte `youtube.com`, un de ses sous-domaines ou `youtu.be`, et un identifiant de vidéo de 11 caractères (`A-Z`, `a-z`, `0-9`, `-`, `_`) sous l'une des formes `watch?v=<id>` (le premier `v`), `youtu.be/<id>`, `/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>`. Un lien YouTube sans vidéo (chaîne, playlist, accueil) est lu comme une page web. `youtube-nocookie.com` et `www.youtu.be` ne sont pas des hôtes YouTube.
- **Page canonique** : seule `https://www.youtube.com/watch?v=<id>` est demandée, jamais l'URL capturée : ses paramètres de suivi (`si`, `utm_*`, `t`) ne quittent pas le NAS.
- **Lecture** : l'enfant (mêmes bornes de mémoire, de temps et de réponse qu'une page) cherche le bloc `ytInitialPlayerResponse = {…}` (3 occurrences au plus) et garde, de ses `videoDetails` dont le `videoId` est celui demandé, le titre, la chaîne (`author`), la durée (`lengthSeconds`) et la description (`shortDescription`). Description : sauts de ligne gardés, retours chariot et caractères de contrôle retirés. Temps de lecture : la durée en minutes arrondie au-dessus, 1 au moins ; null pour une durée nulle (direct), absente, invalide ou de plus de 1 000 000 s.
- **Repli oEmbed** : une page sans données utiles (statut d'erreur, délai, plus de 2 Mio, page de consentement, redirection hors de YouTube, bloc absent, d'une autre vidéo ou mal formé) mène à `https://www.youtube.com/oembed?url=<page canonique>&format=json`, réponse de 64 Kio au plus, servie par YouTube : son `title` et son `author_name` deviennent titre et site. Sinon, le titre provisoire et l'hôte capturé (RG-26).
- **Délais** : 10 s pour la page, 5 s pour oEmbed, soit les 15 s d'un téléchargement (RG-23) ; la lecture par l'enfant s'y ajoute, comme pour une page.
- **Panne, arrêt, crash** : une panne réseau du NAS sur l'une des deux requêtes laisse la vidéo en file, sans demander oEmbed après la page ; un arrêt aussi ; une vidéo dont la tentative n'a pas fini passe en repli sans aucune requête. Toutes les formes d'une vidéo comptent comme l'hôte `www.youtube.com` pour la logique de panne d'une passe.
- **Liens laissés en file par E2-05** : repris par la passe suivante, sans migration.
- Vérification réelle du 5 novembre 2026, avec les en-têtes du service : page de 1,37 Mo sans page de consentement, données du lecteur présentes ; oEmbed répond le titre et la chaîne.
- Mesures de l'enfant du 5 novembre 2026 (darwin/arm64, pages fictives de 1 900 Kio) : données de forme réelle 10,3 Mio ; description qui remplit la page 18,8 Mio en ASCII, 15,5 Mio en caractères de 4 octets ; 11 à 20 ms par lecture.

**Adresses (RG-27)**

Seules les adresses publiques d'Internet sont contactées. Sont refusées : boucle locale, réseaux privés, lien local, plage Tailscale `100.64.0.0/10`, plages locales IPv6 et plages réservées. Le contrôle porte sur chaque adresse réellement appelée, redirections et changement de résolution DNS compris. Un lien vers l'adresse publique de la maison passe par la box : RG-27 ne le couvre pas (accepté avec Ghislain le 1er novembre 2026).

**Éléments privés (RG-92)**

Ils sont extraits comme les autres : seule l'URL capturée est demandée à son site, rien d'autre ne lui est envoyé, et rien ne part vers l'IA (RG-12). Pour une vidéo YouTube, la page canonique et oEmbed sont demandés à YouTube, le site de l'URL capturée, et ne portent que l'identifiant de la vidéo. Le site voit l'adresse publique du NAS, comme pour un navigateur ; un lien à usage unique est consommé.

**Requête**

- User-Agent `Hippo/<version> (personal read-later)` : le NAS se présente honnêtement.
- Pas de proxy, pas de cookie persistant (un stock de cookies éphémère par élément), identifiants retirés de l'URL.
- Règles du client HTTP : [conventions du backend](dev/conventions-backend.md#http-sortant).

**Ce qui laisse l'élément `queued`**

| Cas | Effet |
| --- | --- |
| Arrêt du backend pendant un téléchargement | Rien n'est écrit sur l'élément ; `started_at` est remis à null ; l'élément est repris au redémarrage |
| Panne réseau côté NAS (aucun résolveur DNS joignable, réseau ou hôte injoignable) | Rien n'est écrit sur l'élément ; la passe essaie encore 2 liens d'autres hôtes au plus, puis le worker attend 5 minutes. Un report ne compte que si un autre lien a joint son site depuis le report précédent de ce lien : une panne du NAS qui empêche la résolution, coupe le réseau ou rend les hôtes injoignables ne fait donc jamais passer un lien en repli ; au 3e report compté (un domaine en panne), le lien passe en repli. Si le DNS répond depuis un cache alors que la liaison Internet est coupée, la connexion expire : c'est un échec du site, et le lien passe en repli |
| Le NAS ne peut pas écrire le fichier temporaire d'un PDF (moins de place libre que la taille du PDF, ou 50 Mio si elle est inconnue, plus 64 Mio de marge ; volume en lecture seule ; erreur d'entrée-sortie) | Rien n'est écrit sur l'élément ; `started_at` est remis à null ; une erreur est loguée (`storage_unavailable`) ; la passe continue avec les éléments qui n'ont pas besoin de fichier (notes, images, pages web), puis le worker attend 5 minutes |

Un site qui ne répond pas, refuse la connexion ou n'existe pas (le résolveur a répondu « hôte inconnu ») est un échec du site : repli.

**Marqueur de tentative**

Avant de télécharger un lien, `started_at` est renseigné dans sa ligne `extraction_attempts` et validé. La ligne disparaît avec l'écriture du résultat ; un arrêt ou un report réseau remet `started_at` à null. Au démarrage, une ligne dont `started_at` est renseigné signale une tentative interrompue (crash, arrêt faute de mémoire) : le lien passe en repli sans être téléchargé de nouveau. Une page hostile ne peut donc pas faire tomber le service en boucle.

**Bibliothèque**

[ADR-0008](adr/0008-extraction-et-reanalyse.md) retient go-readability. Le module d'origine, `github.com/go-shiori/go-readability`, est archivé depuis le 30 décembre 2025 et renvoie vers son fork maintenu, `codeberg.org/readeck/go-readability/v2`, épinglé en v2.1.3 (accord de Ghislain le 1er novembre 2026). Le choix de l'ADR ne change pas. `golang.org/x/net/html` lit les balises `og:*` et `html/charset` décode l'encodage déclaré.

Les PDF sont lus par `github.com/ledongthuc/pdf` (BSD-3, sans dépendance transitive, environ +0,6 Mo de binaire), épinglé à la pseudo-version `v0.0.0-20261020125853-5e46805f718e` faute d'étiquette (accord de Ghislain le 2 novembre 2026), avec notre couche de mise en page plutôt que son `GetPlainText`, qui garde tout le texte d'une page en mémoire (231 Mio sur une page piégée) et perd les espaces que seules les positions donnent. Quatre librairies ont été comparées le 2 novembre 2026 sur des PDF fictifs : `github.com/digitorus/pdf` ne décode pas les polices Type0, cas courant sur le web ; `github.com/pdfcpu/pdfcpu` n'extrait pas le texte ; `github.com/klippa-app/go-pdfium` en WebAssembly lit le mieux mais tient 36 Mio pour un petit PDF et 264 Mio au premier démarrage, au-delà de l'enfant et du conteneur. Seul `internal/extraction` importe la librairie (règle `depguard`) ; elle n'ouvre aucun fichier ni connexion d'elle-même.

## API REST

Le contrat fait foi : `api/openapi.yaml` ([ADR-0017](adr/0017-contrat-openapi.md), [ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)) ; il porte aussi le catalogue des types d'erreur. Toutes les routes sont sous `/api` et exigent `Authorization: Bearer <token>`, sauf `/healthz`. JSON en `snake_case`, erreurs au format problem+json, pagination par curseur. Une route déclarée mais pas encore implémentée répond `501`. Le corps JSON d'une opération implémentée est strict : un membre non déclaré ou des données après la valeur répondent `400` ; un corps JSON de plus de 1 Mio répond `413` ([conventions du backend](dev/conventions-backend.md#contrat-dapi)). Le tableau ci-dessous résume le contrat ; en cas d'écart, le contrat gagne.

| Méthode | Route | Rôle |
| --- | --- | --- |
| POST | /api/items | Capturer : JSON `{url ou note, user_note?, is_private?, client_id?, source?}`, ou multipart avec une image (voir ci-dessous) ; `source` facultatif dans les deux formats, `share` par défaut, ou `manual` ; `201` nouvel élément, `200` doublon ou capture rejouée avec l'élément existant (RG-04, RG-07, RG-08) ; un doublon ajoute sa note datée (RG-04) et ne fait que resserrer la confidentialité : il peut rendre l'élément privé, jamais public (RG-15) |
| GET | /api/items | Lister : `q`, `type` (clé), `category` (id), `tag` (id, répétable), `status`, `read_status`, `pinned`, `unsorted` ; curseur |
| GET | /api/items/{item_id} | Détail complet avec la fiche et les images |
| PATCH | /api/items/{item_id} | Corriger type, catégorie, tags ; épingle, annotations, note, privé (`null` efface). Seul `is_private` est appliqué depuis E2-07 (change `mark-private`, 5 novembre 2026) : il ne change jamais le statut de traitement (RG-14) ; une requête qui porte un autre champ répond `501` en entier, sans rien appliquer, jusqu'à E4-03 |
| PUT | /api/items/{item_id}/progress | Position et statut de lecture |
| DELETE | /api/items/{item_id} | Supprimer |
| POST | /api/items/{item_id}/analyze | Analyse immédiate |
| POST | /api/items/{item_id}/retry | Remettre un élément `failed` en file |
| GET | /api/assets/{asset_id} | Récupérer une image, intacte, à son propriétaire seul (voir ci-dessous) |
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

**Capture d'une image** (E2-04) — `POST /api/items` en `multipart/form-data` :

- Champs : `image` (le fichier, obligatoire), `user_note`, `is_private` (exactement `true` ou `false`), `client_id`, `source`, dans n'importe quel ordre. Les champs texte suivent les règles de la capture JSON (RG-08, RG-09). Un champ non déclaré ou répété, un second fichier, un `image` absent ou une valeur mal formée répondent `400 about:blank`.
- Format : JPEG, PNG, HEIC ou WebP, reconnu au contenu du fichier ; le type et le nom de fichier déclarés par le client sont ignorés. Sinon `400 unsupported-image` (RG-02).
- Taille : 20 Mo au plus, soit 20 × 1024 × 1024 octets ; au-delà, `413 image-too-large`. Le corps entier est borné à 21 Mio (`413 about:blank`), chaque champ texte à 1 Mio.
- L'envoi dispose de 5 minutes, au lieu des 60 s du serveur, pour qu'une image de 20 Mo passe sur un réseau mobile lent.
- Rien n'est stocké en cas de refus. Une capture rejouée répond `200` avec l'élément et son image existante, sans nouveau fichier (RG-08) ; une image n'est jamais un doublon (RG-04).
- L'ordre exact des vérifications est dans le contrat.

**Lecture d'une image** — `GET /api/assets/{asset_id}` renvoie le fichier tel que capturé, métadonnées comprises, avec son `Content-Type` enregistré et `Cache-Control: private, max-age=31536000, immutable`. Une image d'un autre utilisateur ou inconnue répond `404`. Toutes les réponses portent `X-Content-Type-Options: nosniff`.

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
