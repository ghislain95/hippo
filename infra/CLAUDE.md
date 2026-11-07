# infra/ — déploiement

Docker Compose sur le NAS, avec un sidecar Tailscale qui publie le backend en HTTPS sans port ouvert ([ADR-0015](../docs/adr/0015-reseau-tailscale.md), [ADR-0019](../docs/adr/0019-deploiement-docker-nas.md)). Installer, mettre à jour, revenir en arrière sur le NAS : [`docs/runbook-nas.md`](../docs/runbook-nas.md).

## Fichiers

- `docker-compose.yml` : production (NAS) et Mac avec Tailscale ; image `ghcr.io/ghislain95/hippo`.
- `docker-compose.local.yml` : variante Mac **sans Tailscale** ; image construite depuis `../backend`, port publié sur `127.0.0.1:8080` uniquement.
- `.env.example` : toutes les variables, commentées. `.env` (non versionné) est créé par Ghislain ; **les agents ne le lisent jamais** (hook).
- `tailscale/config/serve.json` : configuration de `tailscale serve`.
- `check-compose.sh` : contrôle statique des deux compose, lancé par `make check-infra` (section suivante).
- `data/` et `tailscale/state/` : données d'exécution, non versionnées.

## Règles critiques

- Aucun port ouvert sur la box ; aucun secret dans un fichier versionné.
- `mem_limit: 384m` (ADR-0038, amende ADR-0019 : copie des images du batch de nuit), `restart: unless-stopped`, healthcheck par `hippo healthcheck` (l'image distroless n'a pas de `curl`).
- Toute nouvelle variable est documentée dans `.env.example` dans le même commit, et ajoutée à l'`environment` du service `hippo` des deux compose, avec son défaut : ils listent les variables une à une (pas d'`env_file`), et une variable de `infra/.env` absente de cette liste n'atteint pas le conteneur. Exemple : les deux prix de l'analyse, `ANALYSIS_INPUT_PRICE_USD_PER_MTOK` et `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK` (E3-02). `make check-infra` le vérifie.

## Invariants vérifiés par `make check-infra`

Depuis le 7 novembre 2026 (change `deploy-nas`), `make check-infra`, donc le job `infra` de la CI, lance `infra/check-compose.sh` après les deux `docker compose config --quiet`. Le script lit chaque compose avec `--env-file /dev/null --no-interpolate`, dans un environnement vidé : aucune valeur de `infra/.env` ni du shell n'est chargée, et ses messages n'affichent jamais une valeur. Il lit aussi `tailscale/config/serve.json` tel quel. Un invariant cassé donne son propre message et fait échouer la vérification ; un rendu vide ou en erreur, une erreur de `jq`, un `serve.json` qui n'est pas un objet JSON ou un `.env.example` sans aucune variable la font échouer aussi, jamais un faux vert.

| Invariant | Compose | Pourquoi |
| --- | --- | --- |
| Exactement deux services, `hippo` et `tailscale` | production | Tout service ajouté passe par une relecture de ces invariants |
| Aucun port publié | production | Seul `tailscale serve` expose Hippo (ADR-0015) |
| Ports publiés sur `127.0.0.1` seulement | local | Jamais sur le réseau local |
| `hippo` dans le réseau du sidecar (`network_mode: service:tailscale`), avec `HTTP_ADDR=127.0.0.1:8080` | production | Sur une autre adresse, `:8080` serait joignable en HTTP depuis le tailnet, en contournant `serve` |
| `hippo` redémarré avec le sidecar : `depends_on.tailscale.restart: true` | production | Son réseau est celui du sidecar |
| Sidecar en mode noyau : `TS_USERSPACE=false`, pas de `userspace-networking` dans `TS_EXTRA_ARGS` ni `TS_TAILSCALED_EXTRA_ARGS`, `cap_add` exactement `[NET_ADMIN]`, seul périphérique `/dev/net/tun` | production | En mode userspace, Tailscale renvoie vers `127.0.0.1` une connexion du tailnet sur un port sans `serve`, ce qui exposerait `:8080` en clair. `NET_ADMIN` ne porte que sur le réseau du conteneur, pas sur le NAS |
| `TS_AUTH_ONCE=true` | production | La clé d'authentification n'enrôle le nœud qu'une fois ; consommée, elle peut quitter `infra/.env` |
| `serve` lit `./tailscale/config/serve.json` : `TS_SERVE_CONFIG=/config/serve.json`, et `./tailscale/config` monté sur `/config` | production | Le fichier vérifié à la ligne suivante est bien celui que lit le sidecar |
| `serve.json` : uniquement le port 443 en HTTPS (`TCP` vaut `{"443": {"HTTPS": true}}`, chaque hôte `Web` en `:443`), un seul handler, `{"Proxy": "http://127.0.0.1:8080"}`, toutes les valeurs d'`AllowFunnel` à `false`, aucun autre bloc que `TCP`, `Web` et `AllowFunnel` | `serve.json` | Hippo publié en HTTPS sur le tailnet seulement, jamais sur Internet (Funnel) ni sur un autre port (ADR-0015) |
| Ni `privileged`, ni `host` pour `network_mode`, `pid`, `ipc` ou `userns_mode`, ni volume qui monte `docker.sock`, ni `cap_add` hors du service `tailscale`, sur aucun service | les deux | Aucun service ne reçoit les privilèges ni les espaces de noms de l'hôte, ni la main sur le démon Docker ; seul le sidecar a une capacité, `NET_ADMIN`, limitée à son propre réseau. Les autres volumes montés (`./data`, `./tailscale/…`) ne sont pas vérifiés |
| Aucun `env_file` | les deux | Un `env_file: .env` ferait entrer tout `infra/.env` (dont `TS_AUTHKEY`) dans le conteneur, sans passer par la vérification des variables |
| Image Tailscale épinglée, `tailscale/tailscale:vX.Y.Z` (digest `@sha256:` accepté), jamais `latest` | production | Montée de version voulue, proposée par Dependabot (écosystème `docker-compose`) |
| Tag de l'image Hippo obligatoire : `${HIPPO_IMAGE_TAG:?…}` | production | Une ligne absente arrête `docker compose up` au lieu de tirer `latest` en silence |
| Rotation des logs : pilote `json-file` avec `max-size` et `max-file` (10 Mo, 3 fichiers), sur chaque service | les deux | Les logs ne remplissent jamais le volume du NAS |
| `mem_limit` sur `hippo` | les deux | ADR-0038 |
| Chaque variable non commentée de `.env.example` atteint `hippo` par `${NAME…}`, et chaque variable de `hippo` est dans `.env.example` | les deux | Voir « Règles critiques » |

Exceptions de la dernière ligne, listées dans le script :

- `TS_AUTHKEY` et `HIPPO_IMAGE_TAG` (`COMPOSE_ONLY_VARS`) : lues par Compose, pas par le backend ; elles n'atteignent pas `hippo`.
- `HTTP_ADDR` (`PROD_SET_VARS`) : fixée par le compose de production à `127.0.0.1:8080`, absente de `.env.example` exprès.

Une nouvelle variable de Compose, ou fixée par un compose, s'ajoute à ces listes dans le même commit. Pour voir un invariant échouer, sur une copie hors du dépôt : `make check-infra COMPOSE_DIR=<copie>`.

## Tailscale et exploitation

- La clé `TS_AUTHKEY` ne sert qu'une fois : avec `TS_AUTH_ONCE=true`, elle n'est utilisée que si `tailscale/state` ne contient pas encore de nœud. Une fois le nœud `hippo` enrôlé, Ghislain l'efface de `infra/.env` (`TS_AUTHKEY=` vide, autorisé par le compose). Clé à usage unique, pré-approuvée, non éphémère, sans tag.
- **Redémarrer le projet, jamais le conteneur `tailscale` seul** : il porte le réseau de `hippo`. Compose redémarre `hippo` avec lui (`depends_on` avec `restart: true`, Compose 2.17 ou plus) ; un redémarrage lancé hors de Compose, depuis l'interface Docker d'UGOS par exemple, peut le contourner.
- Procédure complète, vérifications et dépannage : [`docs/runbook-nas.md`](../docs/runbook-nas.md).

## Commandes (depuis la racine du dépôt)

```bash
make check-infra   # les deux compose (config --quiet), shellcheck et invariants (check-compose.sh)
make image         # construit l'image du backend
make up            # compose local, puis : curl -fsS http://127.0.0.1:8080/healthz
make logs
make down
```
