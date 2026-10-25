# ADR-0022 — Préparer le multi-utilisateur dès le modèle de données

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Hippo est d'abord personnel, mais pourrait un jour être proposé à d'autres. Ajouter la notion d'utilisateur après coup dans toutes les tables et requêtes est une migration lourde et risquée.

## Décision

- Toutes les tables métier ont une colonne `user_id` (`items`, `categories`, `tags`, `assets`, `batches`, `settings`, et les futures tables de veille).
- Les contraintes d'unicité incluent `user_id` (URL normalisée, nom de catégorie, tag).
- **Toute requête filtre par `user_id`** ; l'utilisateur courant vient de l'authentification (ADR-0016).
- Les types de contenu restent globaux (partagés), car ils sont liés aux schémas et aux templates du code.
- En V1, un seul utilisateur, créé au premier démarrage.

## Conséquences

- Coût quasi nul aujourd'hui.
- Un test d'intégration vérifie qu'un second utilisateur ne voit jamais les données du premier, pour éviter qu'un oubli de filtre passe inaperçu.
- Le passage à plusieurs utilisateurs demandera encore un vrai système de comptes et une infrastructure adaptée, mais pas de refonte des données.

## Alternatives écartées

- **Mono-utilisateur strict** : plus simple d'une colonne, mais migration douloureuse si l'idée prend.
- **Une base par utilisateur** : envisageable plus tard, mais rien ne presse.
