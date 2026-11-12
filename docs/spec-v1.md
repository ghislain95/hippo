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
| IA | Claude Haiku 4.5, API Message Batches, SDK `github.com/anthropics/anthropic-sdk-go/v2` (v2.7.0), derrière l'interface `analysis.Analyzer` | [0005](adr/0005-haiku-batch.md) |
| Copie des images pour l'IA | `golang.org/x/image` (WebP, mise à l'échelle) et `github.com/gen2brain/h265` (HEIC), dans un processus enfant (voir [Copie de l'image](#copie-de-limage)) | [0038](adr/0038-limite-memoire-384-mo.md) |
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

**users** — `id`, `name`, `api_token_hash`, `created_at`, `categories_seeded_at`. Une ligne en V1, créée au premier démarrage. `categories_seeded_at` (E3-01, migration `00006_analysis.sql`) marque la création unique des catégories de départ : nul pour un utilisateur qui ne les a pas encore reçues.

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
| type_id | INTEGER, nullable | Type de contenu ; ajouté par E3-01, écrit à partir d'E3-02 |
| category_id | INTEGER, nullable | Catégorie ; null = À trier ; ajouté par E3-01, écrit à partir d'E3-02 |
| title | TEXT | Titre affiché : avant analyse, titre provisoire (nom d'hôte d'un lien, première ligne d'une note, note de capture ou date d'une image, [RG-26](metier.md#extraction)), puis titre d'origine ou écrit par l'IA |
| fiche_json | TEXT (JSON), nullable | Fiche générée, au format du type |
| search_text | TEXT | Fiche aplatie en texte, pour la recherche ([texte de recherche](#écriture-dune-réponse-valide)) |
| content_ok | INTEGER (0/1), nullable | 0 = contenu pauvre (RG-37) |
| content_issue | TEXT, nullable | Ce qui manque, selon l'IA |
| incomplete_retried | INTEGER (0/1) | La seconde tentative avec le plafond doublé a déjà eu lieu (RG-36) |
| language | TEXT, nullable | Langue de l'original |
| freshness | TEXT, nullable | `dated` ou `timeless` |
| model, prompt_version, schema_version | TEXT, INTEGER, INTEGER | Traçabilité (RG-64) ; le modèle et la version du prompt viennent du batch, la version du schéma de la fiche du schéma assemblé |
| envelope_version | INTEGER, nullable | Version de l'enveloppe de la réponse, recopiée du batch à l'écriture du résultat (RG-64) ; ajoutée par E3-02 |
| user_edited | TEXT (JSON) | Liste des champs corrigés à la main (RG-60) |
| annotations | TEXT, nullable | Annotations de l'utilisateur |
| is_pinned | INTEGER (0/1) | Épingle |
| read_status | TEXT | `to_read`, `in_progress`, `read` |
| read_progress | REAL | 0 à 1 |
| attempts, last_error | INTEGER, TEXT | Tentatives comptées (RG-35) ; **code d'échec** de la dernière tentative : `invalid_response`, `request_errored`, `too_long` ou `storage_failed`, jamais un message ([issues](#issue-de-chaque-ligne)) |
| batch_id | TEXT (ULID), nullable | Batch de la nuit qui porte l'élément (`batches.id`), écrit à l'envoi ; ajouté par E3-01 |
| created_at, updated_at, extracted_at, analyzed_at, read_at, deleted_at | TEXT | Horodatage |
| change_seq | INTEGER | Séquence de modification, pour la synchronisation |

La table `items` naît avec E2-01 (migration `00002_items.sql`). Elle évolue ainsi :

| Évolution | Story | Raison |
| --- | --- | --- |
| Unicité de `(user_id, normalized_url)`, index partiel hors valeurs nulles | E2-02 (migration `00003_capture_ids.sql`) | Un lien n'existe qu'une fois par utilisateur (RG-04). Les liens capturés avant E2-02 gardent `normalized_url` nul et ne sont jamais reconnus comme doublons : aucune base réelle n'existait avant E6, donc aucun remplissage (une migration n'est que du SQL, [ADR-0032](adr/0032-migrations-goose.md), et RG-05 ne s'écrit pas en SQL) |
| Suppression de `client_id` et de son index | E2-02 (migration `00003_capture_ids.sql`) | Les identifiants de capture passent dans `capture_ids`, seule référence des rejeux ; les valeurs existantes y sont copiées avant la suppression |
| `type_id`, `category_id`, `batch_id`, index `items_user_status` sur `(user_id, status, id)` et `items_batch` sur `batch_id` (partiel, hors valeurs nulles) | E3-01 (migration `00006_analysis.sql`) | Les colonnes référencent `content_types`, `categories`, `batches` ; ajoutées par `ALTER TABLE items ADD COLUMN … REFERENCES …`, nulles pour les éléments existants. Le premier index sert la sélection du batch, le second le retour des éléments d'un batch |
| `envelope_version` | E3-02 (migration `00007_results.sql`) | La version de l'enveloppe qui a produit la fiche (RG-64), recopiée du batch comme le modèle et la version du prompt. Nulle pour les éléments existants et tant qu'un élément n'est pas analysé |

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

**categories** — arborescence de 2 niveaux (RG-51), créée par E3-01 (migration `00006_analysis.sql`).

| Champ | Type | Rôle |
| --- | --- | --- |
| id, user_id | INTEGER, TEXT | Clé et propriétaire |
| name, description | TEXT | La description aide l'IA à ranger |
| parent_id | INTEGER, nullable | Catégorie parente (racine si null) |
| status | TEXT | `active` ou `proposed` |
| proposal_reason | TEXT, nullable | Justification donnée par l'IA |
| color | TEXT, nullable | Couleur d'accent ; héritée du parent si null (RG-59) |
| name_key | TEXT, nullable | **Nom normalisé** (RG-54), écrit par Go seulement (`naming.CategoryKey`) ; ajouté par E3-03 (migration `00007_results.sql`). Non synchronisé |
| created_at, updated_at, deleted_at, change_seq | TEXT, INTEGER | Horodatage et séquence de modification |

- Clé étrangère `(parent_id, user_id)` vers `categories (id, user_id)` (index unique `categories_id_user`) : le parent est une catégorie du même utilisateur (RG-90). Une catégorie n'est pas son propre parent (`CHECK`). La profondeur de 2 niveaux est tenue par le store : l'amorçage n'écrit que des racines et leurs sous-catégories, et une proposition (E3-03) a toujours pour parent une racine, une fois son parent corrigé (RG-55).
- **Catégories de départ** : chaque utilisateur reçoit l'arborescence de [métier §6](metier.md#6-catégories-de-départ), écrite en Go par le store (pas par la migration) : 9 racines avec leur description, 15 sous-catégories sans description (l'IA lit leur chemin, `Dev › Java & Spring`), toutes `active`, couleur nulle, une valeur de `change_seq` par ligne. `categories_seeded_at` est posé dans la même transaction : une seule fois par utilisateur, même s'il supprime ensuite toutes ses catégories. Un utilisateur créé avant E3-01 les reçoit au démarrage suivant.
- Unicité du nom normalisé sous un parent (RG-54) : index unique partiel `categories_user_parent_name` sur `(user_id, COALESCE(parent_id, 0), name_key)`, `WHERE deleted_at IS NULL AND name_key IS NOT NULL` (E3-03, migration `00007_results.sql`). `COALESCE` parce que SQLite tient deux `NULL` pour distincts : sans lui, deux racines du même nom passeraient. Une catégorie supprimée n'interdit jamais une nouvelle du même nom.
- **Remplissage de `name_key`** : `lower()` de SQLite ne plie que l'ASCII et ne retire aucun accent, et une migration n'est que du SQL ([ADR-0032](adr/0032-migrations-goose.md)). `store.Open` remplit donc, après les migrations, le `name_key` de chaque catégorie qui n'en a pas (supprimées comprises), en une transaction, utilisateur par utilisateur, sans avancer `change_seq` ; le nombre est logué au démarrage (`category names normalised`). Deux catégories d'un utilisateur de même nom normalisé sous le même parent arrêtent le démarrage avec une erreur qui nomme leurs deux ids (impossible en V1, où seul l'amorçage écrivait des catégories). Toute insertion de catégorie, amorçage compris, écrit ensuite `name_key`.

**content_types** — partagés entre utilisateurs ([ADR-0022](adr/0022-preparation-multi-utilisateur.md)), créée par E3-01 (migration `00006_analysis.sql`). La migration insère les 7 types de départ, ids 1 à 7 dans l'ordre de [métier §7](metier.md#7-types-de-contenu) : `tech_article`, `tutorial`, `tip`, `recipe`, `place`, `resource`, `note`, tous `active` avec `has_template` à 1. Elle avance `sync_sequence` en SQL d'une valeur par ligne : seule exception à la fonction unique du store ([conventions du backend](dev/conventions-backend.md#stockage-sqlite)). Seule table lue sans `user_id` : elle est partagée par conception, ce n'est pas une table d'un utilisateur.

| Champ | Type | Rôle |
| --- | --- | --- |
| id, key, name | INTEGER, TEXT, TEXT | Identité ; clé en anglais, nom affiché en français |
| status | TEXT | `active` ou `proposed` |
| has_template | INTEGER (0/1) | 0 = fiche et template `generic` |
| proposal_reason | TEXT, nullable | Justification donnée par l'IA |
| updated_at, deleted_at, change_seq | TEXT, INTEGER | Synchronisés vers l'appli ; un type fusionné laisse une trace `deleted_at` (RG-72) |

- Un type proposé (E3-03, RG-57) est créé `proposed`, `has_template` à 0, avec sa justification ; sa fiche est `generic`.
- **Limite connue de la V1 : les types proposés sont partagés.** Un type proposé à partir de l'élément d'un utilisateur, avec sa clé, son nom et sa justification, est visible de tous les utilisateurs, et chacun peut l'accepter ou le fusionner (RG-72, E4). Accepté pour la V1, qui n'a qu'un utilisateur (décidé avec Ghislain le 7 novembre 2026, change `apply-batch-results`). Un nouvel ADR qui amende [ADR-0022](adr/0022-preparation-multi-utilisateur.md) est exigé avant la création d'un second utilisateur.

**batches** — un batch de la nuit, créé par E3-01 (migration `00006_analysis.sql`). Non synchronisé vers l'appli. Rôle des statuts : [Envoi du batch de la nuit](#envoi-du-batch-de-la-nuit).

| Champ | Type | Rôle |
| --- | --- | --- |
| id | TEXT (ULID) | Identifiant du batch chez Hippo, créé avant l'envoi |
| user_id | TEXT | Propriétaire |
| anthropic_batch_id | TEXT, nullable, unique | Identifiant donné par Anthropic ; nul tant que l'envoi n'est pas confirmé |
| status | TEXT | `pending` (éléments soumis, envoi pas encore confirmé), `in_progress` (accepté par Anthropic), `ended` (terminé : résultats appliqués, ou indisponibles, coût enregistré ; E3-02), `failed` (refusé, jamais arrivé ou annulé avant l'envoi) |
| model, prompt_version, envelope_version | TEXT, INTEGER, INTEGER | Modèle, version du prompt et version de l'enveloppe de la réponse (RG-64) |
| item_count | INTEGER | Éléments soumis, de 1 à 200 (`CHECK`). Après le retrait d'éléments cochés Privé avant l'envoi, le nombre d'éléments encore dans le batch ; un batch passé en `failed` faute d'élément restant garde le dernier nombre écrit (celui d'origine si tous ses éléments ont été retirés d'un coup) |
| input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens | INTEGER, nullable | Tokens consommés, sommés sur toutes les lignes `succeeded` des résultats et écrits à la fermeture du batch (RG-80, E3-02). `cache_creation_input_tokens` additionne les écritures en cache de 5 minutes et d'1 heure |
| cost_usd | REAL, nullable | Coût calculé ([formule](#tokens-et-coût)), écrit une seule fois à la fermeture du batch (E3-02) ; compté dans le mois de `created_at` (RG-81) |
| created_at | TEXT | Création, juste avant l'envoi |
| sent_at | TEXT, nullable | Acceptation par Anthropic |
| ended_at | TEXT, nullable | Fin du batch donnée par Anthropic, sinon l'heure de sa fermeture par Hippo (E3-02) |
| results_unavailable_at | TEXT, nullable | Fermeture d'un batch dont Anthropic ne sert plus les résultats (**résultats indisponibles**) ; ajouté par E3-02 (migration `00007_results.sql`) |
| results_lines_read | INTEGER, nullable | Nombre de lignes de la lecture coupée la plus complète, dont les sommes de tokens sont gardées dans la ligne ; ajouté par E3-02 (migration `00007_results.sql`) |

Index `batches_user_created` sur `(user_id, created_at)`, pour le coût du mois. Table `STRICT`. Aucun statut n'est ajouté par E3-02 : changer le `CHECK` de `status` demanderait de reconstruire la table, que `items.batch_id` référence ; un batch aux résultats indisponibles est `ended` avec `results_unavailable_at`.

**tags** — les tags d'un utilisateur, créée par E3-04 (migration `00007_results.sql`). Synchronisée vers l'appli.

| Champ | Type | Rôle |
| --- | --- | --- |
| id | INTEGER | Identifiant |
| user_id | TEXT | Propriétaire |
| name | TEXT | Forme normalisée seulement (RG-58), de 1 à 40 caractères (`CHECK`, garde-fou derrière `naming.Tag`) |
| updated_at, deleted_at, change_seq | TEXT, INTEGER | Synchronisation ; un tag fusionné laisse une trace `deleted_at` (RG-74) |

- Index uniques `tags_id_user` sur `(id, user_id)`, cible de la clé composée de `item_tags`, et `tags_user_name` sur `(user_id, name)`, partiel `WHERE deleted_at IS NULL` : un tag supprimé n'interdit pas un nouveau tag du même nom. Index `tags_user_change_seq` sur `(user_id, change_seq)`. Table `STRICT`.

**item_tags** — les tags portés par un élément, créée par E3-04 (migration `00007_results.sql`).

| Champ | Type | Rôle |
| --- | --- | --- |
| item_id | TEXT (ULID) | Élément |
| tag_id | INTEGER | Tag |
| user_id | TEXT | Propriétaire, le même que celui de l'élément et du tag |

- Clé primaire `(item_id, tag_id)`. Clés étrangères `(item_id, user_id)` vers `items (id, user_id)` et `(tag_id, user_id)` vers `tags (id, user_id)` : un élément ne porte que des tags de son utilisateur, même par erreur de code (RG-90). Index `item_tags_tag` sur `tag_id`. Table `STRICT`, `WITHOUT ROWID`.
- La table n'est pas synchronisée seule : les tags d'un élément voyagent dans `Item` (`tag_ids`), et toute écriture de `item_tags` avance le `change_seq` de l'élément dans la même transaction.

**Autres tables**

- **analysis_calls** : les analyses immédiates, de même forme que `batches`, pour le calcul du budget (E3-08).
- **settings** : réglages clé-valeur par utilisateur, dont `last_review_at`.
- **sync_sequence** : une seule ligne (`id = 1`), la valeur courante de la séquence de modification. Elle n'avance que par une seule fonction du store, dans la transaction de l'écriture ([conventions du backend](dev/conventions-backend.md#stockage-sqlite)).
- **items_fts** : table virtuelle FTS5 sur `title`, `search_text`, `user_note`, `annotations` et les tags, tokenizer `unicode61 remove_diacritics 2`, tenue à jour par des triggers.

Les migrations sont versionnées et appliquées au démarrage.

## Pipeline d'analyse

Un élément passe par cinq étapes ; seule l'étape 3 appelle l'IA, une fois par nuit, en un seul batch. Le cycle de vie complet est dans le [document métier](metier.md#4-cycle-de-vie-dun-élément).

1. **Capture** — `POST /api/items` crée l'élément en `queued` et répond tout de suite (RG-01 à RG-06).
2. **Extraction** — en arrière-plan, juste après : page web via go-readability, texte d'un PDF, titre, chaîne, durée et description d'une vidéo YouTube (sans transcription en V1), rien pour une image ou une note. Repli sur les métadonnées `og:*` (ou le titre du PDF) en cas d'échec ; l'élément passe toujours à `extracted` (RG-20 à RG-28). Détail ci-dessous, [Extraction d'un élément](#extraction-dun-élément).
3. **Envoi du batch** — une fois par nuit, à l'heure de Paris (`BATCH_SCHEDULE`, 2 h par défaut), les éléments `extracted` **non privés** de chaque utilisateur partent dans un seul batch, 200 au maximum et 200 Mio au plus, après les gardes (clé d'API, batch sans coût, budget) (RG-30 à RG-32, RG-81). Les images partent en copie réduite et sans métadonnées (RG-38). Statut `submitted`. Détail ci-dessous, [Envoi du batch de la nuit](#envoi-du-batch-de-la-nuit).
4. **Suivi** — l'état de chaque batch en cours est demandé à Anthropic au démarrage, toutes les 15 minutes, et juste avant les gardes de la passe de nuit, jusqu'à la fin du batch (24 h au maximum). Des résultats qu'Anthropic ne sert plus ferment le batch. Détail ci-dessous, [Suivi du batch et résultats](#suivi-du-batch-et-résultats).
5. **Résultats** — lecture ligne par ligne, validation puis écriture, création des propositions, normalisation et réutilisation des tags, gestion des échecs et des réponses coupées par le plafond de sortie, coût du batch (RG-34 à RG-37, RG-53 à RG-58, RG-80). Statut `analyzed`, `extracted` ou `failed`. Même section.

L'**analyse immédiate** (`POST /api/items/{id}/analyze`) envoie la même requête en appel direct.

Le format exact des requêtes, le prompt, les schémas des fiches et l'ordre de validation sont dans [`prompt/`](prompt/README.md). Ils ne sont volontairement pas recopiés ici.

**Budget de tokens**

- Entrée : le contenu extrait seul, jamais le HTML, jusqu'à ~120 000 caractères ; pour une image, sa copie réduite à 1 568 px sur son grand côté (RG-38, [Copie de l'image](#copie-de-limage)). L'API peut la réduire encore (budget d'environ 1,15 Mpx par image, non vérifié) : sans effet sur le résultat attendu.
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
- **Dossier temporaire** : le PDF est copié, sans passer par la mémoire du service, dans un fichier de `DATA_DIR/tmp` **dont le nom est supprimé avant le téléchargement**. Le service écrit par un descripteur, l'enfant lit par un second, en lecture seule, reçu comme descripteur 3. Les données disparaissent avec le dernier descripteur, crash compris ; un fichier laissé par un crash entre la création et la suppression du nom est supprimé au démarrage. Rien d'un PDF ne reste sur le NAS ni dans les sauvegardes. Règles : [conventions du backend](dev/conventions-backend.md#dossier-temporaire).
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

### Envoi du batch de la nuit

Mise en œuvre de RG-12, RG-14, RG-30 à RG-32, RG-38 et RG-81 par E3-01 (change `submit-nightly-batch`, novembre 2026). Package `internal/analysis` (la passe), `internal/schedule` (l'horaire), `internal/analysis/anthropic` (le client). Le format des requêtes est dans [`prompt/`](prompt/README.md).

**Horaire (RG-30)**

- `BATCH_SCHEDULE` n'accepte que la forme quotidienne `M H * * *` : minute et heure en nombres d'un ou deux chiffres, un seul espace entre les champs. Toute autre forme (liste, intervalle, pas, nom, jour ou mois fixé, six champs) arrête le démarrage avec une erreur qui nomme la variable. Défaut : `0 2 * * *`.
- L'heure est toujours celle de Paris (`Europe/Paris`, embarqué dans le binaire), jamais le fuseau du système : `TZ` ne règle que l'horodatage des logs.
- Une seule exécution par date de Paris. À l'automne, une heure qui existe deux fois s'exécute la première fois. Au printemps, une heure qui n'existe pas (entre 2 h et 3 h) s'exécute à la fin du trou, 3 h.
- Pas de rattrapage : un redémarrage après l'heure attend la date suivante. Un réveil plus de 10 minutes après l'instant prévu (`schedule.LateTolerance` ; NAS sorti de veille, horloge qui avance) n'exécute rien et logue `missed_run`.
- Une horloge qui recule pendant une passe ne la relance pas pour la même date.
- La passe et l'horaire ont leur propre contexte ; l'arrêt du service attend la passe en cours avant de fermer la base.

**Gardes, dans cet ordre, avant toute lecture de contenu**

1. **Clé d'API** : sans `ANTHROPIC_API_KEY`, rien n'est envoyé, réconcilié ni suivi (`no_api_key`, une ligne par passe). Le service démarre quand même.
2. **Batch sans coût** : après la réconciliation de ses batchs `pending` puis le [suivi](#suivi-du-batch-et-résultats) de ses batchs en cours, un utilisateur qui a encore un batch `pending`, `in_progress`, ou `ended` sans coût enregistré n'envoie rien (`cost_pending`). Un batch `failed` ne bloque jamais. Le suivi ferme chaque batch avec son coût, y compris quand ses résultats ne sont plus disponibles : la garde ne bloque que tant qu'Anthropic traite le batch, ou tant que son état ne peut pas être demandé (clé révoquée, panne du service). Un batch terminé à 1 h 56 est appliqué avant la garde de 2 h (RG-30).
3. **Budget (RG-81)** : somme des `cost_usd` des batchs de l'utilisateur créés dans le mois en cours, de minuit le 1er à minuit le 1er du mois suivant, heure de Paris. Atteint (`>=`, donc un budget de 0 n'envoie jamais) : rien n'est envoyé (`budget_reached`). Un batch compte dans le mois de son envoi, même s'il finit le mois suivant.

Chaque refus logue l'utilisateur, la raison et le nombre d'éléments en attente. Aucune image n'est décodée pour une passe qui n'enverra rien.

**Sélection (RG-32)**

- Par utilisateur (RG-90) : éléments `extracted`, non privés, non supprimés, les plus anciens d'abord (ordre des ULID), 200 au plus.
- Le corps assemblé ne dépasse pas 200 Mio (209 715 200 octets, sous la limite de 256 Mo annoncée par l'API, non vérifiée) : la requête qui ne tient plus et toutes les suivantes attendent la nuit suivante.
- Un élément supprimé depuis la sélection est sauté, la passe continue.

**Construction**

- Une requête par élément, `custom_id` = id de l'élément. Partie fixe du prompt, référentiel (construit une fois par batch) et outil identiques octet pour octet dans chaque requête.
- L'URL envoyée est l'URL capturée sans ses identifiants (`user:mot-de-passe@`), paramètres de suivi compris : c'est ce qui a été partagé (RG-92).
- Le contenu d'un élément est lu au moment d'écrire sa requête : un seul en mémoire à la fois.
- Chaque requête est écrite dans un fichier de **segments** ; le **corps** est ensuite assemblé, dans un second fichier, à partir des seuls éléments soumis, puis envoyé en flux. Le service ne tient jamais un corps en mémoire. Deux fichiers temporaires sans nom ([conventions du backend](dev/conventions-backend.md#dossier-temporaire)) : au plus deux fois 200 Mio de disque, un instant. Un disque plein arrête la passe de l'utilisateur avant la soumission (`storage_unavailable`).
- Garde-fou de RG-12 : un élément privé qui atteindrait la construction l'arrête ; rien n'est soumis ni envoyé, une erreur est loguée (`private_item`).

**Soumission et envoi**

1. **Transaction** (`BEGIN IMMEDIATE`) : le statut, `is_private` et `deleted_at` de chaque élément sont relus pour l'utilisateur ; seuls ceux encore `extracted`, non privés et non supprimés sont gardés. Le batch est inséré en `pending` et ces éléments passent à `submitted` (`batch_id`, `updated_at`, `change_seq`), sans changer `attempts`. Aucun élément gardé : aucune ligne, aucun appel (RG-31).
2. **Assemblage du corps** à partir des éléments soumis.
3. **Retrait des éléments devenus privés** : les éléments soumis sont relus ; un élément coché Privé depuis la transaction est retiré du batch et revient à `extracted`, sans fiche, et le corps est réécrit sans lui. Cette étape se répète jusqu'à ce qu'aucun élément ne soit retiré ; les autres partent la même nuit (RG-12, RG-14). S'il n'en reste aucun, le batch passe en `failed` et rien n'est envoyé.
4. **Arrêt** : un service qui s'arrête à ce moment n'envoie rien ; le batch passe en `failed`, ses éléments reviennent à `extracted`.
5. **Envoi** : un seul appel, sans nouvelle tentative automatique (une création rejouée serait un double envoi), 10 minutes au plus. Pour un élément coché Privé, il ne reste comme fenêtre que l'envoi lui-même.
6. **Issue** :

| Réponse | Effet |
| --- | --- |
| Acceptée (2xx avec l'identifiant du batch) | Batch `in_progress`, `anthropic_batch_id` et `sent_at` enregistrés |
| Refus (4xx) | Batch `failed` ; ses éléments reviennent à `extracted` sans tentative comptée et repartent la nuit suivante |
| Inconnue : coupure, délai, arrêt pendant l'envoi, crash, redirection, erreur du service (5xx, 529 compris), 2xx sans identifiant lisible | Rien n'est écrit : le batch reste `pending` et la réconciliation le règle |

Les écritures après l'envoi passent par un contexte non annulable, borné à 10 s : un batch accepté pendant un arrêt est enregistré.

**Réconciliation**

- Au démarrage, en arrière-plan, et avant la passe de chaque utilisateur qui a un batch `pending`, quand une clé d'API existe. La passe, la réconciliation et le suivi ne tournent jamais en même temps.
- La liste des batchs de la clé est lue, du plus récent au plus ancien : 100 par page, 10 pages au plus, 2 minutes au plus.
- Pour chaque batch `pending`, du plus ancien au plus récent, un candidat est un batch créé entre 10 minutes avant et 20 minutes après lui, avec le même nombre de requêtes, et dont l'identifiant n'est pas déjà enregistré. Un seul candidat : il est adopté (`in_progress`). Aucun ou plusieurs : le batch passe en `failed`, ses éléments reviennent à `extracted`, et un avertissement `possible_double_send` est logué.
- Une liste en échec ou incomplète laisse le batch `pending` : la garde « batch sans coût » bloque l'utilisateur jusqu'à une réconciliation réussie.
- **Risque résiduel**, accepté au cadrage : un batch accepté par Anthropic mais pas rapproché est renvoyé la nuit suivante. Cas possibles : deux batchs `pending` de même taille, de deux utilisateurs, dans la même fenêtre ; un refus 4xx donné après la création, ce que l'API n'est pas connue pour faire. Au plus un double envoi par batch concerné, toujours logué. En V1, un seul utilisateur et une clé dédiée : seul le second cas reste.

**Logs**

- Une ligne par utilisateur et par passe : `user_id`, `outcome` (`sent`, `nothing_to_send`, ou la raison du refus ou de l'échec), éléments sélectionnés, soumis et laissés en attente, images sans copie, taille du corps, identifiants du batch, durée.
- Un avertissement par batch dont des éléments cochés Privé ont été retirés avant l'envoi : `user_id`, `batch_id`, `outcome` `private_after_submit`, `removed` (éléments retirés), `remaining` (éléments restants).
- Une ligne par image sans copie : `item_id`, `asset_id`, `reason`. Une ligne par batch réconcilié : `batch_id`, `item_count`, `outcome` (`adopted`, `possible_double_send`, `list_failed`, `not_pending`).
- Jamais un titre, une URL, une note, un contenu, une image, un corps, le message d'erreur de l'API ni la clé ; un test le vérifie.

### Copie de l'image

Mise en œuvre de RG-38. L'IA reçoit une copie, jamais le fichier stocké, qui ne change pas.

- **Processus enfant** `hippo copy-image`, aux mêmes règles que celui de l'extraction : environnement réduit à `GOMAXPROCS=2`, ni configuration, ni base, ni log. Il reçoit l'image stockée en lecture seule sur le descripteur 3 et écrit la copie dans un fichier temporaire sans nom, descripteur 4. Aucun chemin ne traverse la frontière du processus. Une image à la fois.
- **Étapes** : lecture de l'en-tête seul ; plus de 50 Mpx, refus sans décodage. Décodage (JPEG, PNG, WebP ; HEIC, images en grille comprises). Réduction, jamais d'agrandissement : moyenne de blocs entiers, puis interpolation bilinéaire jusqu'à 1 568 px sur le grand côté. Redressement : orientation EXIF d'un JPEG, propriétés `irot` et `imir` d'un HEIC. Transparence aplatie sur du blanc. Encodage JPEG qualité 85, sans aucun segment de métadonnées (ni EXIF, ni XMP, ni profil ICC, ni commentaire).
- **Couleurs** : un HEIC et un WebP avec pertes sont convertis selon la matrice et la plage qu'ils déclarent ; un HEIC à matrice inconnue est illisible. Le profil ICC (Display P3) est ignoré : une telle photo sort un peu moins saturée.
- **Limites** : 256 Mio de mémoire (limite souple 200 Mio), 30 s, copie de 5 Mio au plus.
- **Échec** : la requête part sans bloc image (RG-38), la passe continue. Raisons loguées : `too_many_pixels`, `undecodable`, `too_large`, `timeout`, `copy_failed`, `asset_missing`. Un enfant qui ne peut pas démarrer (binaire absent, `fork` refusé) arrête la passe de l'utilisateur avant la soumission (`copier_unavailable`) : rien n'est envoyé.
- **Cas connus sans image** à 48 Mpx : JPEG progressif, PNG entrelacé, PNG 16 bits, HEIC de plus de 8 bits. Les téléphones écrivent des JPEG de base et des PNG non entrelacés.
- **Bibliothèques** : `golang.org/x/image` v0.47.0 (WebP, interpolation) et `github.com/gen2brain/h265` v0.2.4 (HEIC, Go pur, sans cgo), accord de Ghislain le 6 novembre 2026. Seul `internal/imagecopy` les importe (règle `depguard` `image-decoding-only-in-imagecopy`).
- **Mesures de l'enfant** du 6 novembre 2026 (darwin/arm64, images synthétiques bruitées, lancement compris) :

| Image | Pic résident | Temps |
| --- | --- | --- |
| JPEG 12 Mpx | 44 Mo | 0,33 s |
| Capture d'écran PNG, 1290 × 2796 | 40 Mo | 0,06 s |
| HEIC 12 Mpx | 53 Mo | 0,54 s |
| JPEG 48 Mpx | 91 Mo | 1,10 s |
| HEIC 48 Mpx, en grille | 120 Mo | 1,91 s |
| PNG 48 Mpx | 209 Mo | 0,46 s |

Mesuré le 6 novembre 2026 dans le conteneur linux/arm64 limité à 384 Mo : un HEIC de 48 Mpx copié pendant la lecture d'un PDF de 49 Mo. La mémoire du conteneur a culminé à 215 Mio, sans processus tué ; le service a répondu à tous les contrôles de santé et le dossier temporaire était vide après coup. Le pire cas mesuré, un PNG de 48 Mpx, a culminé à 289 Mio. Ces pics ont été mesurés avec une surveillance à la milliseconde ; à 10 ms, un enfant peut aller un peu plus loin avant de s'arrêter, ce qui reste à remesurer pour les cas qui dépassent la limite (PNG entrelacé ou 16 bits). La copie y a pris 13 à 16 s quand l'enfant surveillait sa mémoire toutes les millisecondes, 2,3 s avec l'intervalle de 10 ms retenu pour la copie d'image ; sur le NAS (amd64), le 12 novembre 2026, l'envoi d'un batch avec une capture d'écran et une page a pris 829 ms au total, copie de l'image comprise ; une photo de 48 Mpx y reste à mesurer, une copie au-delà de 30 s partant sans image. Le plafond reste à 50 Mpx.

Le plafond de 50 Mpx tient sous la limite de 256 Mio. La limite du conteneur passe à 384 Mo pour faire place à cet enfant ([ADR-0038](adr/0038-limite-memoire-384-mo.md)).

### Suivi du batch et résultats

Mise en œuvre de RG-12 à RG-14, RG-30, RG-35 à RG-37, RG-50 à RG-59, RG-61, RG-64, RG-79 à RG-81 et RG-90 par E3-02 à E3-05 (change `apply-batch-results`, novembre 2026). Packages `internal/analysis` (suivi, validation, coût), `internal/analysis/anthropic` (état du batch et lecteur des résultats), `internal/store` (écritures), `internal/naming` (normalisations).

#### Suivi (étape 4)

- **Quand** : au démarrage, après la réconciliation ; puis toutes les 15 minutes (`analysis.FollowInterval`), l'attente suivante ne commençant qu'à la fin du suivi en cours (aucun suivi en file) ; et dans la passe de nuit, pour chaque utilisateur, après la réconciliation de ses batchs `pending` et avant la garde « batch sans coût ». Le suivi prend le même verrou que la passe et la réconciliation.
- Sans clé d'API, rien n'est suivi et rien n'est logué à chaque passage.
- **Batchs suivis**, par utilisateur (RG-90) : ceux `in_progress`, du plus ancien au plus récent, et ceux `ended` sans coût (aucun écrivain n'en laisse depuis E3-02). Rien à suivre : aucun appel.
- **État du batch**, demandé à Anthropic (1 minute au plus, nouvelles tentatives du SDK comprises) :

| Réponse | Effet |
| --- | --- |
| En cours ou en annulation | Rien n'est écrit (`in_progress`) |
| Terminé | Lecture et application des résultats, 10 minutes au plus |
| Inconnu (404) ou archivé | Fermeture du batch, résultats indisponibles (ci-dessous) |
| Toute autre erreur (401, 5xx, délai) | Rien n'est écrit (`poll_failed`) ; nouvel essai à l'occasion suivante |

- Une clé révoquée ou fausse donne `poll_failed` toutes les 15 minutes, et le batch bloque l'utilisateur jusqu'à ce que la clé soit corrigée ; visible dans les logs seulement jusqu'à l'alerte de Réglages (E3-08).
- Un batch à suivre sans `anthropic_batch_id` (aucun écrivain n'en laisse) ne peut pas être demandé : il est fermé comme aux résultats indisponibles.
- Le suivi n'envoie rien et ne construit aucune requête (RG-12) : il ne lit que l'état et les résultats.

#### Lecture des résultats

- Le fichier des résultats est lu **une ligne à la fois**, par le lecteur de Hippo et non par le flux du SDK, qui s'arrête à la première ligne illisible. Une ligne fait **4 Mio au plus** (4 194 304 octets) : une sortie de 8 000 tokens fait environ 32 Ko. La mémoire est bornée à une ligne et sa forme décodée.
- **Ligne illisible** : pas du JSON, sans `custom_id`, d'un type de résultat inconnu, `succeeded` sans message, ou plus de 4 Mio. Elle est sautée et comptée, la lecture continue ; son élément reste sans résultat. Une ligne vide n'est pas une ligne.
- Un résultat dont le `custom_id` n'est pas un élément `submitted` de ce batch et de cet utilisateur est ignoré et compté (`unknown`). Une ligne répétée n'est appliquée qu'une fois.
- **Contrôle de l'enveloppe**, une fois par batch, avant la première ligne : la version de l'enveloppe enregistrée dans le batch est comparée à celle du binaire. Différente (un redéploiement entre l'envoi et les résultats), chaque réponse `succeeded` remet son élément dans la file sans tentative comptée.
- **Lecture coupée** (coupure, délai, arrêt du service) : les sommes de tokens lues et le nombre de lignes sont enregistrés dans le batch quand cette lecture est allée plus loin que la précédente (`results_lines_read`) ; `read_failed` est logué ; la lecture suivante reprend depuis le début. Rien n'est appliqué deux fois, rien n'est perdu.
- **Écriture refusée par la base** (contrainte, disque plein, ligne abîmée), hors contexte fini : une réponse valide que la base refuse d'écrire est réglée une fois avec une tentative et le code `storage_failed` (RG-35), `failed` à la troisième tentative ; la ligne est logguée en erreur (`error_type=store`). Sans ce repli, un échec reproductible sur une ligne ferait échouer chaque lecture au même endroit : le batch ne fermerait jamais et bloquerait toutes les nuits suivantes. Si ce réglage échoue aussi (base indisponible), ou si l'écriture de toute autre issue échoue, la lecture est coupée comme ci-dessus et retentée au suivi suivant. Une panne persistante de la base donne donc une ligne d'erreur toutes les 15 minutes, et la garde « batch sans coût » bloque les nuits tant qu'elle dure.

#### Issue de chaque ligne

| Ligne | Élément ensuite |
| --- | --- |
| `succeeded`, enveloppe du batch différente de celle du binaire | `extracted`, `batch_id` nul, tentatives et `last_error` inchangés |
| `succeeded`, `stop_reason = max_tokens`, première fois | `extracted`, `batch_id` nul, `incomplete_retried` à 1, sans tentative ; plafond de 8 000 la nuit suivante (RG-36) |
| `succeeded`, `stop_reason = max_tokens`, déjà retentée | `failed`, `last_error = too_long` (« fiche trop longue »), sans tentative ; contenu extrait gardé (RG-36) |
| `succeeded`, autre `stop_reason`, ou validation en échec | Une tentative, `last_error = invalid_response` ; `extracted` avec `batch_id` nul, ou `failed` à la 3e tentative (RG-35) |
| `succeeded`, réponse valide | `analyzed` ([écriture](#écriture-dune-réponse-valide)) |
| `succeeded`, réponse valide que la base refuse d'écrire | Une tentative, `last_error = storage_failed` ; `extracted` avec `batch_id` nul, ou `failed` à la 3e tentative (RG-35) |
| `errored` | Une tentative, `last_error = request_errored` ; `extracted`, ou `failed` à la 3e tentative (RG-35) |
| `expired`, `canceled` | `extracted`, `batch_id` nul, sans tentative (RG-35) |
| Ligne illisible, ou aucune ligne pour l'élément | `extracted`, `batch_id` nul, sans tentative, à la fermeture du batch (RG-35) |

- Chaque issue s'écrit en une transaction `BEGIN IMMEDIATE` qui relit l'élément pour l'utilisateur, et n'écrit que s'il est encore `submitted` dans ce batch et non supprimé : c'est la clé d'idempotence. Un élément supprimé depuis l'envoi ne reçoit rien (RG-79), et aucune proposition ni aucun tag n'est créé pour lui. Chaque écriture avance `updated_at` et `change_seq` ([ADR-0035](adr/0035-contrat-identifiants-et-synchronisation.md)).
- Une remise en file ne remet jamais `attempts` à zéro. Un élément `failed` garde son `batch_id` (trace de la dernière tentative) ; un élément `extracted` l'a nul.
- Un élément coché Privé après le départ de son batch en reçoit le résultat (RG-13, RG-14). Revenu à `extracted` (réponse coupée, requête non traitée), il ne repart pas : il est privé (RG-12).
- `last_error` ne contient qu'un **code d'échec**, jamais le message de l'API ni celui du validateur, qui peuvent citer la requête ou la fiche.

#### Validation

L'ordre de validation d'une réponse, ses classes de motif et ce qui est rejeté sont dans [`prompt/README.md`](prompt/README.md#validation-dune-réponse). Les étapes qui ne lisent pas la base tournent dans `internal/analysis`, avant la transaction ; celles qui la lisent (type, template, catégorie) dans la transaction d'écriture. La première étape en échec rejette la réponse entière : rien n'est écrit, ni fiche, ni proposition, ni tag (RG-35). Le schéma assemblé est compilé une fois au démarrage ; un schéma de fiche sans `x-schema-version` arrête le démarrage.

**Déploiement d'un changement de schéma** : un schéma de fiche modifié sans changement de la version de l'enveloppe, déployé pendant qu'un batch est en cours, valide ses réponses contre le nouveau schéma, qui peut les rejeter avec une tentative. On déploie donc un tel changement quand aucun batch n'est en cours : la dernière ligne `analysis follow-up` du batch de la nuit donne `outcome=ended` ou `results_unavailable`.

#### Écriture d'une réponse valide

Une transaction par élément (`store.WriteAnalysis`), dans cet ordre :

1. **Type** (RG-57) : un type existant choisi par sa clé ; un type proposé dont la clé existe déjà range l'élément sous ce type, sans le modifier, avec sa fiche générique (RG-72) ; sinon le type est créé `proposed`, sans template, partagé par tous les utilisateurs ([limite de la V1](#modèle-de-données), `content_types`).
2. **Catégorie** (RG-53 à RG-55, RG-59) : une catégorie de l'utilisateur choisie par son id ; une catégorie proposée a d'abord son parent corrigé (RG-55), puis est rapprochée par **nom normalisé** des catégories non supprimées de l'utilisateur sous ce parent (RG-54) ; sans correspondance, elle est créée `proposed`, sans couleur propre (héritée du parent, RG-59), et l'élément y est rangé.
3. **Tags** (RG-58) : chaque tag normalisé reprend le tag non supprimé de l'utilisateur de ce nom, sinon un tag est créé. Les tags de l'élément sont remplacés par ce jeu.
4. **Corrections manuelles** (RG-61) : un champ listé dans `user_edited` (type, catégorie, tags) est validé, mais garde sa valeur et ne crée rien.
5. **Élément** : statut `analyzed`, type, catégorie, titre, fiche (telle que reçue, compactée), texte de recherche, langue, fraîcheur, contenu pauvre et ce qui manque (RG-37), modèle, version du prompt et version de l'enveloppe **du batch**, version du schéma de la fiche (RG-64), `analyzed_at` ; `attempts`, `last_error` et `incomplete_retried` remis à zéro ; nouveau `change_seq`.

- Rien de ce qui existe ne change : aucune catégorie, aucun type, aucun tag n'est renommé, déplacé, fusionné ni supprimé (RG-56). Une catégorie, un type ou un tag réutilisé l'est tel quel.
- Deux éléments d'un même batch qui proposent la même catégorie, le même type ou le même nouveau tag n'en créent qu'un : les transactions se suivent, la seconde trouve la ligne de la première ; les index uniques rendent un doublon impossible.
- **Texte de recherche** (`search_text`, [ADR-0007](adr/0007-fiche-json-source-de-verite.md)) : chaque chaîne de la fiche, dans l'ordre du document, une par ligne ; sauf la valeur du membre `template` de la racine et celle d'un membre `language` d'un objet imbriqué (le langage d'un bloc de code). Ni les clés, ni les nombres, booléens et `null`. Le code est gardé : une commande doit se retrouver (RG-41). L'index plein texte qui le lit arrive avec E4.

#### Tags du référentiel

Le bloc `<tags>` du référentiel (RG-52, format dans [`prompt/system-prompt.md`](prompt/system-prompt.md)) liste les tags non supprimés de l'utilisateur portés par au moins un élément non privé et non supprimé (RG-12), du plus porté au moins porté puis par nom, 300 au plus, séparés par `, `. Il est construit une fois par batch avec le reste du référentiel : chaque requête reste identique octet pour octet (cache de prompt).

#### Fermeture du batch

- **Résultats lus en entier** : en une transaction filtrée par `user_id`, chaque élément encore `submitted` dans ce batch et non supprimé revient à `extracted` sans tentative ; les sommes de tokens de cette lecture complète et le coût sont écrits ; le batch passe `ended`, avec la date de fin d'Anthropic.
- **Résultats indisponibles** (batch inconnu d'Anthropic, archivé, ou au-delà de la conservation de 29 jours) : même fermeture, avec les tokens de la lecture coupée la plus complète, zéro sinon, leur coût, et `results_unavailable_at` (RG-80). Le budget peut être sous-compté d'une nuit au plus.
- Le coût n'est écrit qu'une fois (`cost_usd IS NULL`). Un batch déjà fermé entre-temps ne change pas (`not_in_progress`).

#### Tokens et coût

- Les tokens de **chaque** ligne `succeeded` comptent, quelle que soit son issue (appliquée, rejetée, coupée, élément supprimé, `custom_id` inconnu). Les requêtes `errored`, `expired` et `canceled` ne sont pas facturées et n'ajoutent rien.
- Coût d'un batch, en dollars : `0,5 × ((entrée + 1,25 × écriture en cache 5 min + 2 × écriture en cache 1 h + 0,1 × lecture en cache) × prix d'entrée + sortie × prix de sortie) / 1 000 000`. La remise batch (moitié prix) et les rapports du cache sont des constantes du code ; ils se cumulent (page de tarifs d'Anthropic, relue le 7 novembre 2026). Le coût se calcule sur la répartition des deux durées d'écriture, avant leur somme dans `cache_creation_input_tokens`.
- Une lecture coupée n'enregistre que la somme des deux durées : le coût d'un batch aux résultats indisponibles compte ses écritures en cache comme des écritures de 5 minutes, la seule durée que posent les requêtes.
- **Prix** : `ANALYSIS_INPUT_PRICE_USD_PER_MTOK` et `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK`, prix standard de `ANALYSIS_MODEL` par million de tokens, 1 et 5 par défaut (Haiku 4.5) ; une valeur qui n'est pas un décimal fini et positif arrête le démarrage avec une erreur qui nomme la variable. Le démarrage logue le modèle et les deux prix (`analysis prices`). Les variables sont dans `infra/.env.example` ; on les change avec le modèle ou ses tarifs.
- Un batch compte dans le mois de sa création, même s'il finit le mois suivant (RG-81).

#### Logs

- Une ligne par batch suivi (`analysis follow-up`) : `user_id`, `batch_id`, `anthropic_batch_id`, `outcome` (`in_progress`, `ended`, `results_unavailable`, `poll_failed`, `read_failed`, `not_in_progress`), compteurs (`analyzed`, `invalid`, `cut`, `too_long`, `errored`, `requeued`, `failed`, `deleted`, `unknown`, `unreadable_lines`, `without_result`), sommes de tokens, `cost_usd`, `duration_ms`. Pour `poll_failed` et `read_failed`, seulement le statut HTTP et la classe d'erreur ; quand la classe est `store`, la ligne porte aussi `error`, le texte de l'erreur de la base, qui ne cite que des identifiants et des colonnes. Un réglage de la lecture coupée qui échoue donne une ligne à part, `analysis follow-up: record the cut read`, au niveau erreur avec `error_type=store` et ce seul texte. Une réponse réglée en `storage_failed` est comptée dans `invalid`. Niveau : info pour `ended` et `in_progress`, avertissement pour les autres, erreur quand une écriture de la base a échoué.
- Une ligne par élément, au niveau debug (`analysis follow-up item`) : `item_id`, `outcome`, `reason` (classe de motif de la validation) ou `error_type` (type d'erreur de l'API, borné à `[a-z_]`, 64 caractères au plus, `other` sinon). Pour le repli `storage_failed`, la ligne sort au niveau erreur, avec `error_type=store` et `error`.
- Jamais une réponse, une fiche, un titre, un nom de tag ou de catégorie, un message du validateur ni le message d'erreur de l'API ; un test le vérifie.

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

**Élément répondu** (E3-02, change `apply-batch-results`, novembre 2026) — l'`Item` que renvoient les deux opérations implémentées qui en répondent un, `createItem` (doublon, capture rejouée) et `updateItem`, porte la clé du type de contenu, l'id de la catégorie, les ids des tags non supprimés par ordre croissant et la fiche d'un élément analysé ; avant l'analyse, `content_type` et `category_id` sont `null`, `tag_ids` vaut `[]` et le membre `fiche` est absent. `last_error` est `null` ou un **code d'échec** du schéma `AnalysisError` : `invalid_response` (réponse invalide, RG-35), `request_errored` (requête en erreur chez le service, RG-35), `too_long` (fiche trop longue, RG-36), `storage_failed` (réponse valide que la base a refusé d'écrire, RG-35). Une fiche enregistrée qui ne se décode pas, ou un code hors de cette liste, répond `500` sans rien citer de la fiche.

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

Deux conteneurs : le service Go et un sidecar Tailscale qui le publie en HTTPS sur le réseau privé ([ADR-0015](adr/0015-reseau-tailscale.md), [ADR-0019](adr/0019-deploiement-docker-nas.md)). Le compose de production, [`infra/docker-compose.yml`](../infra/docker-compose.yml), fait référence ; ses invariants, vérifiés en CI par `make check-infra`, sont listés dans [`infra/CLAUDE.md`](../infra/CLAUDE.md). Installation, mise à jour et retour arrière : [runbook NAS](runbook-nas.md).

- Image construite par GitHub Actions pour amd64 et arm64, publiée sur GHCR, **publique** ([ADR-0027](adr/0027-depot-public.md), qui amende ADR-0019) : le NAS la tire sans identifiant. Tags `latest` et `sha-<commit court>` ; le NAS épingle un `sha-…` (`HIPPO_IMAGE_TAG`, obligatoire).
- Mise à jour manuelle et délibérée : une sauvegarde par `hippo backup`, puis `docker compose pull && docker compose up -d` ; les migrations s'appliquent au démarrage.
- Le service écoute sur `127.0.0.1:8080` dans le réseau du sidecar, qui seul le publie (`tailscale serve`) ; aucun port publié. Sidecar en mode noyau (`NET_ADMIN`, `/dev/net/tun`), image Tailscale épinglée ; le service redémarre avec le sidecar, dont il partage le réseau. La clé d'authentification Tailscale ne sert qu'à l'enrôlement (`TS_AUTH_ONCE`).
- Sauvegardes sur le NAS seulement, sans copie externe ([RG-98](metier.md#données-et-multi-utilisateur), [ADR-0039](adr/0039-sauvegardes-sur-le-nas.md), qui remplace la copie externe de l'[ADR-0020](adr/0020-sauvegardes.md)) : chaque nuit à `BACKUP_SCHEDULE` (3 h, heure de Paris) par le service, et à la demande, avant chaque mise à jour, par `docker compose exec hippo /hippo backup`. Copie de la base par `VACUUM INTO` sur une connexion en lecture seule, puis des images, liées en dur à la sauvegarde précédente quand elles n'ont pas changé ; dans le dossier partagé `Sauvegardes/hippo` (`BACKUP_HOST_DIR`, monté sur `/backups`, obligatoire). Conservation : 14 jours, les 3 sauvegardes les plus récentes toujours gardées. Restauration manuelle, par le [runbook NAS](runbook-nas.md#8-revenir-en-arrière). Change `nightly-backup` (E6-03).
- Logs JSON sur la sortie standard, sans secret ni contenu d'élément ; rotation par Docker (`json-file`, taille bornée), pour que les logs ne remplissent pas le volume du NAS.
- Limite mémoire à 384 Mo ([ADR-0038](adr/0038-limite-memoire-384-mo.md), qui amende ADR-0019) : le service, l'enfant d'extraction (48 Mio) et l'enfant de copie d'image (256 Mio) ; pendant `hippo backup`, la commande aussi (limite souple de 32 Mio sur son tas Go).
- `TZ` ne règle que l'horodatage des logs : les horaires du batch et de la sauvegarde, les noms des sauvegardes et toutes les dates du domaine sont à l'heure de Paris.

## Coûts et garde-fous

À 20 éléments par jour, la V1 coûte environ 2 à 4 $ par mois selon la part de contenus techniques ([ADR-0021](adr/0021-garde-fous-couts.md)).

Tarifs de Haiku 4.5 relevés le 7 novembre 2026 : 1 $ / 5 $ par million de tokens (entrée / sortie), moitié prix via l'API Batch. Ils sont en configuration, à vérifier sur la page de tarifs d'Anthropic quand le modèle change ([prix et formule du coût](#tokens-et-coût)).

| Cas | Hypothèse | Coût par élément (batch) |
| --- | --- | --- |
| Astuce, recette, lieu, note | 4 500 tokens entrée, 500 sortie | ~0,0035 $ |
| Article technique | 6 000 tokens entrée, 2 000 sortie | ~0,008 $ |
| Très long contenu | 30 000 tokens entrée, 4 000 sortie | ~0,025 $ |
| Capture d'écran | image + prompt, 800 sortie | ~0,004 $ |
| Analyse immédiate (hors batch) | contenu moyen | ~0,01 $ |

**Garde-fous** : crédits prépayés sans recharge automatique ; budget mensuel applicatif (`MONTHLY_BUDGET_USD`, 5 $) vérifié avant chaque envoi ; plafonds de 200 éléments et 200 Mio par batch, ~120 000 caractères en entrée, 4 000 tokens en sortie. Clé d'API dédiée au projet. Pour le batch de la nuit, les gardes s'appliquent dans cet ordre : clé d'API, batch sans coût enregistré, budget du mois ([détail](#envoi-du-batch-de-la-nuit)). Aucune nouvelle tentative automatique d'un envoi : un envoi à l'issue inconnue est réconcilié plutôt que renvoyé.

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
- **Reels Instagram** : surveiller les outils qui simplifieraient l'extraction d'ici la V1.5.
- **Compte Apple Developer payant** : seulement si d'autres projets iOS le justifient.
