# ADR-0032 — Migrations SQLite avec goose

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Précise** : [ADR-0004](0004-sqlite.md)

## Contexte

ADR-0004 demande des migrations versionnées, appliquées au démarrage, avec le driver pur Go `modernc.org/sqlite` et une image distroless (pas d'outil externe dans l'image).

## Décision

- **`pressly/goose` v3, utilisé comme librairie** : les fichiers SQL `backend/internal/store/migrations/NNNNN_<nom>.sql` sont embarqués dans le binaire (`embed`) et appliqués par `goose.NewProvider` au démarrage, avant d'ouvrir le serveur HTTP.
- Migrations en SQL pur, annotées `-- +goose Up`, sans migration descendante : le retour arrière passe par la sauvegarde ([ADR-0020](0020-sauvegardes.md)).
- Une migration appliquée n'est jamais modifiée, supprimée ni renommée ; la CI le vérifie.

## Conséquences

- Rien à installer sur le NAS ; une base neuve et une base existante suivent le même chemin.
- Les tests d'intégration appliquent les vraies migrations sur une base temporaire.
- Une dépendance de plus, stable et répandue.

## Alternatives écartées

- **Migrateur maison** (une table de versions et une boucle) : une soixantaine de lignes, mais à tester et maintenir nous-mêmes, pour les mêmes garanties.
- **golang-migrate** : pilotes séparés et support de `modernc.org/sqlite` moins direct.
- **Migrations appliquées par un outil externe au déploiement** : contraire à l'image distroless et au démarrage autonome.
