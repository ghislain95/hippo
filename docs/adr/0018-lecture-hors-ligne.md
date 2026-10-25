# ADR-0018 — Lecture hors ligne par cache local synchronisé

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

L'appli sert beaucoup à lire, souvent dans les transports ou sans Tailscale actif. Elle doit afficher et chercher les fiches sans joindre le NAS.

## Décision

- L'appli garde une **copie locale** des éléments (métadonnées, fiches, annotations, états de lecture) dans `expo-sqlite`, avec un index de recherche local.
- Synchronisation incrémentale par `GET /api/sync?since=<horodatage>` : le serveur renvoie les éléments créés, modifiés ou supprimés depuis cette date. Chaque table synchronisée a un `updated_at` et les suppressions laissent une trace (`deleted_at`) le temps de la synchronisation.
- Les écritures faites hors ligne dans l'appli (captures, épingle, annotations, progression de lecture) sont mises en **file d'attente locale** et rejouées au retour du réseau.
- **Conflits** : un seul utilisateur, donc la dernière écriture gagne, champ par champ.
- Les images sont mises en cache à la demande, pas toutes téléchargées d'avance.

## Conséquences

- Lecture et recherche fonctionnent partout (RG-97).
- Le backend doit tenir `updated_at` à jour sur toute modification et conserver les suppressions assez longtemps (30 jours).
- Le contenu extrait (`content_text`) n'est pas synchronisé : il ne sert qu'à l'IA.

## Alternatives écartées

- **Cache de requêtes HTTP uniquement** : pas de recherche hors ligne, données perdues au premier vidage du cache.
- **Base locale comme source de vérité, synchronisée vers le NAS** : beaucoup plus complexe, inutile tant que le serveur reste la référence.
