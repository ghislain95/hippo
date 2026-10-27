# Backlog

Le travail de la V1, découpé en epics et en user stories, **dans l'ordre de réalisation**. Les versions suivantes sont listées à la fin, sans détail.

- Chaque story cite les règles métier (`RG-xx`, voir [`metier.md`](metier.md)) et les ADR qu'elle met en œuvre. En cas de doute, ce sont eux qui font foi.
- Une story est **terminée** quand ses critères d'acceptation sont couverts par des tests automatisés (sauf mention contraire), que la CI passe et que la doc concernée est à jour.
- Priorités : **M** (indispensable pour la V1), **S** (à faire en V1 si possible), **C** (bonus, peut glisser).
- Les stories deviendront des changements OpenSpec au moment de leur réalisation.

## Vue d'ensemble

| Epic | Objectif | Dépend de |
| --- | --- | --- |
| [E0](#e0--fondations) | Projet prêt pour le développement avec des agents | — |
| [E1](#e1--prototype-ios-jetable) | Lever le risque iOS (extension de partage, SideStore, Tailscale) — ✅ réussi (27 octobre 2026) | E0 |
| [E2](#e2--capture-et-extraction) | Capturer et extraire, côté backend | E0 |
| [E3](#e3--analyse) | Analyser la nuit, avec Haiku, sous contrôle des coûts | E2 |
| [E4](#e4--bibliothèque-revue-et-recherche-api) | Lire, chercher, corriger, réviser, exporter (API) | E3 |
| [E5](#e5--appli-mobile) | L'appli iPhone et son extension de partage | E1, E4 |
| [E6](#e6--mise-en-production) | Faire tourner Hippo sur le NAS, sauvegardé | E2 |

E2 à E4 peuvent commencer en parallèle de E1 : ils n'en dépendent pas. Mais **si E1 échoue, on revoit les ADR 0012 à 0014 avant d'avancer sur E5**. E1 a réussi : ADR-0013 et ADR-0014 sont confirmés, et l'extension sera en Swift ([ADR-0036](adr/0036-extension-de-partage-swift.md)).

---

## E0 — Fondations

Fait en séance sur le Mac, en reprenant les pratiques de geomni et wifisense. Le setup (branche `chore/setup`, 25 octobre 2026) est décrit dans [`dev/setup-report.md`](dev/setup-report.md).

| ID | Story | Prio | Statut |
| --- | --- | --- | --- |
| E0-01 | Créer le monorepo (`backend/`, `app/`, `api/`, `infra/`, `docs/`) et y déposer cette documentation | M | ✅ setup |
| E0-02 | Mettre en place le contexte des agents : `CLAUDE.md` racine avec table de routage, un `CLAUDE.md` par dossier, agents, skills et règles repris et adaptés de l'autre projet | M | ✅ setup |
| E0-03 | Écrire la stratégie de test (back, front, prompt) en s'inspirant de celle de l'autre projet | M | ✅ setup ([`dev/testing.md`](dev/testing.md)) |
| E0-04 | Initialiser OpenSpec | M | ✅ setup |
| E0-05 | Écrire le contrat complet `api/openapi.yaml` des routes de la [spec](spec-v1.md#api-rest) : **premier change OpenSpec**, cadré avec Ghislain. Le squelette du contrat (sécurité Bearer, problem+json, pagination par curseur, `/healthz`) et la génération du serveur Go et du client TypeScript, avec la vérification « code généré à jour », sont en place depuis le setup | M | ✅ `api-contract-v1` (25 octobre 2026) |
| E0-06 | CI GitHub Actions : lint, tests back et front, vérification du code généré, build de l'image Docker (filtres par chemin) | M | ✅ setup |
| E0-07 | Commandes d'agent `/eval-prompt` et `/new-content-type` | S | ✅ setup (`/eval-prompt` attend le harnais d'E3-09) |

**Critères d'acceptation communs**
- Un agent qui ouvre le repo trouve, depuis `CLAUDE.md`, le glossaire, le document métier, les ADR et la backlog.
- La CI tourne sur une pull request et échoue si le code généré depuis l'OpenAPI n'est pas à jour.

---

## E1 — Prototype iOS jetable

Objectif unique : **savoir si la chaîne iOS fonctionne avec un compte Apple gratuit**. Le code est jeté ensuite.

✅ Terminé le 27 octobre 2026 par le change `ios-prototype` : [résultats](../openspec/changes/archive/2026-10-27-ios-prototype/results.md), [runbook iOS](runbook-ios.md), [ADR-0036](adr/0036-extension-de-partage-swift.md). Les quatre stories ci-dessous sont faites.

### E1-01 — Partager une URL vers un serveur de test · M

> En tant que Ghislain, je veux partager un lien depuis Safari et Instagram vers une appli de test, afin de vérifier que l'extension de partage fonctionne avec un compte gratuit.

- Une appli Expo minimale (development build) avec une extension de partage est signée avec un compte Apple gratuit et installée sur l'iPhone.
- Depuis Safari **et** depuis Instagram, l'extension apparaît dans la feuille de partage et envoie l'URL (et une note) à un petit serveur sur le Mac.
- L'URL du serveur et un token sont injectés au build ; aucun App Group n'est utilisé (ADR-0014).
- L'extension accepte aussi une image (capture d'écran) et l'envoie.

### E1-02 — Installer et rafraîchir avec SideStore · M

> En tant que Ghislain, je veux installer et rafraîchir l'appli depuis mon iPhone, afin de ne pas dépendre de mon Mac chaque semaine.

- L'appli (avec son extension) est installée via SideStore.
- Le rafraîchissement depuis l'iPhone fonctionne ; la procédure exacte, y compris la coupure de Tailscale, est notée dans `docs/runbook-ios.md`.

### E1-03 — Joindre le serveur via Tailscale en HTTPS · M

- Le serveur de test est publié par `tailscale serve` en HTTPS ; l'extension l'atteint en 4G sans exception ATS.

### E1-04 — Décision · M

- Résultat consigné dans l'ADR-0014 (statut confirmé) ou nouvel ADR si une alternative est retenue (extension Swift, compte payant, SwiftUI).

---

## E2 — Capture et extraction

### E2-01 — Capturer un lien · M

> En tant que Ghislain, je veux envoyer un lien au backend et recevoir une confirmation immédiate, afin de capturer sans attendre.

RG-01, RG-02, RG-03, RG-07, RG-08 · ADR-0003, ADR-0004, ADR-0016, ADR-0022, ADR-0035
- `POST /api/items` avec `{url, user_note?, is_private?, client_id?}` crée un élément `queued`, `to_read`, et répond `201` avec l'élément en moins de 200 ms (hors réseau).
- Une URL et une note dans la même capture : `400 ambiguous-capture` (RG-07).
- Une capture rejouée avec un `client_id` déjà utilisé renvoie `200` avec l'élément existant (RG-08).
- Une URL invalide renvoie `400` au format problem+json.
- Sans token ou avec un mauvais token : `401`. `/healthz` répond sans token.
- L'élément porte le `user_id` de l'utilisateur authentifié.

### E2-02 — Dédoublonner les liens · M

RG-04, RG-05
- Les URL sont normalisées selon RG-05 ; l'URL d'origine est conservée.
- Capturer une URL déjà présente renvoie `200` avec l'élément existant ; aucune ligne n'est créée.
- Si la nouvelle capture a une note, elle est ajoutée à la note existante, datée.
- Tests de normalisation : paramètres `utm_*`, `fbclid`, `igsh`, `si`, fragment, slash final, casse de l'hôte, ordre des paramètres.

### E2-03 — Capturer une note · M

RG-01, RG-02, RG-07
- `POST /api/items` avec `{note}` crée un élément de forme `note` ; le texte vide est refusé (`empty-note`).
- Une note accompagnée d'une note de capture est refusée (`400 ambiguous-capture`, RG-07).

### E2-04 — Capturer une image · M

RG-01, RG-02, RG-08 · ADR-0004, ADR-0035
- `POST /api/items` en multipart avec une image JPEG, PNG, HEIC ou WebP de 20 Mo maximum crée un élément `image` et une ligne `assets`.
- Une image rejouée avec un `client_id` déjà utilisé renvoie `200` avec l'élément existant, sans nouvelle image (RG-08).
- L'image est stockée sur disque dans le volume de données ; `GET /api/assets/{id}` la renvoie (authentifié).
- Format ou taille refusés : `400 unsupported-image` ou `413 image-too-large`.

### E2-05 — Extraire une page web · M

RG-20, RG-21, RG-23, RG-24, RG-25 · ADR-0008
- Après la capture d'un lien, l'extraction s'exécute en arrière-plan ; l'élément passe à `extracted`.
- Le titre, le site et le contenu principal sont extraits avec go-readability ; le temps de lecture est calculé.
- Échec (timeout 15 s, statut HTTP d'erreur, contenu vide) : repli sur `og:title`, `og:description` ; l'élément passe quand même à `extracted`.
- Au-delà de ~120 000 caractères, le contenu est coupé et `content_truncated = 1`.
- Tests sur des pages HTML enregistrées (pas d'accès réseau dans les tests).

### E2-06 — Extraire une vidéo YouTube · S

RG-22
- Pour une URL YouTube, titre, chaîne, durée et transcription (si disponible, langue d'origine) sont extraits ; le temps de lecture est la durée.
- Sans transcription : repli sur titre et description.

### E2-07 — Marquer un élément privé · M

RG-10 à RG-14 · ADR-0023
- `is_private` est enregistré à la capture et modifiable ensuite.
- Un élément privé n'est jamais sélectionné pour l'analyse (vérifié dans E3-01 et E3-06).
- Décocher Privé remet l'élément dans la file.

---

## E3 — Analyse

### E3-01 — Construire et envoyer le batch de la nuit · M

RG-12, RG-30, RG-31, RG-32 · ADR-0005, ADR-0006
- À l'heure configurée (`BATCH_SCHEDULE`), les éléments `extracted`, non privés, sont envoyés en un batch Message Batches (une requête par élément, `custom_id` = id).
- Aucun élément : aucun appel.
- Au-delà de 200 éléments, les plus anciens partent, les autres attendent.
- Chaque requête suit [`prompt/README.md`](prompt/README.md) : partie fixe, référentiel, outil forcé, plafond de sortie, cache.
- **Test obligatoire** : un élément privé n'apparaît dans aucune requête, quel que soit son statut.
- Les éléments envoyés passent à `submitted` ; le batch est enregistré dans `batches`.

### E3-02 — Suivre le batch et appliquer les résultats · M

RG-34, RG-35 · ADR-0006, ADR-0007
- Le batch est interrogé toutes les 15 minutes jusqu'à sa fin.
- Chaque résultat est validé selon [`prompt/README.md`](prompt/README.md#validation-dune-réponse) puis écrit : type, catégorie, tags, titre, fiche, langue, fraîcheur, `content_ok`, modèle, versions, `search_text`, `analyzed_at`.
- Réponse invalide ou erreur : `attempts + 1`, retour à `extracted` ; à la 3e, `failed` avec `last_error`.
- Les tokens consommés et le coût sont enregistrés pour le batch.
- Un redémarrage du backend pendant un batch reprend le suivi sans perte.

### E3-03 — Créer les propositions de catégories et de types · M

RG-53 à RG-57 · ADR-0009
- Une catégorie proposée est créée en `proposed` et l'élément y est rangé.
- Dédoublonnage : deux éléments du même batch qui proposent « Kubernetes » sous le même parent aboutissent à une seule catégorie.
- Parent invalide : rattachement au parent valide le plus proche, sinon à la racine.
- Un type proposé est créé en `proposed`, sans template ; la fiche est `generic`.
- Aucune opération du pipeline ne peut renommer, déplacer ou supprimer une catégorie, un type ou un tag (vérifié par test).

### E3-04 — Normaliser et rattacher les tags · M

RG-58
- Les tags reçus sont normalisés, dédoublonnés et rattachés ; les tags existants sont réutilisés.

### E3-05 — Retenter une réponse coupée · S

RG-36 (modifiée le 25 octobre 2026)
- `stop_reason = max_tokens` : rien n'est enregistré, l'élément revient à `extracted` pour une seule nouvelle tentative la nuit suivante avec un plafond de 8 000 tokens ; s'il s'agit d'une réanalyse, la fiche précédente reste en place.
- Encore coupée : l'élément passe en `failed` avec la raison « fiche trop longue », le contenu extrait est conservé.

### E3-06 — Analyser un élément immédiatement · M

RG-33, RG-12, RG-81
- `POST /api/items/{id}/analyze` analyse l'élément en appel direct et renvoie l'élément mis à jour.
- Refus `409` si l'élément est privé (`private-item`) ; refus `409` avec le type d'erreur `budget-exceeded` si le budget du mois est atteint. Le message est affichable tel quel dans l'appli.
- Refus `409` tant que l'extraction n'est pas terminée (`not-ready`) et quand l'élément est déjà dans le batch de la nuit (`already-submitted`) (RG-33).

### E3-07 — Relancer et réanalyser · M

RG-35, RG-60 à RG-64
- `POST /api/items/{id}/retry` remet un élément `failed` en file avec `attempts = 0`.
- Une réanalyse ne modifie aucun champ listé dans `user_edited`, ni les annotations, la note, l'épingle ou le statut de lecture ; elle remet `read_progress` à 0.

### E3-08 — Respecter le budget mensuel · M

RG-80 à RG-82 · ADR-0021
- Avant chaque envoi (batch ou immédiat), le coût cumulé du mois est comparé au budget ; s'il est atteint, rien n'est envoyé et l'événement est journalisé.
- Le coût est calculé à partir des tokens réels et des tarifs configurés.

### E3-09 — Jeu de test du prompt · M

[`prompt/eval-set.md`](prompt/eval-set.md)
- Ghislain fournit les contenus ; les cas sont figés dans `backend/testdata/eval/`.
- `/eval-prompt` exécute les contrôles automatiques et produit le rapport.
- Avant la mise en service : aucun échec bloquant.

---

## E4 — Bibliothèque, revue et recherche (API)

### E4-01 — Lister, filtrer et chercher · M

RG-95, RG-96 · ADR-0024
- `GET /api/items` accepte `q`, `type`, `category` (inclut les sous-catégories), `tag`, `status`, `read_status`, `pinned`, `unsorted` (À trier), combinables.
- La recherche ignore casse et accents (« detartrer » trouve « détartrer ») et porte sur titre, fiche, note, annotations et tags.
- Pagination par curseur ; tri par défaut du plus récent au plus ancien, épinglés en tête dans une catégorie.

### E4-02 — Détail d'un élément · M

- `GET /api/items/{id}` renvoie l'élément complet avec sa fiche, sa catégorie (chemin), ses tags, ses images.
- Un élément d'un autre utilisateur renvoie `404`.

### E4-03 — Corriger, épingler, annoter · M

RG-60, RG-77, RG-78
- `PATCH /api/items/{id}` modifie type, catégorie, tags, épingle, annotations, note, privé.
- Une modification de type, catégorie ou tags est ajoutée à `user_edited`.

### E4-04 — Suivre la lecture · M

RG-75, RG-76
- `PUT /api/items/{id}/progress` enregistre `read_progress` et fait évoluer `read_status` selon RG-75 ; « marquer lu / non lu » est possible.

### E4-05 — Supprimer · M

RG-79
- `DELETE /api/items/{id}` supprime l'élément, ses tags associés et ses images sur disque ; une trace `deleted_at` reste pour la synchronisation (ADR-0018).

### E4-06 — Revue · M

RG-70 à RG-73
- `GET /api/review` renvoie les éléments analysés depuis la dernière revue et les catégories et types `proposed`.
- `POST /api/review/done` enregistre la date de revue.

### E4-07 — Gérer les catégories · M

RG-51, RG-59, RG-72, RG-74
- Créer, renommer, déplacer (dans la limite de 2 niveaux), changer la couleur et la description, supprimer une catégorie vide.
- Accepter une proposition (`proposed` → `active`).
- Renommer vers un nom déjà pris sous le même parent : `409 name-taken` (RG-74) ; créer une catégorie sous un nom déjà pris aussi (contrainte d'unicité du modèle de données).
- `POST /api/categories/{id}/merge` déplace tous les éléments (et sous-catégories) vers la cible puis supprime la source ; une fusion dans elle-même ou dans sa propre sous-catégorie est refusée (`invalid-merge-target`), comme une fusion qui créerait un 3e niveau (`category-too-deep`).
- Les catégories de départ ([`metier.md`](metier.md#6-catégories-de-départ)) sont créées au premier démarrage.

### E4-08 — Gérer les types et les tags · S

RG-57, RG-74
- Accepter ou renommer un type proposé ; fusionner un type proposé dans un type existant (les fiches concernées sont remises en file pour être réanalysées au bon format).
- `GET /api/tags` avec compteurs ; renommer et fusionner des tags. Renommer un tag vers un nom déjà pris est refusé (`409 name-taken`, RG-74) ; fusionner un type qui n'est pas proposé aussi (`409 type-not-proposed`).

### E4-09 — Synchronisation pour le hors ligne · M

ADR-0018, ADR-0035
- `GET /api/sync?sync_token=` renvoie, page par page, les éléments, catégories, types et tags créés ou modifiés depuis le jeton, et les suppressions (`{id, deleted_at}`), sans `content_text` ; seule la dernière page porte le nouveau jeton.
- Les changements sont ordonnés par la séquence de modification, pas par l'heure.
- Un jeton de plus de 30 jours renvoie `410 sync-expired` : l'appli resynchronise tout.

### E4-10 — Export Markdown · S

RG-91 · ADR-0007
- `GET /api/export` renvoie une archive zip : un fichier `.md` par élément dans un dossier par catégorie, avec front matter, fiche, annotations, et les images.
- Le générateur couvre les 8 templates ; tests sur une fiche d'exemple par type.

### E4-11 — Statistiques · S

RG-82
- `GET /api/stats` renvoie le nombre d'éléments par statut, la taille de la file, les échecs et le coût du mois.

### E4-12 — Isolation des utilisateurs · M

RG-90 · ADR-0022
- Test d'intégration : avec deux utilisateurs, aucune route ne permet à l'un de lire ou modifier les données de l'autre.

---

## E5 — Appli mobile

Les écrans sont **maquettés et validés** avant d'être codés. Le gros travail d'UI viendra plus tard ; la V1 vise une base propre et agréable.

### E5-01 — Socle de l'appli · M

ADR-0012, ADR-0017
- Navigation (`expo-router`), client d'API généré, gestion du token et de l'URL du serveur injectés au build.
- Thèmes clair et sombre (automatique, forçable), taille du texte respectant le réglage iOS.
- Couleur d'accent par catégorie.

### E5-02 — Extension de partage · M

RG-01 à RG-06, RG-10 · ADR-0014, ADR-0036
- Partager un lien, un texte ou une image ouvre un mini formulaire : aperçu, note facultative, interrupteur Privé décoché, bouton Enregistrer.
- Confirmation visible en moins de 5 secondes avec un bon réseau ; message clair et bouton « Copier le lien » si le serveur est injoignable.
- Extension en Swift et SwiftUI, cible générée par `@bacons/apple-targets` (nouvelle dépendance de l'appli, ADR-0036).
- Points ouverts par le prototype E1, à trancher au cadrage :
  - conventions du code Swift (format, lint, tests) ;
  - format des images : Photos les fournit en JPEG, même une capture d'écran ; garder le JPEG ou demander l'original (RG-02) ;
  - arrondi des cartes : Ghislain préfère environ 16 points ;
  - rafraîchissement automatique de SideStore, pas encore observé au 27 octobre 2026.

### E5-03 — Bibliothèque · M

RG-95, RG-96
- Liste des éléments en cartes (titre, site, type, pastille de catégorie, badge « en attente » tant que non analysé).
- Recherche et filtres combinables ; bandeau « Reprendre la lecture » pour le dernier élément en cours.

### E5-04 — Fiche et templates · M

RG-40 à RG-45, RG-75 à RG-78 · ADR-0011
- Un template par type : code coloré et copiable pour `tech_article` et `tutorial` ; ingrédients à cocher et mode pas à pas écran allumé pour `recipe` ; bouton « Ouvrir dans Plans » pour `place` ; template `generic`.
- Barre de progression, enregistrement et reprise de la position, passage automatique en Lu.
- Mentions visibles : contenu partiel, contenu tronqué ; un élément en échec « fiche trop longue » affiche sa raison et le bouton Relancer.
- Actions : ouvrir l'original, épingler, annoter, modifier type, catégorie et tags, réanalyser, supprimer.
- Tests de rendu sur une fiche d'exemple par template.

### E5-05 — Capture dans l'appli · M

RG-01 à RG-08
- Coller une URL, écrire ou dicter une note, choisir une image dans la photothèque, interrupteur Privé.
- Hors ligne : la capture est mise en file locale et envoyée au retour du réseau, avec un `client_id` généré à la capture, pour qu'un renvoi ne crée jamais de doublon (RG-08).

### E5-06 — Revue · M

RG-70 à RG-73
- Liste des éléments analysés depuis la dernière revue ; propositions mises en évidence ; accepter, renommer ou fusionner en un geste ; corriger un élément.

### E5-07 — Explorer · S

- Arborescence des catégories avec compteurs, liste des types, nuage de tags.

### E5-08 — Réglages · M

- Thème, taille du texte, état du serveur, file d'attente, coût du mois et alerte de budget, échecs avec bouton Relancer, export Markdown.

### E5-09 — Hors ligne · M

RG-97 · ADR-0018
- Cache local synchronisé ; lecture et recherche sans réseau ; écritures rejouées au retour du réseau.

### E5-10 — Animations et retour haptique · C

- Transition fluide de la carte vers la fiche, retour haptique sur les actions principales.

---

## E6 — Mise en production

### E6-01 — Image et compose · M

ADR-0019
- Image distroless publiée sur GHCR par la CI ; `infra/docker-compose.yml` avec le sidecar Tailscale ; `.env.example` documenté.
- Le même compose tourne sur le Mac et sur le NAS.

### E6-02 — HTTPS par Tailscale · M

ADR-0015
- `tailscale serve` publie le backend en HTTPS sur le réseau Tailscale ; aucun port ouvert sur la box.

### E6-03 — Sauvegardes · M

ADR-0020
- Sauvegarde locale nocturne (base et images), 14 jours conservés.
- Sauvegarde externe chiffrée (service à choisir).
- Restauration testée une fois, procédure dans `docs/runbook-nas.md`.

### E6-04 — Exploitation · S

- Logs JSON lisibles depuis l'interface Docker du NAS ; aucun secret ni contenu d'élément dans les logs.
- Procédure de mise à jour et de retour arrière dans `docs/runbook-nas.md`.

---

## Après la V1

Sans détail pour l'instant ; chaque ligne deviendra un epic le moment venu.

| Version | Epic |
| --- | --- |
| V1.5 | Recherche sémantique (embeddings locaux) et recherche hybride |
| V1.5 | Éléments liés |
| V1.5 | Extraction des reels Instagram (ADR-0025) |
| V1.5 | Découpage des contenus très longs |
| V1.5 ou plus tard | Mémo vocal transcrit |
| V2 | Sujets suivis et sources de veille |
| V2 | Digest du matin, notification locale |
| V2 | L'essentiel du jour (actualité générale) |
| V2 | Regroupement des articles, importance, diversité |
| V2 | Rétention des articles non gardés |
| V2 | Surlignage |
| V3 | Avis de pertinence |
| V3 | Questions de révision |
| V3 | Badge « peut-être obsolète » |
| V3 | Serveur MCP sur la base |
| Plus tard | Questions posées à la base, suggestions de sujets, radar techno, widget iOS |
