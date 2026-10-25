# ADR-0004 — SQLite comme base de données

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Un seul utilisateur, quelques milliers d'éléments à terme, un NAS aux ressources limitées. Il faut de la recherche plein texte et, plus tard, de la recherche vectorielle.

## Décision

**SQLite** en mode WAL, un seul fichier dans le volume de données.

- Driver Go pur `modernc.org/sqlite`, sans CGO : FTS5 inclus, compilation croisée simple.
- Recherche plein texte avec une table virtuelle **FTS5**, tokenizer `unicode61 remove_diacritics 2` pour ignorer les accents, tenue à jour par des triggers.
- Migrations versionnées, appliquées au démarrage.
- Les images sont stockées **sur disque** (pas en BLOB), référencées par la table `assets`.

## Conséquences

- Aucun service de base de données à faire tourner, sauvegarde par simple copie cohérente (`VACUUM INTO`).
- Une seule instance du backend peut écrire : acceptable, il n'y en a qu'une.
- La recherche vectorielle (V1.5) se fera d'abord par calcul en Go sur des vecteurs stockés en BLOB (quelques milliers d'éléments), puis avec une extension dédiée si besoin.
- Les tests d'intégration utilisent une vraie base SQLite temporaire, jamais de mock.

## Alternatives écartées

- **PostgreSQL + pgvector** : plus puissant mais un service de plus à faire tourner, à sauvegarder et à mettre à jour, sans bénéfice à cette échelle.
- **Driver `mattn/go-sqlite3`** : nécessite CGO, ce qui complique la compilation croisée et l'image distroless.
