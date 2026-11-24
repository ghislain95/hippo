# Runbook iOS — installer et rafraîchir l'appli

Procédure pour installer l'appli Hippo sur l'iPhone avec un compte Apple gratuit (ADR-0013), puis la garder active. Elle sert à Ghislain, seul, sans l'agent.

> **État** : sections 1, 2, 4 et 6 vérifiées avec le prototype E1 sur un iPhone 15 sous iOS 27, les 2026-10-26 et 2026-10-27 ([résultats](../openspec/changes/archive/2026-10-27-ios-prototype/results.md)). Section 5 (vraie appli et son extension, E5-02) : `make ipa` vérifié sur le Mac le 13 novembre 2026 avec une URL et un token factices (le `.ipa` contient l'extension) ; l'installation sur l'iPhone est vérifiée à la QA d'E5-02.

## Ce qu'il faut

| Élément | Où | Coût |
| --- | --- | --- |
| Xcode (licence acceptée, `xcode-select` réglé) | Mac | gratuit |
| CocoaPods 1.16.2 ou plus (`brew install cocoapods`), dépendances du dépôt (`make setup`) | Mac | gratuit |
| Identifiant Apple (compte gratuit, « Personal Team ») | Mac et iPhone | gratuit |
| Compte Tailscale, Tailscale sur le Mac (ou le NAS) et l'iPhone | Mac, NAS, iPhone | gratuit |
| iloader | Mac | gratuit |
| LocalDevVPN, SideStore | iPhone | gratuit |
| Câble USB entre l'iPhone et le Mac | une seule fois | — |

## 1. Préparer Xcode (une fois)

1. Dans un terminal, régler Xcode et accepter sa licence :

   ```bash
   sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
   ```

   ```bash
   sudo xcodebuild -license accept
   ```

2. Xcode › Réglages › Comptes : ajouter l'identifiant Apple. Une équipe « (Personal Team) » apparaît.
3. L'identifiant d'équipe (10 caractères) n'est pas affiché dans cette fenêtre. Il se lit dans les réglages de Xcode :

   ```bash
   defaults read com.apple.dt.Xcode IDEProvisioningTeamByIdentifier | grep -E 'teamID|teamName'
   ```

   Il ne s'écrit jamais dans le dépôt. Il ne sert qu'à un build signé (variable `HIPPO_APPLE_TEAM_ID`) ; le `.ipa` pour SideStore (section 5) n'en a pas besoin.

## 2. Tailscale en HTTPS (une fois)

1. Utiliser le compte [tailscale.com](https://tailscale.com) existant (le NAS et les iPhones y sont déjà), ou en créer un.
2. Ouvrir Tailscale sur le Mac et sur l'iPhone, avec le même compte. Les deux apparaissent dans la console d'administration.
3. Avant d'activer HTTPS, renommer dans la console toute machine qui servira en HTTPS et dont le nom est personnel.
   - Son nom complet `<machine>.<tailnet>.ts.net` sera inscrit dans le registre public des certificats (Certificate Transparency).
   - Les machines qui ne demandent pas de certificat n'y figurent pas.
   - Vérifier aussi, page DNS, que le nom du tailnet est aléatoire.
4. Console d'administration › DNS : vérifier que **MagicDNS** est actif, puis activer **HTTPS Certificates**.
   - Cela ne change rien aux connexions existantes : seules les machines qui demandent un certificat (par `tailscale serve`) en reçoivent un.
   - « Disable HTTPS » reste possible ; il coupe seulement ce qui passe par HTTPS.
5. Publier le backend en HTTPS sur le tailnet.
   - **Sur le Mac** (développement) : lancer le backend avec `make dev` (il écoute sur `127.0.0.1:8080`), puis, avec la commande `tailscale` de l'appli :

     ```bash
     /Applications/Tailscale.app/Contents/MacOS/Tailscale serve --bg 8080
     ```

   - **Sur le NAS** : la publication passe par le conteneur Tailscale du compose et `infra/tailscale/config/serve.json` (ADR-0015, E6).

   L'adresse est de la forme `https://<machine>.<tailnet>.ts.net`. Le certificat est un vrai certificat public : l'appli n'a besoin d'aucune exception ATS.

6. Vérifier depuis l'iPhone, Tailscale actif : ouvrir `https://<machine>.<tailnet>.ts.net/healthz` dans Safari. La réponse attendue est `200` (route publique, sans token).
7. Pour arrêter la publication sur le Mac : `Tailscale serve --https=443 off` ; `Tailscale serve status` la montre.

Le serveur n'est joignable que depuis le tailnet. Rien n'est exposé sur Internet (ADR-0015).

À savoir : un Mac ne joint pas sa propre adresse `.ts.net` (vu en E1), et ne résout ces noms que si « Use Tailscale DNS » est actif. Vérifier depuis l'iPhone, pas depuis le Mac.

## 3. Installer depuis Xcode (option sans SideStore)

Utile pour un premier essai ou un dépannage. L'appli expire au bout de 7 jours et se réinstalle depuis le Mac.

Vérifiée avec le prototype E1 seulement. Pour la vraie appli, construire depuis l'interface de Xcode ne marche pas : l'appli embarquerait un token vide ([conventions](dev/conventions-app.md#configuration-au-build)). Passer par SideStore (section 5).

1. Brancher l'iPhone au Mac, le déverrouiller, puis « Se fier à cet ordinateur ».
2. Réglages › Confidentialité et sécurité › **Mode développeur** : activer. L'iPhone redémarre ; le déverrouiller et confirmer **Activer**. L'option apparaît une fois l'iPhone appairé au Mac.
3. Construire et installer sur l'iPhone, en configuration Release : le JavaScript est embarqué, l'appli marche loin du Mac. Xcode crée un profil « iOS Team Provisioning Profile » pour l'appli et un pour l'extension.
4. Réglages › Général › VPN et gestion de l'appareil › l'identifiant Apple › **Se fier**. Sans cette étape, l'appli refuse de s'ouvrir.

## 4. Installer SideStore (une fois)

Source : [documentation SideStore](https://docs.sidestore.io/docs/installation/prerequisites), consultée le 2026-10-26.

1. Sur l'iPhone : installer **LocalDevVPN** depuis l'App Store, l'ouvrir, **Connect**, autoriser la configuration VPN.
2. Sur le Mac : installer **iloader** ([DMG](https://github.com/nab138/iloader/releases/latest/download/iloader-darwin-universal.dmg)).
3. iPhone branché et déverrouillé : ouvrir iloader, se connecter avec l'identifiant Apple, choisir l'iPhone, puis **Install SideStore (Stable)**.
4. Sur l'iPhone : faire confiance à l'identifiant Apple (Réglages › Général › VPN et gestion de l'appareil) et activer le mode développeur, si ce n'est pas déjà fait.
5. LocalDevVPN connecté, ouvrir SideStore et se connecter avec le **même** identifiant Apple. En cas d'erreur `-80045`, voir « En cas de problème ».
6. SideStore › My Apps : toucher le compteur **7 DAYS** à côté de SideStore. Le compteur qui se remplit indique que le rafraîchissement a réussi.

Si SideStore signale un fichier d'appairage invalide (après une mise à jour d'iOS, par exemple) : iloader › gestion du fichier d'appairage › exporter, puis l'importer dans SideStore.

## 5. Construire et installer l'appli avec SideStore

L'appli et son extension de partage sont construites ensemble, dans un seul `.ipa` non signé. SideStore les signe avec le compte gratuit.

### Avant la première installation (une fois)

1. Supprimer l'appli du prototype E1 (`com.ghislain95.hippo.spike`) : SideStore › My Apps › la supprimer, puis la supprimer de l'écran d'accueil de l'iPhone. Elle occupe une des 3 places d'applis actives.
2. La première installation crée **2 App ID** : `com.ghislain95.hippo` et `com.ghislain95.hippo.share` (l'extension). Il en faut donc 2 de libres sur les 10 de la période de 7 jours ([limites](#limites-du-compte-gratuit)).

### Construire le `.ipa` (Mac)

Dans un terminal du Mac, depuis la racine du dépôt.

1. Définir l'URL du serveur, en `https` (celle du NAS, `https://hippo.<tailnet>.ts.net`, sans slash final) :

   ```bash
   export HIPPO_API_URL='https://hippo.<tailnet>.ts.net'
   ```

2. Définir le token, lu sans écho pour ne pas finir dans l'historique du shell : coller le token du NAS (gestionnaire de mots de passe, « Hippo NAS — API_TOKEN », voir le [runbook NAS](runbook-nas.md#2-fichier-env-une-fois)), rien ne s'affiche, puis `Entrée` :

   ```bash
   read -rs HIPPO_API_TOKEN && export HIPPO_API_TOKEN
   ```

3. Construire :

   ```bash
   make ipa
   ```

   Il produit `app/build/Hippo.ipa` et `app/build/Hippo.ipa.zip`. Il refuse une URL qui n'est pas en `https`, un token absent ou trop court ; le message nomme la variable, jamais sa valeur.

4. Fermer le terminal : le token ne reste pas dans la session.

Ce qu'affiche le prebuild : un avertissement sur `ios.appleTeamId` quand `HIPPO_APPLE_TEAM_ID` n'est pas défini, sans effet ici. **Aucun avertissement sur les App Groups n'est attendu** (`@bacons/apple-targets` 5.0.0). L'appli et l'extension n'ont **aucun App Group, et il ne faut pas en ajouter** : le compte gratuit ne les gère pas de façon fiable ([ADR-0014](adr/0014-extension-de-partage-autonome.md)).

### Installer

1. Envoyer `Hippo.ipa.zip` sur l'iPhone par AirDrop. Il arrive dans Fichiers › Téléchargements ; le toucher pour en extraire le `.ipa`.
   - Un `.ipa` envoyé seul par AirDrop est ouvert par iOS comme une appli à installer. Il laisse une icône cassée (« intégrité non vérifiée ») : la supprimer.
2. Activer LocalDevVPN (Tailscale se coupe : voir section 6), en Wi-Fi.
3. Supprimer la version installée par Xcode, si elle existe.
4. SideStore › My Apps › **+** › choisir le `.ipa`. À la question sur les extensions, choisir **Keep app extension (register app ID for each extension)** : l'extension de partage prend son propre App ID.
5. Réactiver Tailscale.
6. Vérifier : l'appli s'ouvre sur l'accueil avec « Serveur joignable » ; dans Safari, Partager › **Hippo** ouvre le formulaire de l'extension.

SideStore ajoute l'identifiant d'équipe à la fin des identifiants de bundle (`….spike.<équipe>` en E1). Rien ne doit en dépendre.

### Changer d'URL ou de token

L'URL et le token sont figés dans l'appli et l'extension au build ([ADR-0014](adr/0014-extension-de-partage-autonome.md)). Les changer demande un **nouveau build** : reprendre « Construire le `.ipa` » avec les nouvelles valeurs, puis « Installer ». Les identifiants de bundle ne changent pas : aucun nouvel App ID n'est consommé.

## 6. Rafraîchir chaque semaine

iOS ne garde **qu'un seul VPN actif**. LocalDevVPN (pour SideStore) et Tailscale (pour joindre le NAS) s'excluent (ADR-0013).

1. En Wi-Fi, ouvrir LocalDevVPN › **Connect**. Tailscale se coupe.
2. SideStore › My Apps › **Refresh All** (ou le compteur **7 DAYS** de chaque appli).
3. Ouvrir Tailscale et le réactiver. LocalDevVPN se coupe.
4. Vérifier : l'appli joint le serveur.

Vérifié en E1 le 2026-10-27. Le rafraîchissement automatique en arrière-plan n'avait pas encore été observé à cette date : à revoir avant l'expiration du 2026-11-03.

Une appli expirée ne fait rien perdre : les données sont sur le NAS.

## Limites du compte gratuit

| Limite | Effet | Parade |
| --- | --- | --- |
| 3 applis actives, SideStore compris | il reste 2 places | ne garder que Hippo |
| 10 identifiants d'appli (App ID) par période de 7 jours | chaque installation sous un **nouvel** identifiant en consomme ; l'extension compte aussi (2 App ID par installation, vu en E1) | ne jamais changer les identifiants de bundle de l'appli et de l'extension |
| Expiration après 7 jours | l'appli ne s'ouvre plus | rafraîchir chaque semaine (section 6) |
| Pas d'App Groups fiables, pas de push | — | extension autonome, sans App Group, et n'en ajouter aucun (ADR-0014) ; notifications locales (ADR-0013) |

## En cas de problème

| Symptôme | Piste |
| --- | --- |
| L'extension n'apparaît pas dans la feuille de partage | feuille de partage › **Modifier les actions** › activer « Hippo » |
| L'extension n'est pas proposée pour deux photos, un fichier ou un PDF de Fichiers | normal : elle accepte un lien, un texte ou une seule image ([spec](spec-v1.md#application-mobile)) |
| « Serveur injoignable » dans l'extension | Tailscale actif sur l'iPhone ? LocalDevVPN coupé ? `Tailscale serve status` sur le Mac ? Le service tourne sur le NAS ? |
| « Accès refusé » dans l'extension | le token de l'appli n'est plus celui du serveur : reconstruire avec le bon token (section 5, « Changer d'URL ou de token ») |
| `make ipa` : « HIPPO_API_TOKEN is missing » ou « must have at least 32 characters » | token non défini dans ce terminal, ou mal collé : reprendre l'étape 2 de « Construire le `.ipa` » |
| SideStore : erreur de connexion ou d'AFC | LocalDevVPN connecté **et** Wi-Fi actif ; sinon remplacer le fichier d'appairage |
| SideStore : « Failed to sign in », erreur `-80045` | voir ci-dessous |
| L'appli ne s'ouvre plus | elle a expiré : rafraîchir (section 6) |

### Erreur `-80045` à la connexion de SideStore

Apple refuse ces connexions depuis mi-octobre 2026. Le contournement de [SideStore#1675](https://github.com/SideStore/SideStore/issues/1675) a marché le 2026-10-26. Dans SideStore, LocalDevVPN connecté :

1. User Customizations › désactiver **On-Device Anisette** ; accepter le redémarrage.
2. Anisette servers › choisir le n° 5, **SideStore (New)** ; tout en bas, désactiver **Enable auto rotation**.
3. User Customizations › Anisette client configuration › **Remote server sync** › **Fetch**, puis **Save as overrides**.
4. User Customizations › **Reset adi.pb**.
5. Se reconnecter : c'est plus lent que d'habitude.

Si ça ne suffit plus : réinstaller l'appli depuis Xcode chaque semaine (section 3). C'est possible, mais demande le Mac : ADR-0013 l'avait écarté pour cette raison.
