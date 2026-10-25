# ADR-0031 — Génération de code depuis les contrats

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Précise** : [ADR-0006](0006-sortie-structuree.md) et [ADR-0017](0017-contrat-openapi.md)

## Contexte

ADR-0017 fait du contrat OpenAPI la source de vérité et demande un serveur Go et un client TypeScript générés, vérifiés à jour en CI. ADR-0006 demande que les schémas des fiches, écrits en plusieurs fichiers reliés par `$ref`, soient assemblés en un schéma autonome au build. Restait à choisir les outils.

## Décision

- **Serveur Go** : `oapi-codegen` v2, cibles `std-http-server` et `strict-server` (interface stricte, routeur `net/http` de Go 1.22+), plus les modèles. Déclaré en `tool` dans `backend/go.mod`, lancé par `go generate`. Sortie : `backend/internal/httpapi/api.gen.go`.
- **Client TypeScript** : `openapi-typescript` (types), installé dans `api/` avec l'outillage du contrat (il exige TypeScript 5 alors que l'appli est en TypeScript 6), et `openapi-fetch` (client typé, quelques Ko), en dépendance de l'appli. Sortie : `app/src/api/schema.gen.ts`.
- **Lint du contrat** : Redocly CLI, en dépendance de `api/`.
- **Schémas de fiches** : un outil Go du dépôt, `backend/cmd/schemabundle`, assemble `classify-item.schema.json` en un fichier autonome (`backend/internal/analysis/schema/classify-item.bundled.json`, embarqué dans le binaire) et valide les exemples de `docs/prompt/schemas/examples/` avec `santhosh-tekuri/jsonschema` (JSON Schema 2020-12), la librairie qui validera les réponses de l'IA.
- `make generate` régénère tout ; `make check-generated` échoue si un fichier généré diffère de ce que produisent les sources. Les fichiers générés sont versionnés, marqués `linguist-generated`, et protégés contre l'édition par le hook des agents.

## Conséquences

- Une incohérence entre le contrat, le serveur et le client est une erreur de compilation.
- Le schéma assemblé est le même pour l'`input_schema` de l'outil et pour la validation des réponses.
- Les types Go et TypeScript des fiches ne sont pas encore générés : ils viendront avec le premier change qui écrit ou affiche une fiche.

## Alternatives écartées

- **ogen** : génère aussi le routage et la validation, mais impose son propre serveur, contraire à « librairie standard » (ADR-0003).
- **Client complet généré (orval, openapi-generator)** : beaucoup de code produit, dépendances lourdes ; `openapi-fetch` suffit avec TanStack Query.
- **Assemblage des schémas par un script Node** : une chaîne d'outils de plus, et une librairie de validation différente de celle du backend.
