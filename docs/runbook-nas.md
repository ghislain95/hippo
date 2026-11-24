# Runbook NAS — installer, vérifier, mettre à jour Hippo

Procédure pour faire tourner Hippo sur le NAS (Ugreen DXP4800 Plus, UGOS Pro), le joindre en HTTPS par Tailscale, le sauvegarder, le mettre à jour (chaque nuit tout seul, ou à la main), revenir en arrière et restaurer une sauvegarde. Elle sert à Ghislain, seul, sans l'agent ([ADR-0015](adr/0015-reseau-tailscale.md), [ADR-0019](adr/0019-deploiement-docker-nas.md), [ADR-0038](adr/0038-limite-memoire-384-mo.md), [ADR-0039](adr/0039-sauvegardes-sur-le-nas.md), [ADR-0040](adr/0040-mise-a-jour-automatique-de-nuit.md)).

> **État** : écrite le 2026-11-07 par le change `deploy-nas` (E6-01, E6-02, E6-04), à suivre une première fois pendant sa QA. Les libellés des écrans d'UGOS Pro sont donnés à titre indicatif : la QA les corrige.
>
> **Sauvegardes** : ajoutées le 2026-11-12 par le change `nightly-backup` (E6-03), à suivre une première fois pendant sa QA. Hippo se sauvegarde chaque nuit et avant chaque mise à jour dans le dossier partagé `Sauvegardes/hippo` du NAS ; les sauvegardes ne quittent jamais le NAS (section 9). Une installation antérieure à ce change passe d'abord par la section 7, « Mise à jour qui installe les sauvegardes ».
>
> **Mise à jour automatique** : ajoutée le 2026-11-24 par le change `nightly-update` (E6-05), à suivre une première fois pendant sa QA. Chaque nuit à 4 h, le NAS installe seul la dernière version publiée, après une sauvegarde (section 11). Elle reste suspendue tant que `HIPPO_IMAGE_TAG` épingle un `sha-…`.

Conventions de ce document :

- Les commandes marquées « NAS » se tapent dans une session SSH sur le NAS ; celles marquées « Mac » dans un terminal du Mac.
- `<volume>`, `<tailnet>`, `<sha>`, `<tag>`, `<nom>` sont à remplacer. Rien de personnel (nom du tailnet, adresses, clés) ne s'écrit dans le dépôt, qui est public ([ADR-0027](adr/0027-depot-public.md)).
- Un **journal d'exploitation**, hors du dépôt (une note du gestionnaire de mots de passe convient), garde pour chaque installation ou mise à jour : la date, le SHA des fichiers récupérés, le tag de l'image, et ce qui a été fait.

## Ce qu'il faut

| Élément | Où | Coût |
| --- | --- | --- |
| UGOS Pro, appli Docker installée | NAS | gratuit |
| Accès SSH d'un compte administrateur (avec `sudo`) | NAS | gratuit |
| Compte Tailscale existant (le NAS et les iPhones y sont déjà), HTTPS activé | console Tailscale | gratuit |
| Tailscale sur l'iPhone et sur le Mac | iPhone, Mac | gratuit |
| Clé d'API Anthropic dédiée au NAS, crédits prépayés **sans recharge automatique** ([ADR-0021](adr/0021-garde-fous-couts.md)) | console Anthropic | la consommation (2 à 4 $ par mois attendus) |
| Gestionnaire de mots de passe | Mac, iPhone | — |

## 0. Prérequis et vérifications (une fois)

1. **SSH** : UGOS › Panneau de configuration › Terminal › activer SSH. Depuis le Mac :

   ```bash
   ssh <admin>@<adresse-locale-du-nas>
   ```

   `<admin>` : le compte administrateur d'UGOS. `<adresse-locale-du-nas>` : l'adresse qu'utilise le navigateur pour ouvrir l'interface d'UGOS, sans `http://` ni `:port` (par exemple `192.168.1.20`) ; sinon UGOS › Panneau de configuration › Réseau, ou la liste des appareils de la box. Hors de chez soi, le nom Tailscale du NAS (console Tailscale › Machines) marche aussi, Tailscale actif sur le Mac.

   SSH peut être coupé entre deux mises à jour : l'exploitation courante passe par l'interface Docker (section 6).

2. **Architecture et Docker** (NAS) :

   ```bash
   uname -m
   ```

   ```bash
   sudo docker compose version
   ```

   Attendu : `x86_64`, et Docker Compose **v2.17 ou plus récent** (le compose utilise `depends_on` avec `restart: true`). Plus ancien : mettre à jour l'appli Docker depuis le centre d'applications d'UGOS avant de continuer.

3. **Périphérique TUN** (NAS), nécessaire au conteneur Tailscale :

   ```bash
   ls -l /dev/net/tun
   ```

   Attendu : un périphérique `crw-…`. Absent : s'arrêter et le signaler (le Tailscale du NAS l'utilise d'ordinaire, il devrait exister).

4. **Volumes et dossiers partagés** (NAS) :

   ```bash
   ls -d /volume*/*/
   ```

   Repérer le dossier partagé qui accueillera Hippo (par exemple `/<volume>/docker`, créé par l'appli Docker), et celui de ses sauvegardes, `Sauvegardes` (créé à la section 1, point 4, s'il n'existe pas). Chacun doit être :
   - **réservé à l'administrateur** (aucun autre utilisateur, pas d'accès invité) : les sauvegardes contiennent tous les éléments, privés compris (RG-98) ;
   - **exclu de toute tâche de synchronisation ou de sauvegarde vers le cloud** d'UGOS (applis de synchronisation et de sauvegarde, libellés à confirmer pendant la QA) : la base, les images et leurs sauvegardes ne quittent jamais le NAS (RG-92). Il n'y a **aucune copie externe** ([ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)) ;
   - sur un **volume local** du NAS, pas sur un partage réseau monté : le verrou des sauvegardes n'y est pas fiable.

   Une tâche existante qui inclut l'un de ces dossiers : l'en exclure, ou s'arrêter et le signaler.

5. **Alimentation** : UGOS › Panneau de configuration › Matériel et alimentation. Aucune extinction ni mise en veille programmée entre 2 h et 4 h 30 : le batch de nuit (2 h), la sauvegarde de nuit (3 h) et la mise à jour automatique (4 h) manqués ne sont pas rattrapés, ils partent la nuit suivante (RG-30, RG-98, RG-99).

6. **Console Tailscale** ([login.tailscale.com/admin](https://login.tailscale.com/admin)) :
   - **Machines** : aucune machine ne s'appelle déjà `hippo` (par exemple un essai depuis le Mac). Sinon, la supprimer d'abord : la nouvelle s'appellerait `hippo-1`, et ce nom finirait dans les journaux publics de certificats et dans l'adresse compilée dans l'appli.
   - **DNS** : MagicDNS activé, **HTTPS Certificates** activé (fait en E1, voir le [runbook iOS](runbook-ios.md#2-tailscale-en-https-une-fois)). Ne rien changer d'autre dans le tailnet.

## 1. Dossier, droits et fichiers (une fois)

1. Choisir le dossier de Hippo et le SHA des fichiers à récupérer. Le SHA est celui d'un commit de `main` (page [Commits](https://github.com/ghislain95/hippo/commits/main), bouton « copier le SHA complet ») ; pendant la QA du change `deploy-nas`, le dernier commit de sa PR. Toujours un SHA, jamais un nom de branche : une branche fusionnée disparaît. Les variables ne vivent que le temps de la session SSH : les redéfinir à chaque connexion.

   NAS :

   ```bash
   HIPPO_DIR=/<volume>/docker/hippo
   ```

   ```bash
   SHA=<sha complet du commit>
   ```

2. Créer les dossiers et récupérer les trois fichiers versionnés (NAS) :

   ```bash
   sudo mkdir -p "$HIPPO_DIR/data" "$HIPPO_DIR/tailscale/state" "$HIPPO_DIR/tailscale/config" "$HIPPO_DIR/copies"
   ```

   ```bash
   cd "$HIPPO_DIR" && for f in docker-compose.yml tailscale/config/serve.json .env.example; do sudo curl -fsSL -o "$f" "https://raw.githubusercontent.com/ghislain95/hippo/$SHA/infra/$f" || echo "ÉCHEC $f"; done
   ```

3. Donner `data/` à l'utilisateur de l'image (uid 65532, non root) et fermer les dossiers sensibles (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo chown -R 65532:65532 data && sudo chmod 700 tailscale/state copies
   ```

   ```bash
   cd "$HIPPO_DIR" && sudo ls -ldn data tailscale/state copies
   ```

   Attendu : `data` appartient à `65532 65532` ; `tailscale/state` et `copies` en `drwx------`, sans `+` à la fin (un `+` signale une ACL d'UGOS qui ouvre le dossier à d'autres comptes). Si `data` n'appartient pas à 65532 (les droits des dossiers partagés d'UGOS peuvent l'emporter), s'arrêter : SQLite ne pourra pas créer la base. `data/` doit exister **avant** le premier démarrage : créé par Docker, il appartiendrait à root.

4. **Dossier des sauvegardes** (une fois, avant le `.env` de la section 2 et avant tout démarrage) : le compose refuse de démarrer si le dossier n'existe pas, et Hippo refuse de démarrer s'il ne peut pas y écrire.
   - Créer le dossier partagé `Sauvegardes` dans UGOS s'il n'existe pas : Panneau de configuration › Dossiers partagés › Créer (libellés indicatifs), sur le même volume que `docker`, réservé à l'administrateur, sans synchronisation ni sauvegarde cloud (section 0, point 4).
   - Vérifier qu'il existe (NAS) :

   ```bash
   sudo ls -ld /<volume>/Sauvegardes
   ```

   Attendu : un dossier. Absent : le créer dans UGOS d'abord ; un `mkdir` le créerait hors des dossiers partagés d'UGOS.

   - Créer le sous-dossier de Hippo, le donner à l'utilisateur de l'image (uid 65532) et le fermer aux autres. NAS :

   ```bash
   SAUVEGARDES=/<volume>/Sauvegardes/hippo
   ```

   ```bash
   sudo mkdir -p "$SAUVEGARDES" && sudo chown 65532:65532 "$SAUVEGARDES" && sudo chmod 750 "$SAUVEGARDES" && sudo ls -ldn "$SAUVEGARDES"
   ```

   Attendu : `drwxr-x---`, propriétaire `65532 65532`, **sans `+` à la fin**. Un `+` signale une ACL d'UGOS, qui peut ouvrir les sauvegardes à d'autres comptes ou empêcher Hippo d'y écrire : s'arrêter et le signaler. Comme `HIPPO_DIR`, la variable `SAUVEGARDES` se redéfinit à chaque session SSH.

5. Noter dans le journal : date, `SHA`.

## 2. Fichier `.env` (une fois)

1. Créer le fichier, lisible par root seul (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo cp .env.example .env && sudo chmod 600 .env
   ```

2. Générer le token d'API **propre au NAS** (pas celui du Mac). Mac :

   ```bash
   openssl rand -hex 32
   ```

   Le ranger dans le gestionnaire de mots de passe (« Hippo NAS — API_TOKEN ») : l'appli le recevra à son build (E5-02).

3. Choisir la version de Hippo à faire tourner. Le programme arrive sous forme d'**image Docker**, téléchargée depuis GitHub. Chaque version porte une étiquette (un **tag**) de la forme `sha-<7 caractères>` : les 7 premiers caractères du commit qui l'a construite. `latest` désigne toujours la dernière version publiée par `main`. À l'installation, le NAS épingle un `sha-…` précis : la mise à jour automatique reste suspendue (frein à main) jusqu'à ce que la section 11 passe à `latest`.
   - Ouvrir la page du paquet [ghcr.io/ghislain95/hippo](https://github.com/ghislain95/hippo/pkgs/container/hippo) et prendre le tag `sha-…` le plus récent.
   - Une image n'est construite que quand le backend ou un fichier de déploiement change (`docker-compose.yml`, `serve.json`, `.env.example`, `infra/hippo-update.sh`) : ce tag diffère donc, le plus souvent, du `SHA` de la section 1 (qui sert aux fichiers de configuration). C'est normal.
   - Vérifier que ce tag existe pour le processeur du NAS (amd64), en remplaçant `<tag>` (NAS) :

   ```bash
   sudo docker manifest inspect ghcr.io/ghislain95/hippo:<tag> | grep -c '"amd64"'
   ```

   Attendu : `1` (ou plus). Une erreur « no such manifest » : le tag n'existe pas.

4. Éditer le fichier (NAS, `vi` ou `nano` selon ce qu'UGOS propose) :

   ```bash
   sudo vi "$HIPPO_DIR/.env"
   ```

   | Variable | Valeur |
   | --- | --- |
   | `API_TOKEN` | le token du point 2 |
   | `ANTHROPIC_API_KEY` | **vide** en phase 1 (section 4) : rien ne peut partir vers l'IA |
   | `ANALYSIS_MODEL`, `ANALYSIS_INPUT_PRICE_USD_PER_MTOK`, `ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK` | défauts (Claude Haiku 4.5, 1 $ et 5 $ par million de jetons, vérifiés le 2026-11-07) ; à changer ensemble si le modèle change (RG-80) |
   | `BATCH_SCHEDULE` | `"0 2 * * *"` (heure de Paris) |
   | `BACKUP_SCHEDULE` | `"0 3 * * *"` (heure de Paris) ; seule la forme quotidienne `M H * * *` est acceptée, toute autre empêche Hippo de démarrer |
   | `BACKUP_HOST_DIR` | `/<volume>/Sauvegardes/hippo`, le dossier de la section 1, point 4 ; obligatoire, le compose refuse de démarrer sans. Jamais `BACKUP_DIR` ni `DATA_DIR` dans ce fichier : l'image les fixe |
   | `MONTHLY_BUDGET_USD` | `5` (au-delà, plus d'analyse jusqu'au 1er du mois suivant, RG-81) |
   | `TZ` | `Europe/Paris` |
   | `TS_AUTHKEY` | la clé de la section 3, puis **effacée** après l'enrôlement (section 4) |
   | `HIPPO_IMAGE_TAG` | `sha-<7 caractères>` du point 3 ; obligatoire, le compose refuse de démarrer sans. C'est aussi le frein à main de la mise à jour automatique : `latest` la met en marche, un `sha-…` la suspend (section 11) |

5. Vérifier que `.env` n'est lisible que par root (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo ls -ln .env
   ```

   Attendu : `-rw-------`, propriétaire `0 0`, sans `+` à la fin.

6. Noter dans le journal : le tag de l'image.

## 3. Clé d'authentification Tailscale (une fois)

Console Tailscale › Settings › Keys › **Generate auth key** :

- **Reusable** : non (usage unique) ;
- **Ephemeral** : non (le nœud doit survivre à un redémarrage) ;
- **Pre-approved** : oui, si l'approbation des appareils est activée sur le tailnet ;
- **Tags** : aucun (pas d'ACL à toucher) ;
- **Expiration** : 1 jour suffit, la clé sert dans l'heure.

La coller dans `TS_AUTHKEY` du `.env`. Elle ne sert qu'au premier démarrage : le compose demande à Tailscale de ne l'utiliser qu'une fois (`TS_AUTH_ONCE`), puis l'identité du nœud vit dans `tailscale/state`.

## 4. Premier démarrage, phase 1 : sans clé Anthropic

1. Tirer les images et démarrer (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose pull && sudo docker compose up -d
   ```

   Un tag absent fait échouer le `pull` (« manifest unknown ») : revoir la section 2, point 3.

2. Attendre une minute, puis vérifier l'état (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose ps
   ```

   Attendu, colonne STATUS : `Up …` pour `hippo-tailscale-1`, `Up … (healthy)` pour `hippo-hippo-1`.

3. Lire les logs de Hippo (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose logs --tail 30 hippo
   ```

   Attendu, en JSON, une ligne par évènement :
   - `"msg":"hippo started"` avec `"version"` : le SHA complet, qui **commence par** les 7 caractères du tag ;
   - `"msg":"analysis prices"` avec le modèle et les deux prix ;
   - `"msg":"no ANTHROPIC_API_KEY: the nightly analysis sends nothing"` (attendu en phase 1) ;
   - `"msg":"backup scheduled"` avec `"backup_schedule"` et `"next_run"` : la prochaine sauvegarde de nuit, en UTC.

   Aucune ligne ne contient de token, de clé ni de contenu d'élément.

4. Lire les logs de Tailscale (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose logs --tail 50 tailscale
   ```

   Chercher la connexion réussie et la prise en compte de `serve.json`. La console Tailscale montre une nouvelle machine `hippo`, connectée.

5. **Désactiver l'expiration de clé** du nœud : console Tailscale › Machines › `hippo` › ⋯ › **Disable key expiry** (réglage de la machine, pas du tailnet). Sans ça, Hippo décroche du tailnet au bout de 180 jours.

6. **Effacer la clé consommée** : dans `.env`, laisser `TS_AUTHKEY=` vide, puis (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose up -d
   ```

   Le conteneur Tailscale est recréé, Hippo le suit. Vérifier dans la console que `hippo` reste connecté, **sans** nouvelle machine `hippo-1`. Si ce n'est pas le cas, voir la section 10.

7. **Depuis l'iPhone** : LocalDevVPN (le VPN de SideStore) coupé, Tailscale actif ([ADR-0013](adr/0013-distribution-ios-gratuite.md) : iOS n'a qu'un VPN à la fois). Dans Safari, ouvrir `https://hippo.<tailnet>.ts.net/healthz`. Attendu : `{"status":"ok","version":"<SHA complet>"}`. Le tout premier appel peut prendre une dizaine de secondes, le temps que Tailscale obtienne le certificat : recharger.

8. **Depuis le Mac** (« Use Tailscale DNS » actif dans Tailscale, voir le [runbook iOS](runbook-ios.md#2-tailscale-en-https-une-fois)) : une capture authentifiée, une note fictive **marquée privée** (jamais envoyée à l'IA, RG-12). Ces commandes se tapent dans un terminal **du Mac**, pas dans la session SSH : le NAS lui-même ne résout pas les noms `*.ts.net`. Le token est lu sans écho, pour ne pas finir dans l'historique du shell : la commande `read` attend qu'on colle le token (rien ne s'affiche) puis qu'on appuie sur `Entrée`. Mac :

   ```bash
   read -rs HIPPO_TOKEN && export HIPPO_TOKEN
   ```

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -H 'Content-Type: application/json' -d '{"note":"Note fictive de test d installation","is_private":true}'
   ```

   Attendu : `HTTP 201` et l'élément en JSON (`"status":"queued"`, `"is_private":true`). Sans token, ou avec un mauvais token : `HTTP 401`.

9. **Le port en clair est fermé** (Mac). Les deux commandes doivent **échouer** (connexion refusée ou délai dépassé) :

   ```bash
   curl -m 5 http://hippo.<tailnet>.ts.net:8080/healthz
   ```

   ```bash
   curl -m 5 http://hippo.<tailnet>.ts.net/healthz
   ```

10. **Redémarrages** :
    - redémarrer le projet (NAS) puis refaire le point 7 :

      ```bash
      cd "$HIPPO_DIR" && sudo docker compose restart
      ```

    - redémarrer le NAS (UGOS › menu › Redémarrer), attendre, puis refaire le point 7 : les deux conteneurs repartent seuls (`restart: unless-stopped`).

## 5. Phase 2 : clé Anthropic et test de bout en bout

1. Console Anthropic : une clé **dédiée au NAS** (nommée par exemple `hippo-nas`), sur le compte aux crédits prépayés sans recharge automatique. La ranger dans le gestionnaire de mots de passe.
2. La poser dans `ANTHROPIC_API_KEY` du `.env`, puis (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose up -d && sleep 20 && sudo docker compose logs --tail 20 hippo
   ```

   Attendu : `hippo started`, et **plus** la ligne `no ANTHROPIC_API_KEY`.

3. **Test payant de bout en bout** (accepté le 2026-11-07, moins de 0,01 $) : un lien public et une image, tous deux fictifs, partent dans le batch de 2 h. Chaque capture porte un `client_id` fixe, pour la relire le lendemain. Mac :

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -H 'Content-Type: application/json' -d '{"url":"https://fr.wikipedia.org/wiki/SQLite","client_id":"01K9NASTEST000000000000001"}'
   ```

   Pour l'image, une capture d'écran sans rien de personnel (par exemple de cette même page), enregistrée sous `~/Desktop/test-hippo.png`. Mac :

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -F image=@"$HOME/Desktop/test-hippo.png" -F client_id=01K9NASTEST000000000000002
   ```

   Attendu : `HTTP 201` pour les deux.

4. **Le lendemain matin**, les logs de la nuit (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose logs --since 24h hippo | grep -E '"analysis (pass|follow-up)"'
   ```

   Attendu : une ligne `analysis pass` vers 2 h avec `"submitted":2` et `"images_without_copy":0` (son `duration_ms` mesure l'envoi, copie de l'image comprise) ; puis une ligne `analysis follow-up` avec `"analyzed":2` et `cost_usd`. Trois éléments existent (la note privée de la phase 1 en plus) et deux seulement sont partis : la preuve, en vrai, qu'un élément privé n'est pas envoyé (RG-12). Relire chaque élément en **rejouant** sa capture avec le même `client_id` (RG-08) : la réponse `HTTP 200` porte `"status":"analyzed"`, le type, la catégorie, les tags et la fiche. Mac :

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -H 'Content-Type: application/json' -d '{"url":"https://fr.wikipedia.org/wiki/SQLite","client_id":"01K9NASTEST000000000000001"}'
   ```

   Pour l'image, rejouer la commande `-F` du point 3 telle quelle. La console Anthropic › Coût, filtrée sur la clé, montre la dépense quelques heures plus tard.

5. Noter dans le journal : `duration_ms`, `cost_usd`, et le constat (fiches lisibles ou non).

6. **Repartir d'une base vierge**, pour que les éléments fictifs ne restent pas dans la bibliothèque (la suppression d'un élément arrive avec E4). La dépense du mois comptée par Hippo pour le budget (RG-81) repart aussi de zéro : moins de 0,01 $ d'écart avec la console Anthropic, sans effet. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose stop hippo && sudo mv -n data "copies/data-test-$(date +%F-%H%M)" && sudo mkdir data && sudo chown 65532:65532 data && sudo docker compose up -d
   ```

   Vérifier `/healthz` depuis l'iPhone, puis supprimer la copie de test quand tout va bien :

   ```bash
   sudo rm -r "$HIPPO_DIR/copies/data-test-<date>"
   ```

## 6. Au quotidien : l'interface Docker d'UGOS

- **Logs** : appli Docker › Conteneurs › `hippo-hippo-1` › Journal. Une ligne JSON par évènement (`time`, `level`, `msg`, puis des identifiants et des compteurs) ; jamais de secret ni de contenu d'élément. Les logs tournent seuls (3 fichiers de 10 Mo par conteneur).
- **Santé** : la colonne d'état montre `healthy`. Limite connue : la sonde interroge Hippo de l'intérieur ; elle reste verte même si le nœud Tailscale est hors ligne. Le vrai test reste `/healthz` depuis l'iPhone.
- **Redémarrer** : redémarrer **le projet** (les deux conteneurs), ou `hippo-hippo-1` seul. Depuis la mise à jour automatique, le projet a été créé à partir de fichiers de `/var/lib/hippo-update` (section 11) : si l'appli refuse de redémarrer le projet, le faire en SSH, `cd "$HIPPO_DIR" && sudo docker compose restart`. **Jamais `hippo-tailscale-1` seul** : il porte le réseau de Hippo, qui perdrait sa connexion sans que sa sonde ne le voie.
- **Sauvegardes** : dans le journal de `hippo-hippo-1`, la ligne `backup scheduled` au démarrage (prochaine sauvegarde de nuit), puis chaque nuit une ligne `backup` avec son `outcome` (section 9). La sortie d'un `docker compose exec hippo /hippo backup` ne va pas dans ce journal : elle s'affiche dans la session SSH.
- **Mise à jour automatique** : son journal n'est pas dans l'interface Docker ; il se lit en SSH (section 11).
- **Ne pas modifier** le compose ni les variables depuis l'interface : la référence est le dossier `$HIPPO_DIR` et ce runbook.

## 7. Mettre à jour

La mise à jour automatique (section 11) fait tout ceci seule, chaque nuit. La mise à jour à la main reste possible, par exemple pour installer une version précise :

- Avec `HIPPO_IMAGE_TAG=latest` dans `.env`, **serrer d'abord le frein à main** (section 11) : sinon un passage de nuit pourrait se mêler à la mise à jour en cours.
- Le point 4 met un `sha-…` dans `HIPPO_IMAGE_TAG` : la mise à jour automatique reste suspendue après la mise à jour. Pour la reprendre : section 11, « Mettre en marche ».

1. Relever dans le journal le `SHA` et le tag en place, choisir les nouveaux (sections 1 et 2).
2. **Une migration entre les deux ?** Ouvrir `https://github.com/ghislain95/hippo/compare/<ancien SHA>...<nouveau SHA>` et chercher un fichier ajouté dans `backend/internal/store/migrations/`. Le noter : il change le retour arrière (section 8).
3. Récupérer les fichiers au nouveau SHA (section 1, point 2), puis comparer les variables. NAS :

   ```bash
   cd "$HIPPO_DIR" && diff <(sudo sed -n 's/^\([A-Z_]*\)=.*/\1/p' .env | sort) <(sed -n 's/^\([A-Z_]*\)=.*/\1/p' .env.example | sort)
   ```

   Une variable présente seulement à droite est nouvelle : l'ajouter au `.env` avec sa valeur commentée dans `.env.example`. Une montée de l'image Tailscale (proposée par Dependabot dans le dépôt) arrive par ce même `docker-compose.yml`.

4. Mettre le nouveau tag dans `HIPPO_IMAGE_TAG`, puis tirer les images à l'avance (Hippo tourne encore). NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose pull
   ```

5. **Sauvegarder, Hippo en marche**, avec la version en place. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose exec hippo /hippo backup; echo "code de sortie : $?"
   ```

   Attendu : une ligne JSON `"msg":"backup"` avec `"outcome":"ok"` et `"backup":"<nom>"`, puis `code de sortie : 0`. Cette sortie ne va **pas** dans les logs Docker : c'est ici qu'on la lit. Noter `<nom>` dans le journal. Un autre code, ou un autre `outcome` : ne pas mettre à jour, voir la section 10.

   - Toujours `exec`, jamais `run` : `run` lancerait un conteneur neuf de l'image du nouveau tag (point 4), et la sauvegarde ne serait pas celle de la version en place.
   - Jamais `-u root` : des fichiers de la base (`-wal`, `-shm`) pourraient appartenir à root, et Hippo ne pourrait plus les ouvrir.
   - Si une sauvegarde est déjà en cours (celle de la nuit), la commande l'attend jusqu'à 10 minutes (ligne `waiting for the backup lock`), puis échoue avec `"outcome":"locked"`.
   - Les captures faites entre cette sauvegarde et le point 6 n'y sont pas : ne rien capturer pendant la mise à jour.

6. Démarrer la nouvelle version (NAS) :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose up -d
   ```

7. Vérifier : `cd "$HIPPO_DIR" && sudo docker compose ps` (`healthy`), `/healthz` depuis l'iPhone avec la nouvelle `version`. Les migrations s'appliquent seules au démarrage.
8. Noter dans le journal. Les sauvegardes anciennes s'effacent seules (section 9).

### Mise à jour qui installe les sauvegardes (une fois)

La première mise à jour vers une version qui contient le change `nightly-backup` diffère : l'image en place n'a pas encore la commande `backup`, et le nouveau compose exige le dossier des sauvegardes.

1. Points 1 à 3 ci-dessus. Le `diff` du point 3 montre `BACKUP_HOST_DIR` : ne pas l'ajouter tout de suite.
2. Créer et vérifier le dossier des sauvegardes : section 1, point 4. **Avant** la ligne `BACKUP_HOST_DIR` et avant tout démarrage : le compose refuse un dossier absent (« bind source path does not exist »), et Hippo refuse de démarrer s'il ne peut pas y écrire.
3. Ajouter au `.env` la ligne `BACKUP_HOST_DIR=/<volume>/Sauvegardes/hippo` (section 2, point 4). Sans elle, toute commande `docker compose` s'arrête sur « set BACKUP_HOST_DIR in infra/.env ».
4. Point 4 ci-dessus (tag, `pull`).
5. À la place des points 5 et 6, **copier les données à la main, Hippo arrêté** (la base est en WAL : ses fichiers `-wal` et `-shm` partent avec elle), puis démarrer la nouvelle version. La copie porte la date et l'heure ; si elle existe déjà, `mkdir` échoue avant l'arrêt et rien ne change. NAS :

   ```bash
   cd "$HIPPO_DIR" && C="copies/data-$(date +%F-%H%M)" && sudo mkdir "$C" && sudo docker compose stop hippo && sudo cp -a data/. "$C"/ && echo "copie : $C" && sudo docker compose up -d
   ```

   Noter le nom de la copie dans le journal. Pour revenir en arrière depuis cette mise à jour : section 8, « Depuis une copie manuelle ».

6. Point 7 ci-dessus, et dans les logs la ligne `backup scheduled` (section 4, point 3).
7. Une première sauvegarde, avec la commande du point 5 ci-dessus : elle copie toutes les images, c'est la plus longue. Pendant la QA du change `nightly-backup`, garder `sudo docker stats` ouvert dans une autre session SSH et noter le pic de mémoire de `hippo-hippo-1` et la durée (`duration_ms`).
8. Le lendemain matin, la sauvegarde de la nuit (section 9). Supprimer la copie manuelle quand tout va bien.

## 8. Revenir en arrière

Après une mise à jour automatique, commencer par « Après une mise à jour automatique » ci-dessous : elle donne l'ancien tag et la sauvegarde. Après une mise à jour à la main, ils sont dans le journal d'exploitation.

- **Sans migration entre les deux versions** : remettre l'ancien tag dans `HIPPO_IMAGE_TAG` (et les anciens fichiers, section 1 avec l'ancien `SHA`, s'ils ont changé), puis `sudo docker compose up -d`.
- **Avec une migration** : l'ancienne image démarrerait sans erreur sur le nouveau schéma, qu'elle ne connaît pas (goose ignore les versions inconnues). Il faut restaurer la sauvegarde faite avant la mise à jour (section 7, point 5, nom noté dans le journal) : « Restaurer une sauvegarde » ci-dessous. Son manifeste donne l'ancien tag.
- **Depuis une copie manuelle** de `copies/` (mise à jour qui installe les sauvegardes). NAS :

  ```bash
  cd "$HIPPO_DIR" && sudo docker compose stop hippo && sudo mv -n data "copies/data-abandonnee-$(date +%F-%H%M)" && sudo cp -a "copies/<nom de la copie, noté dans le journal>" data && sudo ls -ldn data
  ```

  Attendu : `data` appartient à `65532 65532` (`cp -a` garde le propriétaire). Puis remettre l'ancien tag et les anciens fichiers, et `sudo docker compose up -d`. Les captures faites depuis la copie sont perdues (elles restent dans `copies/data-abandonnee-…`).

### Après une mise à jour automatique

Le retour arrière reste à la main : la mise à jour automatique ne revient jamais en arrière seule ([ADR-0040](adr/0040-mise-a-jour-automatique-de-nuit.md)).

1. Lire la ligne de la mise à jour dans le journal de mise à jour (NAS) :

   ```bash
   sudo tail -n 5 /var/lib/hippo-update/update.log
   ```

   Prendre la dernière ligne `"outcome":"updated"`, ou `"outcome":"failed"` avec `"reason":"up_failed"` ou `"unhealthy"`. Noter dans le journal d'exploitation :
   - `old_revision` : le commit de la version d'avant. Son tag est `sha-` suivi de ses 7 premiers caractères ;
   - `new_revision` : le commit de la version installée ;
   - `backup` : le nom de la sauvegarde faite juste avant, avec la version d'avant.

   `old_revision` vaut `unknown` (image d'avant sans étiquette de révision) : prendre l'ancien tag dans le manifeste de la sauvegarde (« Restaurer une sauvegarde », point 2).

2. **Serrer le frein à main sur l'ancienne version**, en remplaçant `<7 caractères>` par le début d'`old_revision`. Sans lui, la nuit suivante réinstallerait la nouvelle version. NAS :

   ```bash
   sudo sed -i 's/^HIPPO_IMAGE_TAG=.*/HIPPO_IMAGE_TAG=sha-<7 caractères>/' "$HIPPO_DIR/.env" && sudo grep -n '^HIPPO_IMAGE_TAG=' "$HIPPO_DIR/.env"
   ```

   Attendu : une seule ligne, `HIPPO_IMAGE_TAG=sha-…` avec le bon tag. Vérifier que ce tag existe (section 2, point 3).

3. **Une migration entre les deux ?** Section 7, point 2, avec `old_revision` et `new_revision`.
4. **Sans migration** : remettre les fichiers d'avant. Le script en garde une copie dans `/var/lib/hippo-update/previous/` (`sudo ls -lR /var/lib/hippo-update/previous`), mais seulement ceux d'avant le **dernier** passage qui a installé : si une autre nuit a retenté après l'échec, `previous/` contient déjà les nouveaux fichiers. Les fichiers du commit `old_revision` sont toujours les bons. Le plus simple : la section 1, point 2, avec ce commit (NAS) :

   ```bash
   SHA=<old_revision, 40 caractères>
   ```

   Puis la commande `curl` de la section 1, point 2, et `cd "$HIPPO_DIR" && sudo docker compose up -d`. Vérifier : section 7, point 7.
5. **Avec une migration** : restaurer la sauvegarde nommée par `backup` (« Restaurer une sauvegarde » ci-dessous, à partir du point 3 ; le tag est déjà en place).
6. Noter dans le journal d'exploitation. Le frein à main reste serré jusqu'à ce qu'une version corrigée soit publiée sur `main` ; ensuite, section 11, « Mettre en marche ».

### Restaurer une sauvegarde

Restaurer une sauvegarde rend les éléments supprimés depuis et perd les captures faites depuis (RG-98) ; ces captures restent dans le `data/` mis de côté. Hippo n'a pas de commande de restauration : tout se fait à la main.

1. Choisir la sauvegarde (NAS, `SAUVEGARDES` défini à la section 1, point 4) :

   ```bash
   sudo ls "$SAUVEGARDES"
   ```

   Chaque nom est l'heure de Paris du début de la sauvegarde, avec son décalage (`2026-11-13T030000+0100`). Jamais un dossier `.partial-…` : c'est une sauvegarde inachevée.

2. Lire son manifeste (NAS) :

   ```bash
   sudo cat "$SAUVEGARDES/<nom>/manifest.json"
   ```

   `backend_version` est la version qui l'a faite ; le tag de son image est `sha-` suivi des 7 premiers caractères de `backend_version`. Vérifier que ce tag existe (section 2, point 3). `migration_version` est la dernière migration de la base sauvegardée.

3. Arrêter Hippo, mettre `data/` de côté et créer un `data/` vide. Si `data/` n'a pas bougé, `mkdir` échoue et rien n'est écrasé. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose stop hippo && sudo mv -n data "copies/data-abandonnee-$(date +%F-%H%M)" && sudo mkdir data
   ```

4. **Copier** la base et les images de la sauvegarde dans `data/`, puis le donner à l'utilisateur de l'image. Toujours `cp -a`, **jamais `mv` ni `ln`** : déplacer viderait la sauvegarde, et un lien ferait écrire Hippo dans la sauvegarde et dans toutes celles qui partagent ses images. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo cp -a "$SAUVEGARDES/<nom>/hippo.db" "$SAUVEGARDES/<nom>/assets" data/ && sudo chown -R 65532:65532 data && sudo ls -ln data
   ```

   Attendu : `hippo.db` et `assets`, propriétaire `65532 65532`, rien d'autre. **Aucun** `hippo.db-wal` ni `hippo.db-shm` : une sauvegarde n'en contient pas, et un tel fichier venu d'une autre base abîmerait celle-ci. Hippo repasse la base en mode WAL au démarrage.

5. Mettre dans `HIPPO_IMAGE_TAG` le tag du point 2 (et les fichiers du SHA correspondant, section 1, s'ils ont changé), puis démarrer. Ce tag épinglé serre aussi le frein à main de la mise à jour automatique (section 11). NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose up -d
   ```

6. Vérifier : `cd "$HIPPO_DIR" && sudo docker compose ps` (`healthy`), puis `/healthz` depuis l'iPhone : sa `version` commence par les 7 caractères du tag.
7. Noter dans le journal : la sauvegarde restaurée, le tag, le nom du `data/` mis de côté. Le supprimer quand tout va bien : `sudo rm -r "$HIPPO_DIR/copies/data-abandonnee-<date>"`.

### Tester une restauration

À faire une fois, pendant la QA du change `nightly-backup` ([ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)) : sur une base de test, le vrai `data/` mis de côté puis remis en place. Hors de 2 h à 4 h 30 (batch, sauvegarde et mise à jour de nuit) ; aucune capture réelle pendant le test : elle finirait dans la base de test.

1. Mettre le vrai `data/` de côté et démarrer sur un `data/` vide (NAS) :

   ```bash
   cd "$HIPPO_DIR" && R="copies/data-reelle-$(date +%F-%H%M)" && sudo docker compose stop hippo && sudo mv -n data "$R" && sudo mkdir data && sudo chown 65532:65532 data && echo "vrai data : $R" && sudo docker compose up -d
   ```

   Noter le nom du vrai `data/` dans le journal.

2. Deux captures fictives **privées** (jamais envoyées à l'IA, RG-12), chacune avec un `client_id` fixe, depuis le Mac (`HIPPO_TOKEN` comme à la section 4, point 8 ; l'image de la section 5, point 3) :

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -H 'Content-Type: application/json' -d '{"note":"Note fictive de test de restauration","is_private":true,"client_id":"01K9NASREST000000000000001"}'
   ```

   ```bash
   curl -sS -w '\nHTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/items -H "Authorization: Bearer $HIPPO_TOKEN" -F image=@"$HOME/Desktop/test-hippo.png" -F is_private=true -F client_id=01K9NASREST000000000000002
   ```

   Attendu : `HTTP 201` pour les deux. Noter l'`id` de chaque élément et, pour l'image, l'`id` de son image (`assets`).

3. Une sauvegarde de cette base : section 7, point 5. Noter `<nom>`.
4. La restaurer : « Restaurer une sauvegarde », points 3 à 6 (le tag ne change pas : `backend_version` est la version en place).
5. Vérifier depuis le Mac. Rejouer chaque capture du point 2 telle quelle : `HTTP 200` (et non `201`) avec le même `id` (RG-08). Puis l'image :

   ```bash
   curl -sS -o "$HOME/Desktop/test-hippo-restauree.png" -w 'HTTP %{http_code}\n' https://hippo.<tailnet>.ts.net/api/assets/<id-image> -H "Authorization: Bearer $HIPPO_TOKEN" && cmp "$HOME/Desktop/test-hippo.png" "$HOME/Desktop/test-hippo-restauree.png" && echo identique
   ```

   Attendu : `HTTP 200` puis `identique`.

6. Remettre le vrai `data/`. Le test `[ ! -e data ]` empêche de ranger le vrai `data/` **dans** un `data/` resté en place. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo docker compose stop hippo && sudo mv -n data "copies/data-test-restauration-$(date +%F-%H%M)" && [ ! -e data ] && sudo mv -n "copies/<vrai data, noté au point 1>" data && sudo ls -ldn data && sudo docker compose up -d
   ```

   Attendu : `data` appartient à `65532 65532`. Puis `/healthz` depuis l'iPhone.

7. Nettoyer : les deux bases de test, `copies/data-abandonnee-<date>` (mise de côté au point 4) et `copies/data-test-restauration-<date>` (`sudo rm -r "$HIPPO_DIR/copies/<nom>"`), puis la sauvegarde de test (`sudo rm -r "$SAUVEGARDES/<nom>"`) ; sans elle, la sauvegarde suivante lie de nouveau ses images à la dernière vraie sauvegarde. Sur le Mac, `test-hippo-restauree.png`. Noter le résultat dans le journal.

## 9. Sauvegardes

Chaque nuit à `BACKUP_SCHEDULE` (3 h, heure de Paris), et avant chaque mise à jour (section 7, point 5, ou par la mise à jour automatique, section 11), Hippo copie sa base et ses images dans `/<volume>/Sauvegardes/hippo` (`BACKUP_HOST_DIR`) ([RG-98](metier.md#données-et-multi-utilisateur), [ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)). Une nuit manquée (NAS éteint) n'est pas rattrapée.

**Ce qu'une sauvegarde contient** : un dossier par sauvegarde, nommé d'après l'heure de Paris de son début (`2026-11-13T030000+0100`), avec :

- `hippo.db` : une copie cohérente de la base, faite Hippo en marche, sans rien y écrire ;
- `assets/` : les images ; une image inchangée depuis la sauvegarde précédente n'est pas recopiée, mais liée en dur (le même fichier sur le disque) ;
- `manifest.json` : versions, nombres, tailles ; aucun nom de fichier, aucun contenu d'élément.

Tous les éléments y sont, privés compris, sur le NAS seulement. Un élément supprimé reste dans les sauvegardes jusqu'à leur effacement (RG-98). Ni `tailscale/state` (le nœud se réenrôle avec une clé neuve, section 10), ni `.env` (ses secrets sont dans le gestionnaire de mots de passe) ne sont sauvegardés.

Le dossier contient aussi `.lock` (le verrou, à laisser) et, pendant une sauvegarde, un dossier `.partial-<nom>`, renommé à la fin. **Ne jamais modifier un fichier d'une sauvegarde** : une image liée est la même dans toutes les sauvegardes qui la contiennent. Supprimer un dossier de sauvegarde entier ne touche pas les autres.

**Conservation** : après chaque sauvegarde réussie, celles de plus de 14 jours sont effacées, sauf les 3 plus récentes, toujours gardées. Seuls les dossiers nommés comme une sauvegarde sont effacés ; rien d'autre du dossier n'est touché.

**Place occupée** : à peu près les images une fois, plus une base par sauvegarde (14 à 28 bases). NAS :

```bash
sudo du -sh "$SAUVEGARDES"
```

`du` compte une seule fois un fichier lié dans plusieurs sauvegardes ; l'explorateur de fichiers d'UGOS peut l'additionner à chaque sauvegarde et afficher plus.

**Lire la sauvegarde de la nuit**, le matin (NAS) :

```bash
cd "$HIPPO_DIR" && sudo docker compose logs --since 24h hippo | grep -E '"msg":"backup|"schedule":"backup"'
```

Attendu : une ligne `backup` vers 3 h avec `"trigger":"nightly"` et `"outcome":"ok"`. Ses champs : `backup` (le nom), `backend_version`, `migration_version`, `database_bytes`, `images`, `images_copied`, `images_linked`, `images_gone` (une image supprimée pendant la sauvegarde : sans gravité), `image_bytes_written`, `duration_ms`, `backups_removed`, `leftovers_removed`. Les autres `outcome` :

| `outcome` | Sens |
| --- | --- |
| `skipped` | la sauvegarde de nuit a trouvé une autre sauvegarde en cours (`hippo backup`) : rien d'écrit, la suivante part la nuit prochaine |
| `locked` | `hippo backup` a attendu 10 minutes une sauvegarde en cours |
| `no_room` | pas assez de place sur le volume : rien d'écrit |
| `name_taken` | une sauvegarde porte déjà ce nom (deux sauvegardes dans la même seconde) |
| `failed` | erreur, lue dans `error` (jamais un chemin) ; rien ne reste de cette sauvegarde |
| `stopped` | Hippo s'est arrêté pendant la sauvegarde ; rien ne reste de cette sauvegarde |

Une ligne `backup retention` (`error`) signale un effacement impossible : la sauvegarde existe quand même.

**Risques acceptés** par Ghislain le 12 novembre 2026 ([ADR-0039](adr/0039-sauvegardes-sur-le-nas.md)) : un vol, un incendie, une surtension ou un rançongiciel qui atteint le NAS emporte Hippo et ses sauvegardes ensemble ; une image abîmée sur le disque l'est dans toutes les sauvegardes qui la lient. Le RAID 1 couvre la panne d'un disque, pas ces risques.

## 10. En cas de problème

| Symptôme | Piste |
| --- | --- |
| `docker compose` : « set HIPPO_IMAGE_TAG in infra/.env » ou « set API_TOKEN… » | variable vide ou absente dans `.env` (section 2) |
| `docker compose` : « set BACKUP_HOST_DIR in infra/.env » | `BACKUP_HOST_DIR` vide ou absente dans `.env` : section 1 point 4 (dossier), puis section 2 |
| `up` : « bind source path does not exist » | le dossier de `BACKUP_HOST_DIR` n'existe pas, ou le chemin est mal recopié : `sudo ls -ldn "$SAUVEGARDES"`, section 1 point 4. Docker ne le crée pas, exprès |
| `hippo-hippo-1` redémarre en boucle, log « hippo: backup folder: … » (« permission denied », « no such file or directory », « not a directory ») | Hippo refuse de démarrer sans un dossier des sauvegardes où il peut écrire : propriétaire 65532 et absence de `+` (`sudo ls -ldn "$SAUVEGARDES"`, section 1 point 4) |
| Log « BACKUP_DIR must be outside DATA_DIR… » ou « BACKUP_SCHEDULE: … » au démarrage | `BACKUP_DIR` ou `DATA_DIR` changées dans l'image ou dans le compose à la main (le compose ne lit pas ces variables dans `.env`) : reprendre les fichiers au SHA (section 1) ; `BACKUP_SCHEDULE` qui n'est pas de la forme `M H * * *` : la corriger (section 2) |
| `hippo backup` ou la ligne de nuit : `"outcome":"no_room"` | le volume est presque plein ; rien n'a été écrit. Libérer de la place (`df -h /<volume>`) ; une vieille sauvegarde peut s'effacer à la main (`sudo rm -r "$SAUVEGARDES/<nom>"`), jamais la plus récente |
| `hippo backup` : `"outcome":"locked"` | une autre sauvegarde a tenu le verrou 10 minutes (celle de la nuit, ou un autre `exec`) : attendre la ligne `backup` de celle-ci, puis relancer |
| Aucune ligne `backup` la nuit alors que `backup scheduled` est au démarrage | `LOG_LEVEL` au-dessus d'`info` dans `.env` masque la ligne d'une sauvegarde réussie : remettre `info` (la commande `hippo backup`, elle, imprime toujours sa ligne) |
| `hippo backup` : `"outcome":"name_taken"` | deux sauvegardes lancées dans la même seconde : relancer |
| Un dossier `.partial-…` dans `$SAUVEGARDES` alors qu'aucune sauvegarde ne tourne | laissé par un arrêt brutal pendant une sauvegarde : la suivante le supprime, n'y rien toucher |
| Pas de ligne `backup` le matin | NAS éteint ou Hippo arrêté à 3 h (pas de rattrapage) ; une ligne `scheduled run missed` avec `"schedule":"backup"` ; sinon chercher `backup scheduled` au dernier démarrage (section 9) |
| Après un `exec -u root`, Hippo n'arrive plus à ouvrir ou à écrire sa base | des fichiers `hippo.db-wal` ou `hippo.db-shm` appartiennent à root : `cd "$HIPPO_DIR" && sudo chown 65532:65532 data/hippo.db*`, puis redémarrer ; ne jamais lancer `hippo backup` avec `-u root` |
| `pull` : « manifest unknown » | le tag n'existe pas : le relire sur la page du paquet GHCR |
| `hippo-hippo-1` redémarre en boucle, log « open database … » ou « permission denied » | `data/` n'appartient pas à 65532 : `sudo ls -ldn data`, section 1 point 3 |
| `hippo-hippo-1` `unhealthy` | `sudo docker compose logs --tail 50 hippo` |
| `hippo-tailscale-1` : erreur sur `/dev/net/tun` | section 0 point 3 |
| Machine `hippo-1` dans la console | un ancien nœud `hippo` existait, ou `tailscale/state` a été perdu. Supprimer `hippo` et `hippo-1` dans la console, créer une clé neuve (section 3) et la mettre dans `.env`, puis : `cd "$HIPPO_DIR" && sudo docker compose down && sudo find tailscale/state -mindepth 1 -delete && sudo docker compose up -d`, puis section 4, points 5 et 6 (expiration de clé, clé effacée) |
| Après l'effacement de `TS_AUTHKEY`, le nœud ne se reconnecte plus | remettre une clé neuve (section 3) dans `.env`, `up -d`, puis la laisser en place (une clé consommée est inoffensive) et le noter dans le journal |
| `/healthz` injoignable depuis l'iPhone | Tailscale actif, LocalDevVPN coupé ? `hippo` connecté dans la console ? Premier appel : attendre le certificat et recharger |
| Le Mac ne résout pas `hippo.<tailnet>.ts.net` | « Use Tailscale DNS » coupé sur le Mac |
| `curl: (6) Could not resolve host` dans la session SSH | les tests `curl` se lancent depuis le Mac, pas depuis le NAS |
| Rien n'est analysé la nuit | `ANTHROPIC_API_KEY` vide ? budget du mois atteint (RG-81) ? NAS éteint à 2 h ? Lire la ligne `analysis pass` (son `outcome`) |
| L'interface montre `healthy` mais rien ne répond par Tailscale | le conteneur Tailscale a été redémarré seul, ou a planté et été relancé par Docker : redémarrer le projet (section 6) |
| Pas de ligne de la nuit dans le journal de mise à jour | NAS éteint à 4 h (pas de rattrapage) ; ligne cron ou script effacés par une mise à jour d'UGOS, ou `cron` arrêté : section 11, « Après chaque mise à jour d'UGOS » ; sinon le script s'est arrêté avant d'écrire : le lancer à la main (section 11, « Mettre en marche », point 2) et lire ce qu'il affiche |
| `failed` dans le journal de mise à jour | le tableau des `reason`, section 11 |
| Hippo arrêté ou `unhealthy` un matin | lire le journal de mise à jour (section 11) ; après `up_failed` ou `unhealthy` : section 8, « Après une mise à jour automatique » |
| `sudo: /usr/local/sbin/hippo-update: command not found`, ou « Permission denied » | script absent (effacé par une mise à jour d'UGOS ?) ou pas exécutable : section 11, « Installer le script » (`-rwx------`, propriétaire `0 0`) |
| Le script lancé à la main refuse de tourner et parle de propriétaire, de droits, de lien symbolique ou d'ACL | le dossier du projet n'est plus réservé à root : section 11, « Vérifications », point 4 |
| Le script lancé à la main affiche `hippo-update: cannot copy docker-compose.yml and .env into /var/lib/hippo-update/run.` | le disque système du NAS est sans doute plein : `df -h /var/lib`, libérer de la place, relancer. Rien n'a changé |
| Une nouvelle version a démarré sans ligne `updated` ni sauvegarde | `docker compose pull`, ou `up -d` sans tag `latest` local, avec `HIPPO_IMAGE_TAG=latest` : section 11, « Avec `latest` » ; revenir en arrière si besoin (section 8) |

## 11. Mise à jour automatique

Chaque nuit à 4 h, heure de Paris, une ligne cron de root lance le script `/usr/local/sbin/hippo-update`, avec le dossier du projet en argument. Il installe ce que `main` a publié : l'image `latest` et les fichiers de déploiement du même commit (`docker-compose.yml`, `tailscale/config/serve.json`, `.env.example` et le script lui-même) ([RG-99](metier.md#données-et-multi-utilisateur), [ADR-0040](adr/0040-mise-a-jour-automatique-de-nuit.md)). Un passage, dans l'ordre :

0. Il vérifie que le dossier du projet n'est modifiable que par root (« Vérifications », point 4) ; sinon il refuse de tourner, avec un message à l'écran et sans ligne au journal.
1. Frein à main serré (`HIPPO_IMAGE_TAG` n'est pas `latest`) : il note `paused` et s'arrête.
2. Il tire `latest` et récupère les fichiers de son commit, sans toucher à Hippo. Même image et mêmes fichiers que ceux en place : il note `up-to-date` et s'arrête.
3. Il refuse des fichiers qui ne conviennent pas au `.env` : une variable manquante, un compose qui ne se lit pas.
4. Il tire les autres images (Tailscale) ; Hippo tourne encore.
5. Il sauvegarde avec la version en place, comme la section 7, point 5 (`exec`, jamais `run`).
6. Il met les nouveaux fichiers en place (les anciens vont dans `/var/lib/hippo-update/previous/`), puis lance `docker compose up -d`.
7. Il attend 3 minutes au plus que Hippo soit `healthy` : `updated`, sinon `failed`.

Un échec avant le point 6 ne change rien : Hippo continue sur l'ancienne version, la nuit suivante réessaie. Un échec aux points 6 ou 7 laisse tout en l'état, sans retour arrière automatique : section 8, « Après une mise à jour automatique ». Le script n'écrit jamais dans `.env`.

Fichiers sur le NAS, tous à root et hors du dossier du projet : aucun partage d'UGOS ne les atteint. Ne rien y modifier à la main :

| Chemin | Rôle |
| --- | --- |
| `/etc/cron.d/hippo-update` | la ligne cron |
| `/usr/local/sbin/hippo-update` | le script, hors du dossier du projet ; remplacé par celui du nouveau commit après chaque mise à jour réussie, et seulement alors |
| `/var/lib/hippo-update/` | l'état du script (`0700`), créé par lui |
| `/var/lib/hippo-update/run/` | le temps d'un passage, les copies de `docker-compose.yml` et de `.env` (`0600`) que reçoit Compose ; vidé au début et effacé à la fin de chaque passage. Après une coupure de courant ou un `kill -9`, la copie de `.env`, avec ses secrets, reste sur le disque système jusqu'au passage suivant |
| `/var/lib/hippo-update/update.log`, `update.log.1` | le journal de mise à jour, et le précédent, tourné au-delà de 1 Mio |
| `/var/lib/hippo-update/previous/` | les fichiers de déploiement d'avant la dernière mise à jour |
| `/var/lib/hippo-update/staging/` | les fichiers récupérés par le dernier passage |
| `/var/lib/hippo-update/lock`, `/var/lib/hippo-update/installing` | le verrou (un passage à la fois), et la marque d'une installation en cours |

**Avec `latest`**, deux règles. Sans elles, Docker peut démarrer une version que le script n'a pas installée, donc sans sauvegarde :

- jamais `docker compose pull` à la main ;
- avant une intervention à la main qui finit par `docker compose up -d` (changer le `.env`, une mise à jour à la main, une restauration), serrer le frein à main.

### Vérifications (une fois)

1. `flock`, dont le script a besoin (NAS) :

   ```bash
   command -v flock
   ```

   Attendu : `/usr/bin/flock`. Rien : s'arrêter et le signaler.

2. Fuseau horaire de l'hôte : cron lance la ligne à l'heure locale du NAS (NAS) :

   ```bash
   timedatectl
   ```

   Attendu : `Time zone: Europe/Paris`. Sinon, régler le fuseau dans UGOS › Panneau de configuration › Date et heure (libellés indicatifs), redémarrer cron (`sudo systemctl restart cron`) et revérifier.

3. cron en marche (NAS) :

   ```bash
   systemctl is-active cron
   ```

   Attendu : `active`.

4. **Droits du dossier du projet.** Chaque nuit, root lit ces fichiers et démarre le compose qu'ils décrivent : un compte qui pourrait les modifier (le compte administrateur par SMB depuis le Mac, une appli d'UGOS) deviendrait root à 4 h. Le script refuse donc de tourner si l'un d'eux, son état `/var/lib/hippo-update` ou le script lui-même est un lien symbolique, n'appartient pas à root, est modifiable par le groupe ou les autres, ou porte une ACL. Il entre dans le dossier du projet avant de le contrôler et ne lit que ce dossier-là, même renommé ; Compose ne reçoit que des copies de `docker-compose.yml` et de `.env`, faites dans `/var/lib/hippo-update/run/`. Reste une course sur les montages (`./data`, `./tailscale/…`) : le partage parent, `docker`, reste modifiable par le compte administrateur, qui pourrait y renommer le dossier du projet au moment où les conteneurs sont créés. Ce point 4 ne la ferme pas ; elle est décrite dans l'[ADR-0040](adr/0040-mise-a-jour-automatique-de-nuit.md#conséquences). NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo ls -ldn . tailscale tailscale/config docker-compose.yml .env .env.example tailscale/config/serve.json
   ```

   Attendu, sur chaque ligne :
   - premier caractère `d` ou `-`, jamais `l` (un lien symbolique, avec `->`) ;
   - propriétaire (3e colonne) `0` ;
   - aucun `w` en 6e ni en 9e caractère (`drwxr-xr-x`, `-rw-r--r--` et `-rw-------` conviennent) ;
   - pas de `+` à la fin des droits.

   Un propriétaire qui n'est pas `0`, ou un `w` en trop : corriger avec les deux commandes suivantes, puis relancer la vérification. Elles ne touchent que ces chemins, sans `-R` : **jamais** `data/` (à l'utilisateur de l'image, 65532), `tailscale/state/`, `copies/` ni le dossier des sauvegardes. NAS :

   ```bash
   cd "$HIPPO_DIR" && sudo chown 0:0 . tailscale tailscale/config docker-compose.yml .env .env.example tailscale/config/serve.json
   ```

   ```bash
   cd "$HIPPO_DIR" && sudo chmod go-w . tailscale tailscale/config docker-compose.yml .env .env.example tailscale/config/serve.json
   ```

   Ensuite, le dossier du projet n'est plus modifiable depuis le Mac : c'est voulu.

   Un `l` (lien symbolique) ou un `+` (ACL d'UGOS) : s'arrêter et le signaler.

   Après le premier passage (« Mettre en marche », point 2), l'état du script (NAS) :

   ```bash
   sudo ls -ldn /var/lib/hippo-update
   ```

   Attendu : `drwx------`, propriétaire `0 0`, sans `+`. Sinon : `sudo chown 0:0 /var/lib/hippo-update && sudo chmod 700 /var/lib/hippo-update` ; un `l` : s'arrêter et le signaler.

### Installer le script (une fois)

Par `curl` depuis GitHub, comme les fichiers de la section 1 ; `scp` échoue sur UGOS. Le script va dans `/usr/local/sbin`, sur le système du NAS, **hors du dossier du projet** : aucun partage d'UGOS ne l'atteint. `SHA` : section 1, point 1 ; celui d'un commit de `main` (pendant la QA du change `nightly-update`, le dernier commit de sa PR). Ensuite, le script se remplace seul après chaque mise à jour réussie. NAS :

```bash
sudo curl -fsSL -o /usr/local/sbin/hippo-update "https://raw.githubusercontent.com/ghislain95/hippo/$SHA/infra/hippo-update.sh" && sudo chown 0:0 /usr/local/sbin/hippo-update && sudo chmod 700 /usr/local/sbin/hippo-update && sudo ls -ln /usr/local/sbin/hippo-update
```

Attendu : `-rwx------`, propriétaire `0 0`, sans `+` à la fin. `curl: (22)` avec `404` : ce commit n'a pas de script (il est antérieur au change `nightly-update`) ; prendre un commit plus récent. `curl: (23)` : `/usr/local/sbin` n'existe pas ; s'arrêter et le signaler.

### Ligne cron (une fois)

La commande écrit `/etc/cron.d/hippo-update`, avec en argument du script le chemin réel du dossier du projet, tiré de `HIPPO_DIR` (section 1, point 1). Ce chemin ne doit contenir aucune espace. NAS :

```bash
[ -n "$HIPPO_DIR" ] && printf 'SHELL=/bin/bash\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n0 4 * * * root /usr/local/sbin/hippo-update %s >/dev/null 2>&1\n' "$HIPPO_DIR" | sudo tee /etc/cron.d/hippo-update >/dev/null
```

Puis vérifier (NAS) :

```bash
sudo chmod 644 /etc/cron.d/hippo-update && sudo ls -ln /etc/cron.d/hippo-update && cat /etc/cron.d/hippo-update
```

Attendu : `-rw-r--r--`, propriétaire `0 0`, et trois lignes ; la dernière est `0 4 * * * root /usr/local/sbin/hippo-update <chemin du dossier du projet> >/dev/null 2>&1`. cron lit le fichier seul, sans redémarrage. La sortie du script ne va nulle part : tout est dans le journal de mise à jour.

Tant que `HIPPO_IMAGE_TAG` épingle un `sha-…`, chaque nuit note `paused`. Pour le vérifier tout de suite, lancer le script à la main (« Mettre en marche », point 2) : attendu `"outcome":"paused"`.

### Mettre en marche

Hippo tourne (`healthy`, section 4, point 2) ; hors de 2 h à 4 h 30.

1. Passer `HIPPO_IMAGE_TAG` à `latest` (NAS) :

   ```bash
   sudo sed -i 's/^HIPPO_IMAGE_TAG=.*/HIPPO_IMAGE_TAG=latest/' "$HIPPO_DIR/.env" && sudo grep -n '^HIPPO_IMAGE_TAG=' "$HIPPO_DIR/.env" && sudo ls -ln "$HIPPO_DIR/.env"
   ```

   Attendu : une seule ligne, `HIPPO_IMAGE_TAG=latest`, et `.env` toujours en `-rw-------`, propriétaire `0 0`. Ne lancer ni `docker compose pull` ni `docker compose up -d` : le passage s'en charge, après la sauvegarde.

2. Un passage à la main, sans attendre 4 h (NAS) :

   ```bash
   sudo /usr/local/sbin/hippo-update "$HIPPO_DIR"; echo "code de sortie : $?"
   ```

   Attendu, après quelques minutes au plus : la ligne JSON du passage (la même que dans le journal de mise à jour), avec `"outcome":"updated"` (nouvelle version ou nouveaux fichiers, installés après une sauvegarde) ou `"outcome":"up-to-date"`, puis `code de sortie : 0`. Autre chose : « Lire le journal de mise à jour » ci-dessous. Un message sans ligne JSON : le script a refusé de tourner avant d'écrire au journal, souvent pour les droits (« Vérifications », point 4).

3. L'état du script : « Vérifications », point 4, dernière commande. Après `updated` : section 7, point 7 (`healthy`, `/healthz` avec la nouvelle `version`), et l'appli Docker d'UGOS montre toujours le projet `hippo` et ses deux conteneurs. Le script lance Compose sur des copies qu'il efface ensuite : les conteneurs pointent vers un compose qui n'existe plus. Un `docker compose` lancé à la main depuis `$HIPPO_DIR` retrouve le projet par son nom ; si l'appli d'UGOS ne le montre plus, le noter et le signaler (à vérifier pendant la QA du change `nightly-update`). Noter dans le journal d'exploitation la date, `new_revision` et `backup`.
4. Le lendemain matin, une ligne de 4 h dans le journal de mise à jour.

### Lire le journal de mise à jour

NAS :

```bash
sudo tail -n 5 /var/lib/hippo-update/update.log
```

Une ligne JSON par issue : `time` (heure locale du NAS), `msg` (`update`), `outcome`, et seulement quand ils s'appliquent : `reason`, `old_revision` et `new_revision` (des commits de 40 caractères, ou `unknown`), `backup` (le nom de la sauvegarde, section 9), `missing` (des noms de variables), `duration_s`. Jamais un secret, une valeur du `.env` ni un contenu d'élément ; une valeur qui n'a pas sa forme attendue est remplacée par `invalid`. Le fichier précédent : `/var/lib/hippo-update/update.log.1`.

| `outcome` | Sens | Que faire |
| --- | --- | --- |
| `paused` | frein à main serré : rien n'a été fait | rien ; pour reprendre, « Mettre en marche » |
| `up-to-date` | rien de nouveau : ni sauvegarde ni redémarrage | rien |
| `updated` | nouvelle version ou nouveaux fichiers installés, Hippo `healthy` | rien ; au besoin, noter `new_revision` et `backup` dans le journal d'exploitation |
| `interrupted` | un passage a été coupé pendant l'installation (redémarrage du NAS, arrêt) ; `new_revision` dit laquelle | lire la ligne suivante : le passage a continué, et sa sauvegarde et son `up -d` réparent l'installation ; puis `cd "$HIPPO_DIR" && sudo docker compose ps` |
| `locked` | un autre passage tournait (par exemple un passage lancé à la main vers 4 h) ; rien n'a été fait | rien si c'est une fois ; plusieurs nuits de suite, `sudo pgrep -af hippo-update` montre un passage bloqué : le signaler |
| *(aucune ligne de la nuit)* | le script n'a pas tourné (ligne cron ou script effacés, NAS éteint à 4 h), ou s'est arrêté avant d'écrire : droits du dossier du projet refusés (« Vérifications », point 4), copie de `docker-compose.yml` et `.env` impossible dans `/var/lib/hippo-update/run` (disque système plein ?), `flock` absent, `/var/lib/hippo-update` qui est un lien symbolique | `sudo ls -ln /etc/cron.d/hippo-update /usr/local/sbin/hippo-update`, puis lancer le script à la main (« Mettre en marche », point 2) : un arrêt avant le journal affiche sa raison à l'écran |
| `failed` | échec, expliqué par `reason` | tableau suivant |

| `reason` | Ce qui s'est passé | Hippo | Que faire |
| --- | --- | --- | --- |
| `no_image_tag` | `.env` absent, ou sans ligne `HIPPO_IMAGE_TAG` | inchangé | section 2, point 4 |
| `hippo_not_running` | aucun conteneur `hippo` en marche | déjà arrêté | `cd "$HIPPO_DIR" && sudo docker compose ps -a`, puis ses logs et la section 10 |
| `pull_failed` | GHCR, ou Docker Hub pour Tailscale, n'a pas répondu, ou une image n'existe pas | inchangé | rien : la nuit suivante réessaie. Plusieurs nuits de suite : vérifier l'accès à Internet du NAS et la page du paquet GHCR |
| `bad_revision` | l'image publiée n'a pas d'étiquette de révision valable | inchangé | le signaler : la publication de l'image est à corriger |
| `fetch_failed` | un fichier n'a pas pu être récupéré sur GitHub, à ce commit | inchangé | rien : la nuit suivante réessaie. Plusieurs nuits de suite : le signaler |
| `missing_variables` | le nouveau `.env.example` a une variable que `.env` n'a pas ; `missing` la nomme | inchangé | l'ajouter au `.env` (ci-dessous), puis relancer à la main |
| `compose_invalid` | le nouveau compose ne se lit pas avec le `.env` actuel | inchangé | lire l'erreur (ci-dessous), corriger le `.env`, puis relancer à la main |
| `backup_failed` | la sauvegarde d'avant la mise à jour a échoué | inchangé | une sauvegarde à la main (section 7, point 5), et lire son `outcome` (sections 9 et 10) |
| `install_failed` | le passage a commencé à installer (point de non-retour), mais n'a pas pu écrire un fichier du dossier du projet (disque plein, droits) ; la sauvegarde est faite | inchangé ou à moitié installé : certains fichiers peuvent être nouveaux | `df -h "$HIPPO_DIR"`, puis relancer à la main : le passage suivant reprend tout, avec une nouvelle sauvegarde |
| `up_failed` | les nouveaux fichiers sont en place, `docker compose up -d` a échoué | sans doute arrêté | relancer `cd "$HIPPO_DIR" && sudo docker compose up -d` et lire l'erreur ; sinon section 8, « Après une mise à jour automatique » |
| `unhealthy` | la nouvelle version a démarré, mais n'est pas `healthy` après 3 minutes | nouvelle version, lente ou en panne | `cd "$HIPPO_DIR" && sudo docker compose ps` : `healthy` un peu plus tard (une longue migration), rien à faire ; sinon ses logs (section 10), puis section 8, « Après une mise à jour automatique » |

Un `up_failed` ou un `unhealthy` n'est noté qu'**une nuit** : la nuit suivante, si la nouvelle version tourne avec les nouveaux fichiers, le passage note `up-to-date`, même si elle n'est toujours pas `healthy`. Lire le journal le matin d'après une fusion, et `cd "$HIPPO_DIR" && sudo docker compose ps` en cas de doute.

**Variable manquante** : le nouveau `.env.example` est dans `/var/lib/hippo-update/staging/`, avec le commentaire de la variable. En remplaçant `<NOM>` par un nom de `missing` (NAS) :

```bash
sudo grep -n -B 8 '^<NOM>=' /var/lib/hippo-update/staging/.env.example
```

L'ajouter au `.env` avec la valeur qui convient (`sudo vi "$HIPPO_DIR/.env"`, section 2, point 4), puis relancer à la main (« Mettre en marche », point 2).

**Compose qui ne se lit pas** : la même vérification que le script, qui affiche l'erreur. Elle peut citer une valeur du `.env` : ne la copier nulle part. NAS :

```bash
cd "$HIPPO_DIR" && sudo docker compose --project-directory "$HIPPO_DIR" --env-file "$HIPPO_DIR/.env" -f /var/lib/hippo-update/staging/docker-compose.yml config --quiet
```

### Frein à main

**Serrer** : mettre dans `HIPPO_IMAGE_TAG` le tag de la version en place, `sha-` suivi des 7 premiers caractères de la `version` de `/healthz` (section 4, point 7), ou du `new_revision` de la dernière ligne `updated`. En remplaçant `<7 caractères>` (NAS) :

```bash
sudo sed -i 's/^HIPPO_IMAGE_TAG=.*/HIPPO_IMAGE_TAG=sha-<7 caractères>/' "$HIPPO_DIR/.env" && sudo grep -n '^HIPPO_IMAGE_TAG=' "$HIPPO_DIR/.env"
```

Attendu : une seule ligne, avec le bon tag ; vérifier qu'il existe (section 2, point 3). Rien d'autre à faire : Hippo continue sur la même version, et chaque passage note `paused`. Un `docker compose up -d` plus tard recrée peut-être le conteneur sur la même image, sans effet.

**Desserrer** : « Mettre en marche ». Le premier passage installe ce qui a été publié entre-temps, après une sauvegarde.

**Arrêter pour de bon** : `sudo rm /etc/cron.d/hippo-update` ; `/usr/local/sbin/hippo-update` et `/var/lib/hippo-update/` peuvent rester.

### Après chaque mise à jour d'UGOS

Une mise à jour d'UGOS peut effacer la ligne cron, le script ou son état sans prévenir : ils sont sur le système du NAS, pas sur le volume des données. Le lendemain matin d'une mise à jour d'UGOS (NAS) :

```bash
sudo ls -ln /etc/cron.d/hippo-update /usr/local/sbin/hippo-update
```

```bash
systemctl is-active cron
```

```bash
sudo tail -n 1 /var/lib/hippo-update/update.log
```

Attendu : les deux fichiers (`-rw-r--r--` et `-rwx------`, propriétaire `0 0`), `active`, et une ligne dont `time` est 4 h la nuit précédente. Ligne cron absente : « Ligne cron » ci-dessus ; script absent : « Installer le script » ; journal absent : lancer le script à la main (« Mettre en marche », point 2), puis vérifier son état (« Vérifications », point 4). Refaire aussi « Vérifications », points 2 (fuseau horaire) et 4 (droits du dossier du projet) : une mise à jour d'UGOS peut changer les droits de ses partages.

### Effacer les vieilles images

Chaque mise à jour laisse l'image d'avant sur le disque, sans tag (environ 25 Mo chacune). De temps en temps (NAS) :

```bash
sudo docker image prune
```

Répondre `y`. Seules les images sans tag et sans conteneur s'effacent, jamais celle en marche. Un retour arrière retélécharge l'ancienne image par son tag `sha-…`.
