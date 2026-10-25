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

## Contrat d'API

- `api/openapi.yaml` fait foi ([ADR-0017](../adr/0017-contrat-openapi.md)) ; on le modifie **avant** le code, puis `make generate`.
- `oapi-codegen` génère l'interface stricte du serveur (`StrictServerInterface`) et le routage `net/http` ([ADR-0031](../adr/0031-generation-depuis-les-contrats.md)). Le handler implémente cette interface : une route oubliée ou un type faux ne compile pas.
- JSON en `snake_case`, dates ISO 8601 UTC (`time.Time` en UTC). Identifiants ([ADR-0035](../adr/0035-contrat-identifiants-et-synchronisation.md)) : ULID (`TEXT`) pour les éléments, les images et les batchs ; entiers pour les catégories et les tags ; clé (`key`) pour les types de contenu.
- **Nullabilité** : un scalaire nullable s'écrit `type: [T, "null"]` sur la propriété, une référence nullable `anyOf: [{$ref: …}, {type: "null"}]` ; la fiche est facultative plutôt que nullable. Jamais de schéma nullable nommé (oapi-codegen le rend non nullable), jamais `type: [object, "null"]` avec un `oneOf`. Dans un corps `PATCH`, un champ absent ne change rien et un `null` explicite efface (types `nullable.Nullable[T]`).
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

### Migrations

- Fichiers SQL `internal/store/migrations/NNNNN_<nom_snake_case>.sql`, annotés `-- +goose Up`, appliqués au démarrage par goose utilisé comme librairie ([ADR-0032](../adr/0032-migrations-goose.md)).
- **Une migration appliquée n'est jamais modifiée, supprimée ni renommée** : on en ajoute une. La CI le vérifie.
- Pas de migration descendante : un retour arrière passe par la sauvegarde ([ADR-0020](../adr/0020-sauvegardes.md)).

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
