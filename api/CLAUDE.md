# api/ — contrat OpenAPI

`openapi.yaml` est la source de vérité de l'API ([ADR-0017](../docs/adr/0017-contrat-openapi.md)) : le serveur Go et le client TypeScript en sont générés ([ADR-0031](../docs/adr/0031-generation-depuis-les-contrats.md), [ADR-0035](../docs/adr/0035-contrat-identifiants-et-synchronisation.md)).

## Fichiers

- `openapi.yaml` : écrit à la main, **le seul à modifier**.
- `fiches.gen.json` : composants des fiches, générés depuis `docs/prompt/schemas/fiches/` par `schemabundle` (un composant `Fiche<Type>` par fiche, union `Fiche` discriminée par `template`). Ne jamais l'éditer : changer le schéma de la fiche.
- `openapi.bundled.gen.json` : contrat autonome (`redocly bundle`), lu par oapi-codegen et openapi-typescript. Généré.
- `redocly.yaml`, `.redocly.lint-ignore.yaml` : règles de lint.

## Règles critiques

- Toute évolution d'API commence ici, **avant** le code, dans le change OpenSpec qui la porte. Les opérations et les schémas sont définitifs ; les codes et types d'erreur d'une route restent provisoires jusqu'à la story qui l'implémente, qui les amende ici d'abord.
- OpenAPI 3.1 ; JSON en `snake_case` ; dates ISO 8601 UTC (`format: date-time`).
- Identifiants : ULID pour les éléments, les images et les batchs ; entiers pour les catégories et les tags ; clé pour les types de contenu. Un élément référence catégorie, type et tags par identifiant, jamais par nom.
- Erreurs `application/problem+json` (schéma `Problem`) : type métier `urn:hippo:problem:<slug>` du catalogue de la description, `about:blank` sinon.
- Pagination par curseur (`cursor`, `limit`, `next_cursor`).
- **Nullabilité** : scalaire `type: [T, "null"]` sur la propriété ; référence `anyOf: [{$ref}, {type: "null"}]` ; fiche facultative, pas nullable. Jamais de schéma nullable nommé, jamais `type: [object, "null"]` avec un `oneOf`.
- Toutes les routes sont sous `/api` et exigent le token Bearer, sauf `/healthz`.
- Noms des schémas, champs et valeurs d'énumération tirés du glossaire.
- Après chaque modification : `make generate`, puis commit du contrat **et** des fichiers générés.

## Commandes (depuis la racine du dépôt)

```bash
make lint-api         # Redocly
make generate         # schemabundle, redocly bundle, api.gen.go, schema.gen.ts (dans cet ordre)
make check-generated  # échoue si un fichier généré n'est pas à jour
```
