# Runbook NAS — installer, vérifier, mettre à jour Hippo

Procédure pour faire tourner Hippo sur le NAS (Ugreen DXP4800 Plus, UGOS Pro), le joindre en HTTPS par Tailscale, puis le mettre à jour et revenir en arrière. Elle sert à Ghislain, seul, sans l'agent ([ADR-0015](adr/0015-reseau-tailscale.md), [ADR-0019](adr/0019-deploiement-docker-nas.md), [ADR-0038](adr/0038-limite-memoire-384-mo.md)).

> **État** : écrite le 2026-11-07 par le change `deploy-nas` (E6-01, E6-02, E6-04), à suivre une première fois pendant sa QA. Les libellés des écrans d'UGOS Pro sont donnés à titre indicatif : la QA les corrige.
>
> **Pas encore de sauvegarde** : la sauvegarde nocturne arrive avec E6-03 (change `nightly-backup`). D'ici là, rien de précieux dans Hippo (section 9).

Conventions de ce document :

- Les commandes marquées « NAS » se tapent dans une session SSH sur le NAS ; celles marquées « Mac » dans un terminal du Mac.
- `<volume>`, `<tailnet>`, `<sha>`, `<tag>` sont à remplacer. Rien de personnel (nom du tailnet, adresses, clés) ne s'écrit dans le dépôt, qui est public ([ADR-0027](adr/0027-depot-public.md)).
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

   Repérer le dossier partagé qui accueillera Hippo (par exemple `/<volume>/docker`, créé par l'appli Docker). Il doit être **réservé à l'administrateur** (aucun autre utilisateur, pas d'accès invité) et **exclu de toute tâche de synchronisation ou de sauvegarde vers le cloud** d'UGOS : la base et les images de Hippo ne doivent pas quitter le NAS en clair ([ADR-0020](adr/0020-sauvegardes.md)). Le chiffrement de la copie externe arrive avec E6-03.

5. **Alimentation** : UGOS › Panneau de configuration › Matériel et alimentation. Aucune extinction ni mise en veille programmée autour de 2 h : le batch de nuit manqué n'est pas rattrapé, il part la nuit suivante (RG-30).

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

4. Noter dans le journal : date, `SHA`.

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

3. Choisir le tag de l'image : page du paquet [ghcr.io/ghislain95/hippo](https://github.com/ghislain95/hippo/pkgs/container/hippo), le tag `sha-<7 caractères>` le plus récent. Il n'existe que pour les fusions qui touchent le backend ; c'est normal qu'il diffère du `SHA` de la section 1. Vérifier qu'il existe pour le NAS (NAS) :

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
   | `MONTHLY_BUDGET_USD` | `5` (au-delà, plus d'analyse jusqu'au 1er du mois suivant, RG-81) |
   | `TZ` | `Europe/Paris` |
   | `TS_AUTHKEY` | la clé de la section 3, puis **effacée** après l'enrôlement (section 4) |
   | `HIPPO_IMAGE_TAG` | `sha-<7 caractères>` du point 3 ; obligatoire, le compose refuse de démarrer sans |

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
   - `"msg":"no ANTHROPIC_API_KEY: the nightly analysis sends nothing"` (attendu en phase 1).

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

8. **Depuis le Mac** (« Use Tailscale DNS » actif dans Tailscale, voir le [runbook iOS](runbook-ios.md#2-tailscale-en-https-une-fois)) : une capture authentifiée, une note fictive **marquée privée** (jamais envoyée à l'IA, RG-12). Le token est lu sans écho, pour ne pas finir dans l'historique du shell. Mac :

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
- **Redémarrer** : redémarrer **le projet** (les deux conteneurs), ou `hippo-hippo-1` seul. **Jamais `hippo-tailscale-1` seul** : il porte le réseau de Hippo, qui perdrait sa connexion sans que sa sonde ne le voie.
- **Ne pas modifier** le compose ni les variables depuis l'interface : la référence est le dossier `$HIPPO_DIR` et ce runbook.

## 7. Mettre à jour

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

5. **Copier les données, Hippo arrêté** (la base est en WAL : ses fichiers `-wal` et `-shm` partent avec elle), puis démarrer la nouvelle version. La copie porte la date et l'heure ; si elle existe déjà, `mkdir` échoue avant l'arrêt et rien ne change. NAS :

   ```bash
   cd "$HIPPO_DIR" && C="copies/data-$(date +%F-%H%M)" && sudo mkdir "$C" && sudo docker compose stop hippo && sudo cp -a data/. "$C"/ && echo "copie : $C" && sudo docker compose up -d
   ```

   Noter le nom de la copie dans le journal.

6. Vérifier : `cd "$HIPPO_DIR" && sudo docker compose ps` (`healthy`), `/healthz` depuis l'iPhone avec la nouvelle `version`. Les migrations s'appliquent seules au démarrage.
7. Noter dans le journal ; supprimer les copies de plus de quelques jours dans `copies/`.

## 8. Revenir en arrière

- **Sans migration entre les deux versions** : remettre l'ancien tag dans `HIPPO_IMAGE_TAG` (et les anciens fichiers, section 1 avec l'ancien `SHA`, s'ils ont changé), puis `sudo docker compose up -d`.
- **Avec une migration** : l'ancienne image démarrerait sans erreur sur le nouveau schéma, qu'elle ne connaît pas (goose ignore les versions inconnues). Il faut restaurer la copie faite avant la mise à jour. NAS :

  ```bash
  cd "$HIPPO_DIR" && sudo docker compose stop hippo && sudo mv -n data "copies/data-abandonnee-$(date +%F-%H%M)" && sudo cp -a "copies/<nom de la copie, noté dans le journal>" data && sudo ls -ldn data
  ```

  Attendu : `data` appartient à `65532 65532` (`cp -a` garde le propriétaire). Puis remettre l'ancien tag et les anciens fichiers, et `sudo docker compose up -d`. Les captures faites depuis la copie sont perdues (elles restent dans `copies/data-abandonnee-…`).

## 9. Sauvegardes : pas encore

Jusqu'au change `nightly-backup` (E6-03, [ADR-0020](adr/0020-sauvegardes.md)), **aucune sauvegarde automatique** :

- le RAID protège d'une panne de disque, pas d'une erreur ni d'un bug ;
- seules existent les copies manuelles de `copies/`, faites avant chaque mise à jour ;
- ne rien mettre de précieux dans Hippo d'ici là ;
- ne pas ajouter `$HIPPO_DIR` à une synchronisation ou à une sauvegarde cloud d'UGOS : la copie externe sera chiffrée sur le NAS avant l'envoi.

Perdre `tailscale/state` crée une nouvelle machine au prochain démarrage (nouvelle clé d'authentification, et peut-être un nouveau nom) : `nightly-backup` décidera s'il faut le sauvegarder.

## 10. En cas de problème

| Symptôme | Piste |
| --- | --- |
| `docker compose` : « set HIPPO_IMAGE_TAG in infra/.env » ou « set API_TOKEN… » | variable vide ou absente dans `.env` (section 2) |
| `pull` : « manifest unknown » | le tag n'existe pas : le relire sur la page du paquet GHCR |
| `hippo-hippo-1` redémarre en boucle, log « open database … » ou « permission denied » | `data/` n'appartient pas à 65532 : `sudo ls -ldn data`, section 1 point 3 |
| `hippo-hippo-1` `unhealthy` | `sudo docker compose logs --tail 50 hippo` |
| `hippo-tailscale-1` : erreur sur `/dev/net/tun` | section 0 point 3 |
| Machine `hippo-1` dans la console | un ancien nœud `hippo` existait, ou `tailscale/state` a été perdu. Supprimer `hippo` et `hippo-1` dans la console, créer une clé neuve (section 3) et la mettre dans `.env`, puis : `cd "$HIPPO_DIR" && sudo docker compose down && sudo find tailscale/state -mindepth 1 -delete && sudo docker compose up -d`, puis section 4, points 5 et 6 (expiration de clé, clé effacée) |
| Après l'effacement de `TS_AUTHKEY`, le nœud ne se reconnecte plus | remettre une clé neuve (section 3) dans `.env`, `up -d`, puis la laisser en place (une clé consommée est inoffensive) et le noter dans le journal |
| `/healthz` injoignable depuis l'iPhone | Tailscale actif, LocalDevVPN coupé ? `hippo` connecté dans la console ? Premier appel : attendre le certificat et recharger |
| Le Mac ne résout pas `hippo.<tailnet>.ts.net` | « Use Tailscale DNS » coupé sur le Mac |
| Rien n'est analysé la nuit | `ANTHROPIC_API_KEY` vide ? budget du mois atteint (RG-81) ? NAS éteint à 2 h ? Lire la ligne `analysis pass` (son `outcome`) |
| L'interface montre `healthy` mais rien ne répond par Tailscale | le conteneur Tailscale a été redémarré seul, ou a planté et été relancé par Docker : redémarrer le projet (section 6) |
