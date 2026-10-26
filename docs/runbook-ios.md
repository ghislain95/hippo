# Runbook iOS — installer et rafraîchir l'appli

Procédure pour installer l'appli Hippo sur l'iPhone avec un compte Apple gratuit (ADR-0013), puis la garder active. Elle sert à Ghislain, seul, sans l'agent.

> **État** : rédigée le 2026-10-26 pendant le prototype E1, avant les essais sur l'iPhone. Les étapes marquées *(à confirmer en E1)* seront corrigées d'après les résultats du prototype.

## Ce qu'il faut

| Élément | Où | Coût |
| --- | --- | --- |
| Xcode (licence acceptée, `xcode-select` réglé) | Mac | gratuit |
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
3. Relever son identifiant d'équipe (10 caractères), visible dans la fiche de l'équipe. Il est passé au build par `HIPPO_APPLE_TEAM_ID`. Il ne s'écrit pas dans le dépôt.

## 2. Tailscale en HTTPS (une fois)

1. Créer le compte sur [tailscale.com](https://tailscale.com), en se connectant avec l'identifiant de son choix.
2. Ouvrir Tailscale sur le Mac (ou le NAS) et sur l'iPhone, avec le même compte. Les deux apparaissent dans la console d'administration.
3. Console d'administration › DNS : vérifier que **MagicDNS** est actif, puis activer **HTTPS Certificates**.
4. Publier le serveur en HTTPS sur le tailnet. Sur le Mac, la commande `tailscale` est dans l'appli :

   ```bash
   /Applications/Tailscale.app/Contents/MacOS/Tailscale serve --bg 8787
   ```

   L'adresse affichée est de la forme `https://<machine>.<tailnet>.ts.net`. Le certificat est un vrai certificat public : l'appli n'a besoin d'aucune exception ATS.

5. Vérifier depuis l'iPhone, Tailscale actif : ouvrir `https://<machine>.<tailnet>.ts.net/ping` dans Safari. La réponse `401` prouve que le serveur répond, car il demande le token.
6. Pour arrêter la publication : `Tailscale serve --https=443 off`, et `Tailscale serve status` pour la voir.

Le serveur n'est joignable que depuis le tailnet. Rien n'est exposé sur Internet (ADR-0015).

## 3. Préparer l'iPhone (une fois)

1. Brancher l'iPhone au Mac, puis « Se fier à cet ordinateur ».
2. Réglages › Confidentialité et sécurité › **Mode développeur** : activer. L'iPhone redémarre. *(L'option n'apparaît qu'après une première installation depuis Xcode ou iloader.)*

## 4. Installer depuis Xcode (option sans SideStore)

Utile pour un premier essai ou un dépannage. L'appli expire au bout de 7 jours et se réinstalle depuis le Mac.

1. Construire et installer sur l'iPhone branché, en configuration Release (le JavaScript est embarqué, l'appli marche loin du Mac).
2. Au premier lancement : Réglages › Général › VPN et gestion de l'appareil › « Developer App » › l'identifiant Apple › **Se fier**.

## 5. Installer SideStore (une fois)

Source : [documentation SideStore](https://docs.sidestore.io/docs/installation/prerequisites), consultée le 2026-10-26.

1. Sur l'iPhone : installer **LocalDevVPN** depuis l'App Store, l'ouvrir, **Connect**, autoriser la configuration VPN.
2. Sur le Mac : installer **iloader** (fichier DMG de sa page de publication).
3. iPhone branché, ouvrir iloader, se connecter avec l'identifiant Apple, choisir l'iPhone, puis **Install SideStore (Stable)**.
4. Sur l'iPhone : Réglages › Général › VPN et gestion de l'appareil › « Developer App » › l'identifiant Apple › **Se fier** (ou **Autoriser et redémarrer**). Puis activer le mode développeur si ce n'est pas déjà fait.
5. Ouvrir LocalDevVPN, **Connect**. Ouvrir SideStore et se connecter avec le **même** identifiant Apple.
6. SideStore › My Apps : toucher le compteur **7 DAYS** à côté de SideStore pour le rafraîchir une première fois. Accepter la création d'un certificat si on le propose.

Si SideStore signale un fichier d'appairage invalide (après une mise à jour d'iOS, par exemple) : iloader › gestion du fichier d'appairage › exporter, puis l'importer dans SideStore.

## 6. Installer l'appli avec SideStore

1. Construire le `.ipa` non signé de l'appli sur le Mac. SideStore le signe avec le compte gratuit, extension de partage comprise.
2. L'envoyer sur l'iPhone (AirDrop vers Fichiers).
3. Activer LocalDevVPN (Tailscale se coupe : voir section 7), en Wi-Fi.
4. SideStore › My Apps › **+** › choisir le `.ipa`.
5. Réactiver Tailscale.

## 7. Rafraîchir chaque semaine

iOS ne garde **qu'un seul VPN actif**. LocalDevVPN (pour SideStore) et Tailscale (pour joindre le NAS) s'excluent (ADR-0013).

1. En Wi-Fi, ouvrir LocalDevVPN › **Connect**. Tailscale se coupe.
2. SideStore › My Apps › **Refresh All** (ou le compteur **7 DAYS** de chaque appli).
3. Ouvrir Tailscale et le réactiver. LocalDevVPN se coupe.
4. Vérifier : l'appli joint le serveur.

*(À confirmer en E1 : durée réelle, rafraîchissement en arrière-plan.)*

Une appli expirée ne fait rien perdre : les données sont sur le NAS.

## Limites du compte gratuit

| Limite | Effet | Parade |
| --- | --- | --- |
| 3 applis actives, SideStore compris | il reste 2 places | ne garder que Hippo |
| 10 identifiants d'appli (App ID) par période de 7 jours | chaque installation sous un **nouvel** identifiant en consomme ; l'extension de partage compte aussi *(à confirmer en E1)* | ne jamais changer les identifiants de bundle de l'appli et de l'extension |
| Expiration après 7 jours | l'appli ne s'ouvre plus | rafraîchir chaque semaine (section 7) |
| Pas d'App Groups fiables, pas de push | — | extension autonome (ADR-0014), notifications locales (ADR-0013) |

## En cas de problème

| Symptôme | Piste |
| --- | --- |
| L'extension n'apparaît pas dans la feuille de partage | feuille de partage › **Modifier les actions** › activer « Hippo » |
| « Serveur injoignable » dans l'extension | Tailscale actif sur l'iPhone ? `Tailscale serve status` sur le Mac ? |
| SideStore : erreur de connexion ou d'AFC | LocalDevVPN connecté **et** Wi-Fi actif ; sinon remplacer le fichier d'appairage |
| L'appli ne s'ouvre plus | elle a expiré : rafraîchir (section 7) |
