# Glossaire

Vocabulaire du domaine de Hippo. Chaque terme a **un seul sens** dans tout le projet : la doc, le code, l'API, les tests et les échanges avec les agents. Si un terme manque ou devient ambigu, on le corrige ici d'abord.

Les règles métier qui utilisent ces termes sont dans [`metier.md`](metier.md).

## Conventions de nommage

- **La doc est en français, le code est en anglais.** Chaque terme ci-dessous donne son identifiant de code (tables, champs, types, routes, valeurs d'énumération).
- **Exception : « fiche ».** Le mot est gardé tel quel dans le code (`fiche`, `fiche_json`). C'est le concept central du domaine, et ses traductions possibles (`card`, `sheet`, `summary`) entrent en collision avec d'autres notions : la carte d'une liste dans l'UI, ou un simple résumé.
- **Même exception pour « référentiel ».** Le code garde `referentiel` : c'est le nom du bloc `<referentiel>` du prompt, partie du contrat avec l'IA (décidé le 6 novembre 2026, change `submit-nightly-batch`).
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
| **Identifiant de capture** | `client_id`, table `capture_ids` | Identifiant (ULID) choisi par l'appareil qui envoie une capture. Une capture rejouée avec le même identifiant renvoie l'élément déjà créé au lieu d'en créer un second (RG-08). Chaque identifiant reçu est enregistré dans `capture_ids` avec l'élément auquel la capture a abouti, y compris quand elle a été fusionnée dans un doublon. |
| **URL normalisée** | `normalized_url` | L'URL débarrassée de ce qui ne change pas le contenu (paramètres de tracking, fragment, slash final). Sert à détecter les doublons. |

### Traitement

| Terme | Code | Définition |
| --- | --- | --- |
| **Extraction** | `extraction` | Récupération du texte utile d'un élément de forme `link` : contenu principal d'une page web, texte d'un PDF, ou métadonnées d'une vidéo YouTube (titre, chaîne, durée, description ; pas de transcription en V1, RG-22). Faite juste après la capture, sans IA. |
| **Contenu extrait** | `content_text` | Le texte obtenu par l'extraction, conservé tel quel pour pouvoir réanalyser plus tard sans retélécharger. |
| **Mode d'extraction** | `extraction_mode` : `full`, `fallback`, `none` | Comment l'extraction a abouti : contenu principal lu (`full`), **repli** (`fallback`), ou rien à extraire (`none`, une note ou une image seulement ; jamais un lien). |
| **Repli** | `fallback` | Extraction d'un lien qui n'a pas pu lire le contenu principal : on garde ce qu'on a pu obtenir (`og:title`, `og:description`) et la note de capture. Une vidéo YouTube est toujours en repli, faute de transcription : elle garde sa description comme contenu et sa durée comme temps de lecture (RG-22). L'élément part quand même en analyse (RG-23). |
| **Tentative d'extraction** | table `extraction_attempts` | Ligne écrite avant le téléchargement d'un lien et effacée avec son résultat. Une ligne dont `started_at` est renseigné signale une tentative interrompue (crash) : le lien passe en repli sans être retéléchargé. |
| **Format de la réponse** | `format` : `html`, `pdf`, `other`, `youtube` | Ce qu'un lien a renvoyé, décidé sur le type déclaré et les premiers octets avant de lire le corps : une page HTML, lue par Readability ; un **PDF** (`application/pdf`, ou un corps qui commence par `%PDF-`), lu par le lecteur de PDF (RG-28) ; autre chose, en repli. `youtube` n'est pas un type de média mais un lecteur : la page d'une vidéo YouTube, lue pour les données de son lecteur (RG-22). |
| **Fichier temporaire** | `TempDir`, `TempDir.NewFile()`, dossier `DATA_DIR/tmp` | Fichier sans nom de `DATA_DIR/tmp` : le PDF téléchargé avant sa lecture (extraction), les requêtes et le corps d'un batch, la copie d'une image (passe d'analyse). Son nom est supprimé dès sa création, avant toute donnée : il disparaît avec son dernier descripteur ouvert, crash compris. Rien ne reste sur le NAS ni dans les sauvegardes. À ne pas confondre avec les fichiers `.tmp-*` des images. |
| **Classe de raison** | `reason` (logs) | Pourquoi une extraction a donné son mode, ou pourquoi l'élément reste en file : `ok`, `timeout`, `status`, `not_html`, `too_large`, `blocked`, `short`, `unreachable`, `reading_failed`, `crashed_before` ; pour un PDF, aussi `encrypted` (mot de passe ou chiffrement non lu) et `damaged` (PDF abîmé) ; pour une vidéo YouTube, aussi `oembed` (page sans métadonnées, titre donné par oEmbed) et `no_metadata` (page répondue sans données du lecteur utilisables, ou redirigée hors de YouTube, oEmbed en échec aussi) ; élément laissé en file : `network`, `reader_unavailable`, `storage_unavailable` (le NAS ne peut pas écrire le fichier temporaire). Jamais en base. |
| **Contenu tronqué** | `content_truncated` | Le contenu extrait dépassait la limite (~120 000 caractères) et a été coupé. |
| **Contenu pauvre** | `content_ok = false` | L'IA a jugé le contenu insuffisant pour produire une vraie fiche (paywall, page vide, reel sans légende). La fiche se limite à ce qui est connu. |
| **Analyse** | `analysis` | Le traitement d'un élément par l'IA : choix du type, de la catégorie, des tags, et rédaction de la fiche. |
| **Référentiel** | `referentiel` | Le bloc `<referentiel>` du prompt système : les types de contenu, les catégories et les tags de l'utilisateur, tels que l'IA les voit (format dans [`prompt/system-prompt.md`](prompt/system-prompt.md)). Construit une fois par batch, identique dans chaque requête. |
| **Copie de l'image** | `imagecopy`, processus enfant `hippo copy-image` | Le JPEG envoyé à l'IA à la place d'une image : redressé, 1 568 px au plus sur son grand côté, sans aucune métadonnée (RG-38). Le fichier stocké ne change jamais. |
| **Batch** (lot nocturne) | `batch` | L'envoi groupé, une fois par nuit, de tous les éléments en attente d'analyse, via l'API Message Batches d'Anthropic (moitié prix). |
| **Passe d'analyse** | `analysis.Pass` | Le traitement de nuit, une fois par date de Paris (RG-30) : pour chaque utilisateur, il réconcilie ses batchs `pending`, fait le **suivi** de ses batchs en cours, vérifie les gardes (clé d'API, batch sans coût, budget), sélectionne les éléments en attente, construit leurs requêtes et envoie un batch. |
| **Statut d'un batch** | `batches.status` : `pending`, `in_progress`, `ended`, `failed` | `pending` : éléments passés à `submitted`, envoi pas encore confirmé. `in_progress` : accepté par Anthropic. `ended` : terminé, résultats appliqués ou **indisponibles**, coût enregistré (E3-02). `failed` : refusé, jamais arrivé, ou annulé avant l'envoi ; ses éléments sont revenus à `extracted`. |
| **Réconciliation** | `reconciliation`, `Pass.Reconcile` | Rapprochement d'un batch `pending`, dont l'envoi n'a pas eu de réponse claire, avec les batchs qu'Anthropic détient pour la clé (date de création proche, même nombre de requêtes). Un seul candidat : il est adopté. Aucun ou plusieurs : le batch passe en `failed` et ses éléments repartent la nuit suivante (double envoi possible, logué). Faite au démarrage et avant la passe de chaque utilisateur. |
| **Suivi du batch** | `follow-up`, `Pass.Follow` | Demander à Anthropic l'état des batchs en cours d'un utilisateur et, une fois un batch terminé, en lire et appliquer les **résultats**. Fait au démarrage, toutes les 15 minutes et avant les gardes de la passe de nuit ([spec](spec-v1.md#suivi-du-batch-et-résultats)). |
| **Résultat** | `result`, `analysis.Result` | Une ligne du fichier des résultats d'un batch : l'issue d'une requête (`succeeded`, `errored`, `expired`, `canceled`), avec son `custom_id`, l'id de l'élément. |
| **Ligne illisible** | `unreadable line`, `ErrUnreadableLine` | Ligne du fichier des résultats qui n'est pas un résultat JSON lisible ou dépasse 4 Mio : sautée et comptée, la lecture continue ; son élément reste sans résultat et revient dans la file sans tentative (RG-35). |
| **Résultats indisponibles** | `batches.results_unavailable_at`, `ErrResultsUnavailable` | Anthropic ne sert plus les résultats d'un batch (inconnu, archivé) : le batch est fermé avec les tokens déjà lus, zéro sinon (RG-80), et ses éléments sans résultat reviennent dans la file sans tentative (RG-35). |
| **Code d'échec** | `last_error`, `AnalysisError` | Pourquoi la dernière tentative d'analyse a échoué : `invalid_response` (réponse invalide, RG-35), `request_errored` (requête en erreur chez le service, RG-35), `too_long` (fiche trop longue, RG-36) ou `storage_failed` (réponse valide que la base a refusé d'écrire, RG-35). Jamais un message de l'API ni du validateur. |
| **Analyse immédiate** | `POST /api/items/{id}/analyze` | Analyse d'un seul élément tout de suite, hors batch, au plein tarif. Déclenchée à la main. |
| **Réanalyse** | `reanalysis` | Nouvelle analyse d'un élément déjà analysé, à partir du contenu extrait conservé (meilleur prompt, nouveau schéma, type proposé fusionné dans un type existant, ou à la demande). |
| **Statut de traitement** | `status` : `queued`, `extracted`, `submitted`, `analyzed`, `failed` | Où en est l'élément dans le pipeline. Indépendant du *statut de lecture*. Voir le cycle de vie dans [`metier.md`](metier.md#4-cycle-de-vie-dun-élément). |
| **Fiche incomplète** | `stop_reason = max_tokens` | Réponse de l'IA coupée par son plafond de tokens de sortie avant la fin de la fiche. Elle n'est jamais enregistrée : l'élément est retenté une fois avec un plafond doublé, puis passe en échec (« fiche trop longue ») (RG-36). |
| **Version de prompt** | `prompt_version` | Numéro de la version du prompt système qui a produit la fiche. |
| **Version de schéma** | `schema_version` | Numéro de la version du schéma JSON de la fiche pour son type. Permet de repérer les fiches à réanalyser quand un template évolue. |
| **Version de l'enveloppe** | `envelope_version` | `x-schema-version` de [`classify-item.schema.json`](prompt/schemas/classify-item.schema.json), l'enveloppe de la réponse, lu dans le schéma assemblé embarqué ; enregistré avec chaque batch, puis recopié sur l'élément avec sa fiche (RG-64). |

### Fiche et classement

| Terme | Code | Définition |
| --- | --- | --- |
| **Fiche** | `fiche`, `fiche_json` | Le contenu structuré produit par l'IA pour un élément, au format de son type de contenu. C'est la **source de vérité** de l'affichage. Non éditable par l'utilisateur. |
| **Type de contenu** | `content_type`, clé `key` | La nature du contenu, qui détermine la structure de la fiche et son template d'affichage. 7 types de départ : `tech_article`, `tutorial`, `tip`, `recipe`, `place`, `resource`, `note`. |
| **Template** | `template` | Composant de l'appli qui affiche une fiche selon son type. Un type sans template dédié utilise le **template générique** (`generic`). |
| **Catégorie** | `category` | Le rangement principal d'un élément, dans une arborescence de 2 niveaux maximum (ex. *Voyages › Lille*). **Une seule catégorie par élément**, celle de son sujet principal. |
| **Catégorie racine / sous-catégorie** | `parent_id = null` / `parent_id ≠ null` | Premier et second niveau de l'arborescence. |
| **Tag** | `tag` | Étiquette libre et transversale (2 à 5 par élément), en minuscules, mots séparés par des tirets, sans accent (ex. `spring-boot`). Porte ce que la catégorie ne dit pas. |
| **Nom normalisé** | `categories.name_key`, `naming.CategoryKey` | Le nom d'une catégorie en minuscules, sans accent, espaces intérieurs réduits à un seul, ceux des bords retirés : « Ménage » et « menage » ont le même. Sert à réutiliser une catégorie existante plutôt que d'en proposer une seconde sous le même parent (RG-54). À ne pas confondre avec la normalisation d'un tag (`naming.Tag`, RG-58). |
| **Texte de recherche** | `search_text` | La fiche aplatie en texte (chaque chaîne, une par ligne), lue par la recherche plein texte ([règle](spec-v1.md#écriture-dune-réponse-valide), [ADR-0007](adr/0007-fiche-json-source-de-verite.md)). |
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
| **Jeton de synchronisation** | `sync_token` | Valeur opaque, donnée par le serveur à la fin d'une synchronisation, que l'appli renvoie la fois suivante pour ne recevoir que les changements depuis. Trop ancien (plus de 30 jours), il est refusé et l'appli resynchronise tout. |
| **Séquence de modification** | `change_seq`, table `sync_sequence` | Compteur qui augmente à chaque écriture d'une ligne synchronisée ; c'est lui, et non l'heure, qui ordonne les changements envoyés à l'appli. |
| **Marque de suppression** | `deleted_at`, *tombstone* | Trace d'un élément, d'une catégorie, d'un type ou d'un tag supprimé, gardée 30 jours pour la synchronisation : seulement son identifiant et sa date de suppression (RG-79). |
| **Export** | `GET /api/export` | Archive Markdown de toutes les fiches, un fichier par fiche, rangés par catégorie, compatible Obsidian. |

### Coûts et exploitation

| Terme | Code | Définition |
| --- | --- | --- |
| **Budget mensuel** | `MONTHLY_BUDGET_USD` | Plafond de dépense IA par mois (5 $ par défaut). Une fois atteint, plus aucune analyse jusqu'au mois suivant. |
| **Coût d'un batch** | `batches.cost_usd` | Coût calculé à partir des tokens réellement consommés, au tarif batch ([formule](spec-v1.md#tokens-et-coût)). |
| **Déclencheur** | `trigger` : `nightly`, `command` | Ce qui a lancé une sauvegarde : la planification du service (`BACKUP_SCHEDULE`), ou la commande `hippo backup`. |
| **Dossier des sauvegardes** | `BACKUP_DIR` (backend, `/backups` dans l'image), `BACKUP_HOST_DIR` (Compose : le dossier de l'hôte monté sur `BACKUP_DIR`, `Sauvegardes/hippo` sur le NAS, `./backups` en local) | Le dossier qui contient les sauvegardes, le fichier de verrou `.lock` et, pendant une sauvegarde, son dossier partiel. Ici « dossier » désigne un dossier du système de fichiers : la règle des « Mots à éviter » (jamais « dossier » pour une catégorie) porte sur le rangement des éléments, pas sur lui. |
| **Dossier partiel** | `.partial-<nom>` | Le dossier où une sauvegarde s'écrit avant d'être renommée sous son nom. Un dossier partiel trouvé au début d'une sauvegarde a été laissé par un crash : il est supprimé. |
| **Manifeste** | `manifest.json` | Le petit fichier JSON d'une sauvegarde : versions du backend et de migration, nombres, tailles. Aucun nom de fichier, aucun contenu d'élément. |
| **NAS** | — | Le serveur domestique (Ugreen DXP4800 Plus) qui héberge le backend et les données. |
| **Nom d'une sauvegarde** | format `YYYY-MM-DDTHHMMSS±hhmm` | L'heure de Paris à laquelle la sauvegarde a commencé, avec son décalage (`2026-11-13T030000+0100`). La conservation ne supprime jamais que des dossiers portant un tel nom. |
| **Restauration** | — | La procédure manuelle de [`runbook-nas.md`](runbook-nas.md) qui remet une sauvegarde à la place du dossier de données. Hippo n'a pas de commande de restauration ([ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)). |
| **Sauvegarde** | `backup`, paquet `internal/backup` | Un dossier daté du dossier des sauvegardes : une copie de la base, des images et un manifeste, faite chaque nuit ou par `hippo backup` ([RG-98](metier.md#données-et-multi-utilisateur), [ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)). À ne pas confondre avec « sauvegardé », employé par le digest *(V2)* pour un article que l'utilisateur garde (`digest_trace`) : une sauvegarde n'est jamais un geste de l'utilisateur. |
| **Tailscale** | — | Réseau privé qui relie l'iPhone et le NAS sans ouvrir de port sur la box. |
| **Token d'API** | `API_TOKEN` | Secret partagé entre l'appli et le backend, envoyé dans `Authorization: Bearer`. |
| **Version de migration** | `migration_version` | La dernière migration goose appliquée à une base, notée dans le manifeste d'une sauvegarde. À ne pas confondre avec la *version de schéma* (`schema_version`), qui est celle du schéma JSON d'une fiche (RG-64). |

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
