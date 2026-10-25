# Glossaire

Vocabulaire du domaine de Hippo. Chaque terme a **un seul sens** dans tout le projet : la doc, le code, l'API, les tests et les échanges avec les agents. Si un terme manque ou devient ambigu, on le corrige ici d'abord.

Les règles métier qui utilisent ces termes sont dans [`metier.md`](metier.md).

## Conventions de nommage

- **La doc est en français, le code est en anglais.** Chaque terme ci-dessous donne son identifiant de code (tables, champs, types, routes, valeurs d'énumération).
- **Exception : « fiche ».** Le mot est gardé tel quel dans le code (`fiche`, `fiche_json`). C'est le concept central du domaine, et ses traductions possibles (`card`, `sheet`, `summary`) entrent en collision avec d'autres notions : la carte d'une liste dans l'UI, ou un simple résumé.
- Un terme marqué *(V2)* ou *(V1.5)* n'existe pas encore dans le code de la V1 ; il est défini ici pour que personne ne réutilise le mot pour autre chose.

## Termes

### Contenu et capture

| Terme | Code | Définition |
| --- | --- | --- |
| **Élément** | `item` | L'unité de base de Hippo : une chose que l'utilisateur a voulu garder (un lien, une image ou une note), avec tout ce qui s'y rattache (contenu extrait, fiche, classement, état de lecture). |
| **Capture** | `capture` (action), `POST /api/items` | Le geste qui crée un élément. Une capture est instantanée : elle enregistre et rend la main, l'analyse vient plus tard. |
| **Source de capture** | `source` : `share`, `manual`, `rss` *(V2)* | D'où vient l'élément : la feuille de partage iOS (`share`), l'écran Capture de l'appli (`manual`), ou la veille automatique (`rss`, V2). |
| **Forme de l'élément** | `kind` : `link`, `image`, `note` | Ce qui a été capturé, techniquement : une URL, une image, ou un texte libre. À ne pas confondre avec le *type de contenu*. |
| **Note de capture** | `user_note` | Le texte que l'utilisateur ajoute au moment de la capture (« pour le projet X », « à tester »). C'est un indice précieux sur l'intention, transmis à l'IA. Pour un élément de forme `note`, c'est le contenu lui-même. |
| **Élément privé** | `is_private = 1` | Élément stocké et cherchable, mais **jamais envoyé à l'IA**. Décoché par défaut. L'utilisateur le range lui-même. |
| **Image** | `asset` | Fichier image rattaché à un élément (capture d'écran, photo), stocké sur le disque du NAS. |
| **URL normalisée** | `normalized_url` | L'URL débarrassée de ce qui ne change pas le contenu (paramètres de tracking, fragment, slash final). Sert à détecter les doublons. |

### Traitement

| Terme | Code | Définition |
| --- | --- | --- |
| **Extraction** | `extraction` | Récupération du texte utile d'un élément de forme `link` : contenu principal d'une page web, ou transcription d'une vidéo YouTube. Faite juste après la capture, sans IA. |
| **Contenu extrait** | `content_text` | Le texte obtenu par l'extraction, conservé tel quel pour pouvoir réanalyser plus tard sans retélécharger. |
| **Contenu tronqué** | `content_truncated` | Le contenu extrait dépassait la limite (~120 000 caractères) et a été coupé. |
| **Contenu pauvre** | `content_ok = false` | L'IA a jugé le contenu insuffisant pour produire une vraie fiche (paywall, page vide, reel sans légende). La fiche se limite à ce qui est connu. |
| **Analyse** | `analysis` | Le traitement d'un élément par l'IA : choix du type, de la catégorie, des tags, et rédaction de la fiche. |
| **Batch** (lot nocturne) | `batch` | L'envoi groupé, une fois par nuit, de tous les éléments en attente d'analyse, via l'API Message Batches d'Anthropic (moitié prix). |
| **Analyse immédiate** | `POST /api/items/{id}/analyze` | Analyse d'un seul élément tout de suite, hors batch, au plein tarif. Déclenchée à la main. |
| **Réanalyse** | `reanalysis` | Nouvelle analyse d'un élément déjà analysé, à partir du contenu extrait conservé (meilleur prompt, nouveau schéma, fiche incomplète). |
| **Statut de traitement** | `status` : `queued`, `extracted`, `submitted`, `analyzed`, `failed` | Où en est l'élément dans le pipeline. Indépendant du *statut de lecture*. Voir le cycle de vie dans [`metier.md`](metier.md#4-cycle-de-vie-dun-élément). |
| **Fiche incomplète** | `fiche_incomplete` | L'IA a atteint son plafond de tokens de sortie avant de finir la fiche. |
| **Version de prompt** | `prompt_version` | Numéro de la version du prompt système qui a produit la fiche. |
| **Version de schéma** | `schema_version` | Numéro de la version du schéma JSON de la fiche pour son type. Permet de repérer les fiches à réanalyser quand un template évolue. |

### Fiche et classement

| Terme | Code | Définition |
| --- | --- | --- |
| **Fiche** | `fiche`, `fiche_json` | Le contenu structuré produit par l'IA pour un élément, au format de son type de contenu. C'est la **source de vérité** de l'affichage. Non éditable par l'utilisateur. |
| **Type de contenu** | `content_type`, clé `key` | La nature du contenu, qui détermine la structure de la fiche et son template d'affichage. 7 types de départ : `tech_article`, `tutorial`, `tip`, `recipe`, `place`, `resource`, `note`. |
| **Template** | `template` | Composant de l'appli qui affiche une fiche selon son type. Un type sans template dédié utilise le **template générique** (`generic`). |
| **Catégorie** | `category` | Le rangement principal d'un élément, dans une arborescence de 2 niveaux maximum (ex. *Voyages › Lille*). **Une seule catégorie par élément**, celle de son sujet principal. |
| **Catégorie racine / sous-catégorie** | `parent_id = null` / `parent_id ≠ null` | Premier et second niveau de l'arborescence. |
| **Tag** | `tag` | Étiquette libre et transversale (2 à 5 par élément), en minuscules, mots séparés par des tirets, sans accent (ex. `spring-boot`). Porte ce que la catégorie ne dit pas. |
| **Proposition** | `status = proposed` | Catégorie ou type créé par l'IA parce que rien d'existant ne convenait. Utilisable tout de suite, en attente de validation lors de la revue. |
| **Catégorie / type actif** | `status = active` | Catégorie ou type validé (créé par l'utilisateur, ou proposition acceptée). |
| **Revue** | `review` | Le passage, en général le matin, sur ce que l'IA a rangé depuis la dernière revue : on corrige ce qui est faux et on traite les propositions (accepter, renommer, fusionner). Jamais bloquante. |
| **Fusion** | `merge` | Opération qui verse tous les éléments d'une catégorie (ou d'un tag) dans une autre, puis supprime la première. |
| **Correction manuelle** | `user_edited` | Modification par l'utilisateur du type, de la catégorie ou des tags d'un élément. Une correction n'est **jamais écrasée** par une réanalyse. |
| **À trier** | `category_id IS NULL` | Vue des éléments sans catégorie : éléments privés pas encore rangés, éléments en échec. |
| **Fraîcheur** | `freshness` : `dated`, `timeless` | Indique si le contenu vieillit (actualité, version d'un outil) ou non (concept, recette). Servira au badge « peut-être obsolète » (V3). |

### Lecture et organisation

| Terme | Code | Définition |
| --- | --- | --- |
| **Statut de lecture** | `read_status` : `to_read`, `in_progress`, `read` | « À lire » : jamais ouverte. « En cours » : ouverte mais pas finie. « Lu » : lue jusqu'au bout ou marquée lue. |
| **Position de lecture** | `read_progress` (0 à 1) | Jusqu'où l'utilisateur a lu la fiche, pour reprendre au bon endroit. |
| **Épingle** | `is_pinned` | Marqueur « à ne pas oublier ». Remplace la notion de favori : il n'y a pas de favori distinct. |
| **Annotations** | `annotations` | Texte libre de l'utilisateur sur une fiche, à côté du contenu généré. Jamais touché par l'IA. |
| **Bibliothèque** | écran `Library` | L'écran d'accueil de l'appli : tous les éléments, avec recherche et filtres. |
| **Recherche plein texte** | FTS5, paramètre `q` | Recherche par mots-clés sur le titre, la fiche, la note et les tags, insensible aux accents. |
| **Recherche sémantique** *(V1.5)* | `embedding` | Recherche par le sens, grâce à des vecteurs calculés localement sur le NAS. |
| **Éléments liés** *(V1.5)* | `related_items` | Éléments qui parlent du même sujet, calculés à partir des tags, des catégories et des vecteurs. |
| **Export** | `GET /api/export` | Archive Markdown de toutes les fiches, un fichier par fiche, rangés par catégorie, compatible Obsidian. |

### Coûts et exploitation

| Terme | Code | Définition |
| --- | --- | --- |
| **Budget mensuel** | `MONTHLY_BUDGET_USD` | Plafond de dépense IA par mois (5 $ par défaut). Une fois atteint, plus aucune analyse jusqu'au mois suivant. |
| **Coût d'un batch** | `batches.cost_usd` | Coût calculé à partir des tokens réellement consommés. |
| **NAS** | — | Le serveur domestique (Ugreen DXP4800 Plus) qui héberge le backend et les données. |
| **Tailscale** | — | Réseau privé qui relie l'iPhone et le NAS sans ouvrir de port sur la box. |
| **Token d'API** | `API_TOKEN` | Secret partagé entre l'appli et le backend, envoyé dans `Authorization: Bearer`. |

### Veille *(V2)*

| Terme | Code | Définition |
| --- | --- | --- |
| **Sujet suivi** | `topic` | Un centre d'intérêt déclaré en langage naturel (« Java et JVM », « DevOps, niveau débutant »), auquel sont rattachées des sources. |
| **Source de veille** | `feed` | Un flux suivi automatiquement : RSS, blog officiel, releases GitHub, Hacker News filtré. |
| **Digest** | `digest` | La sélection du matin : un élément à la une puis les autres par pertinence. Aucun quota : un article n'y entre que s'il dépasse le seuil de pertinence. |
| **L'essentiel du jour** | `daily_brief` | Le bloc d'actualité générale du digest : 5 à 10 points factuels, chacun lié à ses sources. |
| **Pertinence** | `relevance` | À quel point un contenu compte pour l'utilisateur, d'après ses sujets et ses habitudes. |
| **Importance** | `importance` : `info`, `notable`, `important`, `major` | À quel point une actualité compte pour tout le monde. « Majeur » reste rare. Ne s'applique qu'aux actualités. |
| **Groupe** | `story` | Plusieurs articles qui parlent du même événement, réunis en une seule fiche qui cite toutes les sources. |
| **Trace de lecture** | `digest_trace` | Ce qu'on garde d'un article du digest non sauvegardé, une fois son contenu supprimé (30 jours) : le lien, le sujet, ouvert ou ignoré. |

## Mots à éviter

| Ne pas dire | Dire | Pourquoi |
| --- | --- | --- |
| article, bookmark, post | **élément** | Un élément n'est pas toujours un article : ce peut être une recette, une photo ou une note. |
| résumé | **fiche** | Une fiche n'est pas qu'un résumé : une recette ou un tuto gardent toutes leurs étapes. |
| favori | **épingle** | Une seule notion pour « à ne pas oublier ». |
| dossier | **catégorie** | Évite de penser à un système de fichiers. |
| catégorie (pour parler du format) | **type de contenu** | La catégorie dit *de quoi* ça parle, le type dit *quelle forme* ça a. |
| traiter, process | **extraire** ou **analyser** | Les deux étapes sont distinctes : l'extraction est locale et gratuite, l'analyse passe par l'IA et coûte. |
