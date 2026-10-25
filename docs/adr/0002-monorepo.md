# ADR-0002 — Un seul dépôt (monorepo)

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Le projet comporte un backend Go, une appli Expo, une configuration de déploiement et de la documentation. Les évolutions touchent souvent plusieurs parties à la fois (une nouvelle route d'API et l'écran qui l'utilise). Les agents travaillent mieux quand ils voient tout le contexte.

## Décision

Un seul dépôt GitHub privé :

```
hippo/
├── backend/   service Go
├── app/       appli Expo (React Native) et extension de partage
├── api/       contrat OpenAPI (voir ADR-0017)
├── infra/     docker compose, configuration Tailscale
└── docs/      métier, glossaire, spec, ADR, backlog, prompt
```

## Conséquences

- Une modification qui traverse le back et le front tient dans un seul commit et une seule revue.
- La CI doit savoir ne lancer que les jobs concernés (filtres par chemin) pour rester rapide.
- Chaque dossier aura ses propres instructions pour les agents.

## Alternatives écartées

- **Un dépôt par composant** : synchronisation du contrat d'API pénible, contexte fragmenté pour les agents, aucun bénéfice pour un projet porté par une seule personne.
