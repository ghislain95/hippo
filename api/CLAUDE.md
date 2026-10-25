# api/ — contrat OpenAPI

`openapi.yaml` est la source de vérité de l'API ([ADR-0017](../docs/adr/0017-contrat-openapi.md)) : le serveur Go et le client TypeScript en sont générés ([ADR-0031](../docs/adr/0031-generation-depuis-les-contrats.md)).

## Règles critiques

- Toute évolution d'API commence ici, **avant** le code, dans le change OpenSpec qui la porte.
- OpenAPI 3.1 ; JSON en `snake_case` ; dates ISO 8601 UTC (`format: date-time`) ; identifiants ULID ; erreurs `application/problem+json` (schéma `Problem`, `type` métier `urn:hippo:problem:<slug>`) ; pagination par curseur (`cursor`, `limit`, `next_cursor`).
- Toutes les routes sont sous `/api` et exigent le token Bearer, sauf `/healthz`.
- Noms des schémas, champs et valeurs d'énumération tirés du glossaire.
- Après chaque modification : `make generate`, puis commit du contrat **et** du code généré.

## Commandes (depuis la racine du dépôt)

```bash
make lint-api         # Redocly
make generate         # backend/internal/httpapi/api.gen.go et app/src/api/schema.gen.ts
make check-generated  # échoue si le code généré n'est pas à jour
```
