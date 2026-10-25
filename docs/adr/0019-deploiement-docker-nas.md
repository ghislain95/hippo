# ADR-0019 — Déploiement Docker Compose sur le NAS

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Le NAS (Ugreen DXP4800 Plus) fait tourner des conteneurs Docker. Le déploiement doit être simple, reproductible et testable à l'identique sur le Mac en attendant le NAS.

## Décision

- Image du backend **distroless** (binaire statique), environ 20 Mo, construite par **GitHub Actions** et publiée sur **GitHub Container Registry** (privé).
- `infra/docker-compose.yml` avec deux services : `tailscale` (sidecar, ADR-0015) et `hippo` (backend, dans le réseau du sidecar).
- Configuration par variables d'environnement (`.env` non versionné) : `API_TOKEN`, `ANTHROPIC_API_KEY`, `ANALYSIS_MODEL`, `BATCH_SCHEDULE`, `BACKUP_SCHEDULE`, `MONTHLY_BUDGET_USD`, `TZ`, `TS_AUTHKEY`.
- Volume `./data` pour la base SQLite et les images.
- Limite mémoire du conteneur à 128 Mo, redémarrage automatique.
- Mise à jour : `docker compose pull && docker compose up -d`. Les migrations de base s'appliquent au démarrage.
- `/healthz` sert de sonde de santé.

## Conséquences

- Même compose sur le Mac et le NAS : on développe et on teste avant que les disques soient arrivés.
- Pas de déploiement continu automatique vers le NAS en V1 : la mise à jour est manuelle et volontaire.

## Alternatives écartées

- **Binaire lancé directement sur le système du NAS** : moins isolé, dépendant du système du constructeur.
- **Kubernetes (k3s)** : disproportionné pour un service.
