# ADR-0017 — Contrat d'API OpenAPI comme source de vérité

- **Statut** : Accepté — précisé par [ADR-0031](0031-generation-depuis-les-contrats.md) et [ADR-0035](0035-contrat-identifiants-et-synchronisation.md)
- **Date** : 2026-10-11

## Contexte

Le backend et l'appli sont écrits par des agents, souvent dans des sessions différentes. Sans contrat explicite, les deux côtés divergent (nom de champ, format de date, code d'erreur).

## Décision

- Le contrat est écrit en **OpenAPI 3.1** dans `api/openapi.yaml`. Il est modifié **avant** le code.
- Le serveur Go est généré avec `oapi-codegen` (mode compatible `net/http` standard, interface stricte) ; le client TypeScript de l'appli avec un générateur de types et de client.
- La CI vérifie que le code généré est à jour.
- Conventions : JSON en `snake_case`, dates en ISO 8601 UTC, identifiants ULID, erreurs au format [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) (`application/problem+json`), pagination par curseur.

## Conséquences

- Une incohérence entre back et front devient une erreur de compilation, pas un bug en production.
- Toute évolution d'API commence par une modification du contrat, ce qui la rend visible en revue.
- Les schémas des fiches (ADR-0006) sont référencés par le contrat OpenAPI pour typer `fiche`.

## Alternatives écartées

- **Code d'abord, documentation générée** : le contrat suit le code au lieu de le guider ; l'appli dépend de ce que le backend a fait.
- **Pas de contrat** : divergence assurée avec plusieurs agents.
