# infra/ — déploiement

Docker Compose sur le NAS, avec un sidecar Tailscale qui publie le backend en HTTPS sans port ouvert ([ADR-0015](../docs/adr/0015-reseau-tailscale.md), [ADR-0019](../docs/adr/0019-deploiement-docker-nas.md)). Le NAS se met à jour seul chaque nuit à 4 h (RG-99, ADR-0040, `hippo-update.sh`). Installer, mettre à jour, revenir en arrière sur le NAS : [`docs/runbook-nas.md`](../docs/runbook-nas.md).

## Fichiers

- `docker-compose.yml` : production (NAS) et Mac avec Tailscale ; image `ghcr.io/ghislain95/hippo`.
- `docker-compose.local.yml` : variante Mac **sans Tailscale** ; image construite depuis `../backend`, port publié sur `127.0.0.1:8080` uniquement.
- `.env.example` : toutes les variables, commentées. `.env` (non versionné) est créé par Ghislain ; **les agents ne le lisent jamais** (hook).
- `tailscale/config/serve.json` : configuration de `tailscale serve`.
- `check-compose.sh` : contrôle statique des deux compose, lancé par `make check-infra` (section suivante).
- `hippo-update.sh` : la mise à jour automatique de nuit, installée sur le NAS **hors du dossier du projet**, en `/usr/local/sbin/hippo-update`, et lancée par une ligne cron de root avec le dossier du projet en argument (section « Mise à jour automatique »). `hippo-update.test.sh` : sa suite de tests, un test par scénario de `openspec/specs/automatic-update/`.
- `data/` et `tailscale/state/` : données d'exécution, non versionnées.
- `backups/` : sauvegardes de la variante locale (`BACKUP_HOST_DIR` vide), créé par `make up`, non versionné. Sur le NAS, les sauvegardes vont dans le dossier partagé `Sauvegardes/hippo`, hors du dossier du projet ([ADR-0039](../docs/adr/0039-sauvegardes-sur-le-nas.md)).

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
| `hippo` monte un volume sur `/backups`, le dossier des sauvegardes | les deux | Sans lui, le service refuse de démarrer (`BACKUP_DIR` absent), ou les sauvegardes manquent |
| Ce volume lit `${BACKUP_HOST_DIR:?…}`, en syntaxe longue, avec `bind.create_host_path: false` | production | Une ligne absente de `.env` arrête `docker compose up` au lieu de sauvegarder dans le dossier du projet ; un dossier absent l'arrête aussi au lieu d'être créé par Docker au nom de root. La syntaxe courte est refusée : sans interpolation, Compose la coupe sur les `:` de `${…:?…}` et le script ne verrait plus de volume sur `/backups` |
| Chaque variable non commentée de `.env.example` atteint `hippo` par `${NAME…}`, et chaque variable de `hippo` est dans `.env.example` | les deux | Voir « Règles critiques » |
| Aucune variable de `COMPOSE_ONLY_VARS` dans l'`environment` de `hippo` | les deux | Elles ne servent qu'à Compose ; `TS_AUTHKEY` en particulier n'a rien à faire dans le conteneur du backend |

Exceptions de la dernière ligne, listées dans le script :

- `TS_AUTHKEY`, `HIPPO_IMAGE_TAG` et `BACKUP_HOST_DIR` (`COMPOSE_ONLY_VARS`) : lues par Compose, pas par le backend ; elles n'atteignent pas `hippo`.
- `DATA_DIR` et `BACKUP_DIR` : fixées par l'image (`/data`, `/backups`) et par `make dev`, commentées dans `.env.example`, jamais dans `infra/.env`.
- `HTTP_ADDR` (`PROD_SET_VARS`) : fixée par le compose de production à `127.0.0.1:8080`, absente de `.env.example` exprès.

Une nouvelle variable de Compose, ou fixée par un compose, s'ajoute à ces listes dans le même commit. Pour voir un invariant échouer, sur une copie hors du dépôt : `make check-infra COMPOSE_DIR=<copie>`.

## Mise à jour automatique (`hippo-update.sh`)

Change `nightly-update` (design D1 à D17, ADR-0040). Installé en `/usr/local/sbin/hippo-update` (root, `0700`), hors du partage UGOS qui contient le dossier du projet, et lancé par root à 4 h. Son état est aussi hors du partage, dans `/var/lib/hippo-update` (root, `0700`) : `lock`, `update.log` (+ `.1`), `run/` (copies de `docker-compose.yml` et `.env` le temps d'un passage), `staging/`, `previous/`, `installing`.

```
0 4 * * * root /usr/local/sbin/hippo-update /<volume>/docker/hippo >/dev/null 2>&1
```

Un passage, dans l'ordre : contrôle des droits (ci-dessous), verrou `flock`, frein à main (`HIPPO_IMAGE_TAG` autre que `latest` : `paused`), `docker pull` de `latest`, lecture de son label `org.opencontainers.image.revision`, téléchargement de `docker-compose.yml`, `tailscale/config/serve.json`, `.env.example` et du script **à ce commit** dans `/var/lib/hippo-update/staging/`, rien de nouveau (`up-to-date`), refus des fichiers qui ne vont pas avec `.env`, pull des autres images, sauvegarde par `docker compose exec -T hippo /hippo backup`, puis, point de non-retour : installation par renommage, `up -d`, attente de `healthy` sur la nouvelle image, et seulement après `updated`, remplacement de son propre fichier par le script téléchargé.

Règles du script, à garder à chaque modification :

- **Le dossier du projet est atteint par son inode, jamais par son chemin** (D16) : le script fait `cd -P` dans le dossier dès l'argument validé, puis n'utilise que des chemins relatifs pour ses propres lectures et écritures (`cmp`, `cp`, installation vers `./<fichier>`). Aucune variable `$HIPPO_DIR/…` après le `cd` (elle ne sert qu'aux messages). Il n'écrit dans le dossier du projet que les fichiers de déploiement qu'il installe ; tout le reste va dans le dossier d'état.
- **Compose ne reçoit que des copies, jamais un fichier du partage** (D17) : Compose ouvre ses fichiers par chemin absolu (vu par `strace`), même avec `-f ./docker-compose.yml`. Une fois le verrou pris, `./docker-compose.yml` et `./.env` sont copiés dans `/var/lib/hippo-update/run/` (`0600`), et chaque appel est `docker compose --project-directory . --env-file <état>/run/.env -f <état>/run/docker-compose.yml …`, ou `-f <état>/staging/docker-compose.yml` pour la version téléchargée, y compris `up -d` et les sondages après l'installation. `HIPPO_IMAGE_TAG` et les noms de variables sont lus dans la copie. `run/` est effacé à la fin de chaque passage, quelle que soit l'issue : la copie de `.env` ne vit sur le disque système que le temps du passage.
- **Le dossier du projet appartient à root seul** (D15) : l'argument est obligatoire et absolu. Avant toute écriture et tout `docker`, le dossier du projet, `tailscale/`, `tailscale/config/`, `docker-compose.yml`, `.env`, `.env.example`, `serve.json`, le dossier d'état et le fichier du script lui-même (ceux qui existent ; le dossier, le compose et `.env` sont obligatoires) ne doivent être ni un lien symbolique, ni à un autre uid que 0, ni modifiables par le groupe ou les autres, ni porteurs d'une ACL (`+` dans `ls -ldn`). Sinon : message sur stderr qui nomme le chemin et la cause, exit 1, aucun journal. Lecture par `ls -ldn`, jamais `stat`. Les dossiers parents ne sont pas vérifiés : les montages relatifs (`./data`, `./tailscale/config`, `./tailscale/state`) sont résolus par le démon sous le chemin du dossier, et un compte qui gagne la course de renommage à la création des conteneurs peut y substituer les siens (autres données, `serve.json` avec Funnel, autre identité du nœud ; pas de code root, D16). Le refus d'une ACL n'est pas testé par la suite (vérifié en recette).
- **`.env` n'est jamais sourcé ni évalué** : seuls la valeur de `HIPPO_IMAGE_TAG` et les noms de variables sont lus, avec `sed`. Le script n'écrit jamais `.env`, ne lance jamais `down`, `run` ni `--force-recreate`.
- **Journal en liste blanche** (`/var/lib/hippo-update/update.log`, `0600`, rotation au-delà de 1 Mio) : une ligne JSON par issue, construite avec des clés fixes (`time`, `msg`, `outcome`, `reason`, `old_revision`, `new_revision`, `backup`, `missing`, `duration_s`) dont chaque valeur est vérifiée contre sa forme, sinon `invalid`. Rien de ce qu'affichent `docker` ou `curl` n'y entre : leur sortie part dans `/dev/null` ou reste dans une variable analysée, jamais recopiée.
- **Avant le point de non-retour, rien n'est changé** et le tag local `latest` est remis comme avant ; après, rien n'est défait (pas de retour arrière automatique, ADR-0032).
- **Portable bash 3.2 et outils BSD** (la suite tourne sur le Mac) comme GNU/Debian : ni tableau associatif, ni `mapfile`, ni `sed -i`, ni `stat`, ni `readlink -f`, ni option propre à GNU. `set -u`, corps en fonctions, dernière ligne `main "$@"; exit $?` (bash a tout lu avant que le script se remplace). Aucune dépendance au-delà de `bash`, `docker compose`, `curl`, `flock`, `sed`, `cmp` : ni `jq` ni `python3` sur le NAS.
- **Environnement réservé aux tests** : `HIPPO_UPDATE_OWNER_UID` (uid attendu, 0 par défaut), `HIPPO_UPDATE_STATE_DIR` (dossier d'état, absolu, `/var/lib/hippo-update` par défaut), `HIPPO_UPDATE_HEALTH_TIMEOUT`, `HIPPO_UPDATE_HEALTH_INTERVAL`, `HIPPO_UPDATE_REPO_URL` ; cron n'en fixe aucun.
- **Tests** : `hippo-update.test.sh`, bash pur (pas de `bats`), faux `docker`, `curl`, `flock` et `ls` en tête de `PATH`, qui répondent depuis un dossier de scénario et journalisent leurs appels (le faux `ls` annonce un autre propriétaire pour un chemin donné) ; chaque test lance une **copie** du script, hors du dossier de projet temporaire, avec ce dossier en argument, un dossier d'état temporaire et `HIPPO_UPDATE_OWNER_UID` égal à l'uid de l'utilisateur qui lance les tests (le script remplace son propre fichier). Le faux `docker compose` se comporte comme Compose : il prend son répertoire dans `$PWD`, puis ouvre `-f` et `--env-file` par ce chemin ; un crochet de course échange les dossiers entre les deux. Avec l'échange définitif au `pull`, le test « A project folder swapped during the run is not followed » garde les correctifs D16 et D17. Les appels `t` suivent l'ordre des scénarios de la spec. Un scénario ajouté à la spec = un test nommé d'après lui. Ce que seuls les essais sur le NAS vérifient : le vrai déclenchement à 4 h et le fuseau, `flock`, `exec -T`, les identifiants d'image et le label de révision, GitHub et GHCR pour de vrai.
- **Publication** : `image.yml` publie depuis `main` seulement, et aussi quand l'un des quatre fichiers téléchargés change, pour qu'il arrive au NAS avec une image du même commit.

## Tailscale et exploitation

- La clé `TS_AUTHKEY` ne sert qu'une fois : avec `TS_AUTH_ONCE=true`, elle n'est utilisée que si `tailscale/state` ne contient pas encore de nœud. Une fois le nœud `hippo` enrôlé, Ghislain l'efface de `infra/.env` (`TS_AUTHKEY=` vide, autorisé par le compose). Clé à usage unique, pré-approuvée, non éphémère, sans tag.
- **Redémarrer le projet, jamais le conteneur `tailscale` seul** : il porte le réseau de `hippo`. Compose redémarre `hippo` avec lui (`depends_on` avec `restart: true`, Compose 2.17 ou plus) ; un redémarrage lancé hors de Compose, depuis l'interface Docker d'UGOS par exemple, peut le contourner.
- Procédure complète, vérifications et dépannage : [`docs/runbook-nas.md`](../docs/runbook-nas.md).

## Commandes (depuis la racine du dépôt)

```bash
make check-infra   # les deux compose (config --quiet), shellcheck, invariants (check-compose.sh), tests de hippo-update.sh
bash infra/hippo-update.test.sh   # les tests de la mise à jour automatique seuls (quelques secondes, sans Docker ni réseau)
make image         # construit l'image du backend
make up            # compose local, puis : curl -fsS http://127.0.0.1:8080/healthz
docker compose -f infra/docker-compose.local.yml exec hippo /hippo backup   # une sauvegarde dans infra/backups/
make logs
make down
```
