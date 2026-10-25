# ADR-0016 — Authentification par token d'API statique

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Un seul utilisateur, un service accessible uniquement via son réseau Tailscale privé. Il faut quand même une protection, au cas où un autre appareil du réseau Tailscale tenterait d'y accéder.

## Décision

- Toutes les routes `/api/*` exigent `Authorization: Bearer <token>`. Seul `/healthz` est public.
- Le token est une chaîne aléatoire longue (au moins 32 octets), fournie au backend par variable d'environnement (`API_TOKEN`) et injectée au build dans l'appli.
- Comparaison en temps constant ; le token n'apparaît jamais dans les logs.
- Le token est associé à un utilisateur, ce qui fournit le `user_id` des requêtes (ADR-0022).

## Conséquences

- Simple à mettre en place et à faire tourner.
- Changer le token demande de reconstruire l'appli.
- En cas de commercialisation, il faudra un vrai système de comptes (nouvel ADR) ; le `user_id` déjà présent partout facilite la transition.

## Alternatives écartées

- **OAuth / OIDC, comptes utilisateurs** : complexité sans objet pour un seul utilisateur sur un réseau privé.
- **Aucune authentification** : le réseau Tailscale peut contenir d'autres appareils.
