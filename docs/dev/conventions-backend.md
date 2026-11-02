# Conventions du backend Go

Le backend est un binaire Go unique, sans framework ([ADR-0003](../adr/0003-backend-go.md)) : API HTTP, stockage SQLite, extraction, planificateur nocturne. Ce document fixe l'organisation du code. Commandes : `backend/CLAUDE.md`.

## Organisation

```
backend/
├── cmd/
│   ├── hippo/              point d'entrée : config, logger, base, migrations, serveur, arrêt propre
│   └── schemabundle/       outil de build : assemble et valide les schémas de fiches
├── internal/
│   ├── config/             variables d'environnement → struct Config validée
│   ├── httpapi/            transport HTTP : serveur généré, handlers, middlewares, problem+json
│   │   └── api.gen.go      généré depuis api/openapi.yaml (jamais édité)
│   ├── store/              SQLite : ouverture, migrations, requêtes ; un fichier par table métier
│   │   └── migrations/     NNNNN_<nom>.sql (goose), embarquées dans le binaire
│   ├── assetfs/            fichiers des images sous DATA_DIR/assets ; feuille, sans HTTP ni base
│   ├── extraction/         téléchargement et lecture des liens (pages, PDF), dossier DATA_DIR/tmp
│   ├── analysis/schema/    schéma classify_item assemblé (généré), embarqué
│   └── <domaine>/          un package par domaine métier, créé par le change qui l'introduit
└── testdata/               fixtures (pages enregistrées, réponses de l'IA, jeu de test)
```

Les packages de domaine suivent le glossaire et naissent avec leurs stories : `capture` (E2), `extraction` (E2), `analysis` (E3, avec l'interface `Analyzer`), `library`, `review`, `export` (E4). Un nouveau package de premier niveau sous `internal/` se justifie dans le `design.md` du change.

### Dépendances entre couches

```
cmd/hippo ──► httpapi ──► <domaine> ──► store
                 │             │
                 └──► config   └──► analysis (Analyzer) ──► SDK Anthropic
```

- `httpapi` décode, appelle un domaine, encode. **Aucune règle métier dans un handler.**
- Un domaine ne connaît pas HTTP (pas d'import de `net/http`, sauf `extraction` qui télécharge des pages) ni le code généré.
- `store` ne connaît ni HTTP ni les domaines au-dessus de lui.
- Seul `internal/analysis` importe `anthropic-sdk-go`, derrière l'interface `Analyzer` ([ADR-0005](../adr/0005-haiku-batch.md)).
- Ces règles sont vérifiées par `depguard` dans `backend/.golangci.yml` : une violation casse le lint.
- `assetfs` est une feuille, importée seulement par `httpapi` et `cmd/hippo` (le domaine `capture` déclare l'interface dont il a besoin) : elle ne connaît ni HTTP, ni la base, ni aucun autre package Hippo. Deux règles `depguard` le vérifient : `assetfs-is-a-leaf` pour ce qu'elle importe, et celle du domaine `capture`, qui lui interdit d'importer `assetfs` (E2-04).
- Seul `internal/extraction` importe Readability (`codeberg.org/readeck/go-readability/v2`), `golang.org/x/net` (`html`, `html/charset`) et le lecteur de PDF `github.com/ledongthuc/pdf` : règle `depguard` `page-reading-only-in-extraction` (E2-05, étendue par E2-08). Dans le package, le lecteur de PDF n'est appelé que par `pdf.go` et `pdflayout.go`, exécutés dans le processus enfant. Le générateur de PDF de test `internal/extraction/pdftest` n'est importé que par des tests : règle `pdf-generator-only-in-tests`. `capture` ne connaît pas `extraction` : il déclare une interface `Waker` (`Wake()`), branchée par `cmd/hippo`.

## Contrat d'API

- `api/openapi.yaml` fait foi ([ADR-0017](../adr/0017-contrat-openapi.md)) ; on le modifie **avant** le code, puis `make generate`.
- `oapi-codegen` génère l'interface stricte du serveur (`StrictServerInterface`) et le routage `net/http` ([ADR-0031](../adr/0031-generation-depuis-les-contrats.md)). Le handler implémente cette interface : une route oubliée ou un type faux ne compile pas.
- JSON en `snake_case`, dates ISO 8601 UTC (`time.Time` en UTC). Identifiants ([ADR-0035](../adr/0035-contrat-identifiants-et-synchronisation.md)) : ULID (`TEXT`) pour les éléments, les images et les batchs ; entiers pour les catégories et les tags ; clé (`key`) pour les types de contenu.
- **Nullabilité** : un scalaire nullable s'écrit `type: [T, "null"]` sur la propriété, une référence nullable `anyOf: [{$ref: …}, {type: "null"}]` ; la fiche est facultative plutôt que nullable. Jamais de schéma nullable nommé (oapi-codegen le rend non nullable), jamais `type: [object, "null"]` avec un `oneOf`. Dans un corps `PATCH`, un champ absent ne change rien et un `null` explicite efface (types `nullable.Nullable[T]`).
- **Corps JSON strict** : le décodeur généré (`encoding/json` par défaut) ignore les membres inconnus et les données après la valeur, et ne vérifie ni `pattern` ni `enum`. Chaque opération implémentée inscrit donc son corps JSON dans le registre du middleware de corps strict de `httpapi` : membre non déclaré ou données après la valeur → `400 about:blank`. Ce middleware borne aussi tout corps `application/json` à 1 Mio (`413 about:blank`). Un second middleware de `httpapi`, `boundTransfers`, borne le corps `multipart/form-data` de `POST /api/items` à 21 Mio et pose par `http.ResponseController` les délais propres à une route, au lieu des 60 s globales du serveur : 5 minutes de lecture et d'écriture pour ce multipart (la réponse part après l'envoi), 5 minutes d'écriture pour `GET /api/assets/{asset_id}`. Les `pattern` et `enum` (`Ulid`, `CaptureSource`…) sont vérifiés dans le domaine.
- Les erreurs métier ont un type stable du catalogue de `api/openapi.yaml` ; une opération déclarée mais pas encore implémentée répond `501 not-implemented` (stub embarqué par `handlers`, remplacé par la story qui l'implémente).
- Pagination par curseur : paramètres `cursor` et `limit`, réponse `{ "items": [...], "next_cursor": "…" | null }`. Le curseur est opaque pour le client.

## Erreurs

- Toujours enveloppées avec le contexte : `fmt.Errorf("open database %s: %w", path, err)`. Jamais avalées (`_ = err` interdit hors `Close` en sortie de fonction de lecture, et commenté).
- Les domaines exposent des erreurs sentinelles ou typées (`ErrNotFound`, `ValidationError{Field, Reason}`), testées avec `errors.Is` / `errors.As`.
- `httpapi` traduit chaque erreur en **problem+json** (RFC 9457) : `type`, `title`, `status`, `detail`. Les erreurs métier ont un `type` stable `urn:hippo:problem:<slug>` (ex. `urn:hippo:problem:budget-exceeded`), que l'appli peut reconnaître ; les erreurs HTTP génériques ont `about:blank`. Le `detail` est affichable tel quel (en français).
- Une erreur inattendue → `500` avec un `detail` générique ; la cause va dans les logs, jamais dans la réponse.
- `panic` seulement pour une erreur de programmation au démarrage ; `log.Fatal` seulement dans `main`.

## Logs

- `log/slog`, handler JSON sur la sortie standard, niveau par `LOG_LEVEL` (`debug`, `info`, `warn`, `error`).
- Le logger est passé explicitement (champ de struct), pas de logger global hors `main`.
- Une ligne par requête HTTP : méthode, motif de route, statut, durée, identifiant de requête.
- **Jamais dans un log** : un token ou une clé, l'en-tête `Authorization`, un corps de requête ou de réponse, la chaîne de requête de recherche, le contenu d'un élément (titre, note, fiche, contenu extrait), une URL capturée. On logue des identifiants (`item_id`), des compteurs et des durées.

## Stockage SQLite

- Driver `modernc.org/sqlite` (sans CGO), mode WAL, `foreign_keys` activé, `busy_timeout` de 5 s, transactions d'écriture en `BEGIN IMMEDIATE` ([ADR-0004](../adr/0004-sqlite.md)).
- **Chaque requête sur une table métier filtre par `user_id`** (RG-90, [ADR-0022](../adr/0022-preparation-multi-utilisateur.md)). Le `user_id` vient du contexte authentifié (`httpapi` → paramètre explicite des fonctions du domaine et du store), jamais d'un champ de la requête.
- Tables déclarées `STRICT` : SQLite refuse une valeur du mauvais type au lieu de la convertir en silence.
- Requêtes paramétrées (`?`) uniquement ; aucune concaténation de valeur.
- `defer rows.Close()` et vérification de `rows.Err()`.
- Les tables synchronisées (éléments, catégories, types de contenu, tags) portent `updated_at` et la séquence de modification `change_seq`, incrémentée à chaque écriture, qui ordonne la synchronisation ; les suppressions laissent une marque `deleted_at`, sans contenu, pendant 30 jours ([ADR-0018](../adr/0018-lecture-hors-ligne.md), [ADR-0035](../adr/0035-contrat-identifiants-et-synchronisation.md)).
- **La séquence n'avance que par la fonction dédiée du store** (`internal/store/sequence.go`), appelée dans la transaction de l'écriture : elle incrémente l'unique ligne de `sync_sequence` et renvoie la nouvelle valeur. Toute écriture d'une ligne synchronisée passe par elle ; pas de trigger, pas de `MAX(change_seq) + 1`. Une table non synchronisée (par exemple `capture_ids`) n'a pas de `change_seq` : ses écritures n'avancent pas la séquence.
- `change_seq` est `NOT NULL` **sans défaut** : une insertion qui oublie la séquence échoue au lieu d'écrire une valeur fausse.

### Migrations

- Fichiers SQL `internal/store/migrations/NNNNN_<nom_snake_case>.sql`, annotés `-- +goose Up`, appliqués au démarrage par goose utilisé comme librairie ([ADR-0032](../adr/0032-migrations-goose.md)).
- **Une migration appliquée n'est jamais modifiée, supprimée ni renommée** : on en ajoute une. La CI le vérifie.
- Pas de migration descendante : un retour arrière passe par la sauvegarde ([ADR-0020](../adr/0020-sauvegardes.md)).

## Fichiers des images

Les images sont sur le disque, sous `DATA_DIR/assets`, référencées par la table `assets` ([ADR-0004](../adr/0004-sqlite.md), modèle dans la [spec](../spec-v1.md#modèle-de-données)). Seul `internal/assetfs` y touche.

- **Un fichier est écrit et renommé avant la transaction**, jamais après la validation : fichier temporaire `.tmp-<aléatoire>` dans `DATA_DIR/assets` (même système de fichiers que la destination), `fsync`, renommage en `<user_id>/<asset_id>.<ext>`, `fsync` du dossier. Une ligne ne pointe donc jamais vers un fichier absent.
- **Il est supprimé sur tout chemin d'échec** : vérification refusée après l'écriture, capture rejouée, erreur, transaction qui n'aboutit pas. Seul un nouvel élément validé garde son fichier.
- Un crash entre le renommage et la validation laisse un fichier orphelin : accepté. Au démarrage, `cmd/hippo` crée `DATA_DIR/assets` et supprime les seuls fichiers `.tmp-*`.
- Droits : fichiers `0640`, dossiers `0750`.
- Le chemin est construit à partir d'identifiants du serveur uniquement, jamais d'une valeur de la requête ; la lecture passe par `os.Root` sur `DATA_DIR/assets`.
- Le nom et le type déclarés par le client ne sont ni stockés ni logués.

## HTTP sortant

Seul `internal/extraction` contacte des sites (E2-05). Le comportement est dans la [spec](../spec-v1.md#extraction-dun-élément) ; ces règles fixent le code et se vérifient en relecture.

- **Un client propre.** Jamais `http.DefaultClient`, `http.Get` ni `readability.FromURL` (transport par défaut, sans contexte ni limite).
- **Politique d'adresses (RG-27)** dans `net.Dialer.Control` : chaque adresse réellement appelée est vérifiée, redirections et changement de résolution DNS compris. La politique est une `func(netip.AddrPort) bool` passée au worker : `PublicAddress` en production ; les tests passent la leur, qui accepte aussi `127.0.0.1` pour `httptest`.
- **Pas de proxy** : `Transport.Proxy = nil`, les variables `HTTP_PROXY` ne sont pas lues.
- **Limites** : un seul délai de 15 s pour tout le téléchargement, PDF compris, par le contexte de la requête ; `TLSHandshakeTimeout` ; `MaxResponseHeaderBytes` de 64 Kio ; 10 redirections au plus, `http` et `https` seulement ; corps lu par `io.LimitReader` sur le flux décompressé : 2 Mio pour une page HTML, 50 Mio (`MaxPDFSize`) pour un PDF ; un PDF annoncé plus gros par son `Content-Length` n'est pas lu du tout. La page ou le PDF est lu dans un processus enfant borné en mémoire et en temps (ci-dessous). Pas d'`Accept-Encoding` manuel, pour garder la décompression gzip de Go.
- **Format décidé avant de lire le corps** : les 512 premiers octets sont lus d'avance (`bufio.Reader` de 4 Kio). Un PDF est une réponse de type `application/pdf` ou dont le corps commence par `%PDF-`, quel que soit son type déclaré ; sinon une page HTML (type HTML ou absent) est lue en mémoire, et tout autre type n'est pas lu au-delà de ces octets. Le format est décidé aussi quand le corps cale avant 512 octets, d'après le type et ce qui est arrivé. Un PDF est copié par `io.Copy` dans un fichier temporaire sans nom (ci-dessous), jamais en mémoire : la mémoire du service ne grandit pas avec le PDF. Une erreur d'écriture du fichier (disque plein) est distinguée d'une erreur de lecture du réseau : la première laisse l'élément en file (`storage_unavailable`), la seconde est celle du site.
- **En-tête `Accept`** : `text/html,application/xhtml+xml,application/pdf;q=0.9`, pour qu'un site qui négocie le type serve un lien PDF au lieu de `406`, tout en préférant le HTML quand il a les deux.
- **Pas de cookie persistant** : un stock éphémère par élément. Les identifiants de l'URL (`user:mot-de-passe@`) sont retirés avant la requête, sinon `net/http` les enverrait en Basic Auth.
- **Jamais dans un log ni en base** : l'URL, l'hôte ou l'erreur de téléchargement (`*url.Error` affiche l'URL). Une ligne par extraction : `item_id`, mode, classe de raison (`ok`, `timeout`, `status`, `blocked`… ; liste dans le [glossaire](../glossary.md)), `format` (`pdf`) pour un PDF, statut HTTP, durée ; `last_error` reste nul. Pour un PDF, jamais non plus le nom de son fichier, le chemin du fichier temporaire ni une erreur de la librairie PDF.
- **Processus enfant** : la page est lue par `hippo extract-page`, lancé par `exec.CommandContext` avec un environnement explicite (`GOMAXPROCS=2` seulement, jamais celui du service, qui porte les secrets), stderr ignoré (un message de panique peut citer la page), sortie bornée à 2 Mio. Seul `internal/extraction/child.go` lance un sous-processus (exclusion `gosec` G204 ciblée dans `.golangci.yml`).
- **PDF dans l'enfant** : la requête porte `format: pdf` et `deadline_ms` ; le PDF n'est pas sur l'entrée standard mais sur le **descripteur 3** (`exec.Cmd.ExtraFiles`), ouvert en lecture seule, le seul que l'enfant hérite (Go ouvre les autres en close-on-exec). Aucun chemin ne traverse la frontière du processus. L'enfant lit le PDF dans une goroutine et répond à `deadline_ms` (80 % de `ReadTimeout`) avec ce qu'il a lu ; il ne recopie jamais un message d'erreur de la librairie PDF (il cite des octets du fichier) : il le traduit en raison (`encrypted`, `damaged`…). Le parent n'accepte ces raisons que pour `format: pdf`.
- **Arrêt** : le worker a son propre contexte, annulé par `cmd/hippo`, qui attend sa fin avant de fermer la base. Les écritures après un téléchargement passent par `context.WithoutCancel`, bornées à 10 s.

## Dossier temporaire de l'extraction

Un PDF est téléchargé dans `DATA_DIR/tmp`, propre à `internal/extraction` (`tempfile.go`), jamais dans `os.TempDir()` : `/tmp` du conteneur est la couche de l'image ou un tmpfs compté dans sa mémoire de 128 Mo ([ADR-0019](../adr/0019-deploiement-docker-nas.md)).

- `cmd/hippo` l'ouvre au démarrage par `extraction.OpenTempDir` : créé en `0700`, accès par `os.Root`, **tout fichier régulier qui y reste est supprimé** (nombre logué), puis une sonde vérifie qu'on peut y écrire : le service échoue au démarrage plutôt qu'au premier PDF.
- Avant chaque PDF, l'espace libre du volume est lu (`syscall.Statfs`, un fichier `freespace_*.go` par système : Linux et macOS ; ailleurs il est inconnu et seule l'écriture échoue) : il faut la taille annoncée du PDF, ou 50 Mio quand elle est inconnue, plus une marge de 64 Mio (`freeSpaceMargin`) laissée à la base, à son WAL et aux images ; sinon rien n'est écrit et l'élément reste en file (`storage_unavailable`).
- Par PDF : fichier `.pdf-<aléatoire>` créé en `0600` (`O_EXCL`), rouvert en lecture seule, puis **son nom est supprimé avant le téléchargement**. Les données vivent tant qu'un descripteur est ouvert (le service écrit, l'enfant lit) et disparaissent avec le dernier, crash compris. Seul l'instant entre la création et la suppression du nom peut laisser un fichier : le balayage du démarrage le supprime.
- Le worker ferme les deux descripteurs sur tous les chemins (lu, repli, laissé en file, arrêt). Les sauvegardes ([ADR-0020](../adr/0020-sauvegardes.md)) ne voient jamais un fichier sans nom.
- Les erreurs du dossier ne nomment aucun fichier (comme `assetfs`) ; ni le chemin ni le nom n'atteignent un log.

## Configuration

- Uniquement par variables d'environnement, lues une fois dans `config.Load`, qui reçoit une fonction `getenv` (testable) et renvoie une erreur claire pour toute valeur manquante ou invalide. Le binaire refuse de démarrer sur une configuration invalide.
- La liste des variables et leur rôle est dans `infra/.env.example` ; toute nouvelle variable y est documentée dans le même commit.

## Style Go

- `gofumpt` + `goimports` (appliqués par le hook après édition, vérifiés par golangci-lint).
- Identifiants et commentaires en anglais, noms tirés du glossaire (`Item`, `Fiche`, `Category`, `ContentType`, `ReadStatus`). « fiche » reste `fiche`.
- `context.Context` en premier paramètre de toute fonction qui fait des entrées-sorties.
- Constructeurs `NewX(...)` qui reçoivent leurs dépendances ; pas de variable globale mutable, pas d'`init()` qui fait des entrées-sorties.
- Les interfaces sont déclarées par celui qui les consomme, petites (une à trois méthodes).
- Commentaire de documentation sur tout identifiant exporté ; un commentaire dans le code explique **pourquoi**, pas quoi.
- Tests à côté du code (`x_test.go`), voir [testing.md](testing.md).

## Code généré

| Fichier | Source | Commande |
| --- | --- | --- |
| `internal/analysis/schema/classify-item.bundled.json` | `docs/prompt/schemas/` | `make generate` (schemabundle) |
| `api/fiches.gen.json` (composants OpenAPI des fiches) | `docs/prompt/schemas/fiches/` | `make generate` (schemabundle) |
| `api/openapi.bundled.gen.json` (contrat autonome) | `api/openapi.yaml` + `api/fiches.gen.json` | `make generate` (redocly bundle) |
| `internal/httpapi/api.gen.go` | `api/openapi.bundled.gen.json` | `make generate` (oapi-codegen) |
| `app/src/api/schema.gen.ts` | `api/openapi.bundled.gen.json` | `make generate` (openapi-typescript) |

`make generate` les produit dans cet ordre (l'ordre compte, il mêle Go et Node).

Jamais édité à la main (le hook `protect-files.sh` le refuse) ; la CI vérifie qu'il est à jour (`make check-generated`).
