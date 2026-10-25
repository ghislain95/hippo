# ADR-0035 — Contrat d'API : fiches générées, identifiants et synchronisation

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Précise** : [ADR-0017](0017-contrat-openapi.md) et [ADR-0018](0018-lecture-hors-ligne.md)

## Contexte

Le change `api-contract-v1` (story E0-05) écrit le contrat complet de la V1. Trois points d'ADR-0017 et d'ADR-0018 ne tenaient pas à l'usage :

- ADR-0017 demande que le contrat référence les schémas de fiches, mais oapi-codegen ne suit pas une `$ref` vers un fichier JSON Schema, et l'assemblage par Redocly recopie des `$id` qui cassent un validateur JSON Schema 2020-12.
- ADR-0017 dit « identifiants ULID », alors que le modèle de données de la spec et le schéma `classify_item` utilisent des entiers pour les catégories, et des clés pour les types de contenu.
- ADR-0018 synchronise « depuis une date » : deux écritures dans la même milliseconde, ou une horloge qui saute sur le NAS, feraient perdre des changements, et rien ne dit quoi faire après les 30 jours de conservation des suppressions.

## Décision

**Fiches dans le contrat**

- L'outil `schemabundle` produit, à partir de `docs/prompt/schemas/`, les composants OpenAPI des fiches (`api/fiches.gen.json`) : un composant par fiche (`FicheRecipe`…), sans `$id` ni `$schema`, et une union `Fiche` dont le discriminateur `template` et sa table de correspondance sont tirés des schémas eux-mêmes.
- `api/openapi.yaml` référence ces composants ; `redocly bundle` en fait un contrat autonome (`api/openapi.bundled.gen.json`), dont sont générés le serveur Go et les types TypeScript. Les schémas de `docs/prompt/schemas/` restent la seule source du format des fiches.
- Un changement incompatible d'un schéma de fiche est un changement cassant de l'API : soit le type est réanalysé avant la nouvelle version de l'appli, soit un composant versionné est ajouté.

**Identifiants**

- ULID pour les éléments, les images et les batchs ; entiers pour les catégories et les tags ; les types de contenu sont désignés par leur clé.
- Un élément référence sa catégorie, son type et ses tags par identifiant, jamais par leur nom ni par le chemin de la catégorie : un renommage ne laisse pas de copie périmée dans le cache de l'appli.

**Synchronisation**

- `GET /api/sync` prend un `sync_token` opaque donné par le serveur, plus un curseur de pagination ; seule la dernière page renvoie le nouveau jeton.
- Le jeton encode une **séquence de modification**, compteur commun à toute la base (SQLite n'a qu'un écrivain ; les types de contenu sont partagés entre utilisateurs), incrémentée à chaque écriture d'une ligne synchronisée, et non une date. Les compteurs d'éléments (`item_count`) envoyés avec les catégories, types et tags ne sont pas tenus à jour par la synchronisation : l'appli les recalcule depuis son cache. Les tables synchronisées (éléments, catégories, types, tags) portent cette séquence ; elles sont créées avec elle par les stories E2 à E4.
- Les suppressions sont renvoyées sous forme de marques `{id, deleted_at}`, gardées 30 jours, sans aucun contenu (RG-79). Un jeton plus ancien que cette conservation est refusé (`410`, `urn:hippo:problem:sync-expired`) : l'appli vide son cache et resynchronise tout.

## Conséquences

- Le contrat compte deux fichiers générés de plus, vérifiés à jour en CI ; la CI vérifie aussi le code généré quand un schéma de fiche change.
- La première story qui crée chaque table synchronisée y ajoute la séquence de modification et les marques de suppression, dès sa migration.
- L'appli construit le chemin d'une catégorie et les noms des tags depuis son cache, pas depuis l'élément.

## Alternatives écartées

- **Fiche typée comme objet libre** : perd le typage qu'ADR-0006 demande, et chaque template devrait deviner la forme de la fiche.
- **Composants qui pointent vers les fichiers de fiches, plus un décorateur Redocly qui retire `$id`** : deux mécanismes au lieu d'un, et une table de correspondance écrite à la main, qui peut dériver.
- **ULID partout** : des identifiants de 26 caractères que l'IA recopierait à chaque catégorie du référentiel, pour aucun gain avec un seul serveur.
- **Synchronisation par `updated_at`** : simple, mais fragile face aux écritures simultanées et aux sauts d'horloge, alors que SQLite n'a qu'un écrivain et rend la séquence gratuite.
