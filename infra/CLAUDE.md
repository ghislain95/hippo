# infra/ — déploiement

Docker Compose sur le NAS, avec un sidecar Tailscale qui publie le backend en HTTPS sans port ouvert ([ADR-0015](../docs/adr/0015-reseau-tailscale.md), [ADR-0019](../docs/adr/0019-deploiement-docker-nas.md)).

## Fichiers

- `docker-compose.yml` : production (NAS) et Mac avec Tailscale ; image `ghcr.io/ghislain95/hippo`.
- `docker-compose.local.yml` : variante Mac **sans Tailscale** ; image construite depuis `../backend`, port publié sur `127.0.0.1:8080` uniquement.
- `.env.example` : toutes les variables, commentées. `.env` (non versionné) est créé par Ghislain ; **les agents ne le lisent jamais** (hook).
- `tailscale/config/serve.json` : configuration de `tailscale serve`.
- `data/` et `tailscale/state/` : données d'exécution, non versionnées.

## Règles critiques

- Aucun port ouvert sur la box ; aucun secret dans un fichier versionné.
- `mem_limit: 128m`, `restart: unless-stopped`, healthcheck par `hippo healthcheck` (l'image distroless n'a pas de `curl`).
- Toute nouvelle variable est documentée dans `.env.example` dans le même commit.

## Commandes (depuis la racine du dépôt)

```bash
make image   # construit l'image du backend
make up      # compose local, puis : curl -fsS http://127.0.0.1:8080/healthz
make logs
make down
```
