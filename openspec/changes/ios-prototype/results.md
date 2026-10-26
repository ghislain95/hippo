# Prototype iOS E1 — QA et résultats

Checklist des essais de Ghislain sur l'iPhone, puis les résultats observés. La procédure durable est dans [`docs/runbook-ios.md`](../../../docs/runbook-ios.md). Les commandes ci-dessous portent sur le code jetable de `spikes/e1-ios/`, retiré avant la PR (il reste dans l'historique de la branche `docs/ios-prototype`).

## Préparation sur le Mac

1. Runbook §1 (Xcode) et §2 (Tailscale), jusqu'à `serve --bg 8787`. Noter l'adresse `https://<machine>.<tailnet>.ts.net`.
2. Lancer le serveur d'écho, dans un terminal qui reste ouvert :

   ```bash
   cd spikes/e1-ios/echo && go run .
   ```

3. Configurer le build pour l'adresse Tailscale (le token est créé au premier passage) :

   ```bash
   cd spikes/e1-ios/app && HIPPO_SPIKE_URL=https://<machine>.<tailnet>.ts.net npm run configure
   ```

4. Installer depuis Xcode, iPhone branché (runbook §3 et §4) :

   ```bash
   cd spikes/e1-ios/app && HIPPO_APPLE_TEAM_ID=<équipe> npx expo run:ios --device --configuration Release
   ```

5. Pour SideStore (partie F), construire le `.ipa` non signé :

   ```bash
   cd spikes/e1-ios/app && npm run ipa
   ```

Chaque essai laisse une ligne `capture received` dans le terminal du serveur d'écho. Les images reçues sont dans `spikes/e1-ios/echo/received/`.

## Checklist

Colonne « Observé » : ce qui s'est passé, même quand c'est conforme. Colonne « OK » : ✅ ou ❌.

### A. Signature et installation avec le compte gratuit (E1-01)

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| A1 | Installation depuis Xcode avec la Personal Team | le build signe l'appli **et** l'extension, l'appli s'ouvre sur deux onglets | | |
| A2 | Feuille de partage de Safari | « Hippo E1 » apparaît (sinon : Modifier les actions) | | |

### B. Partage vers le serveur, en HTTPS par Tailscale (E1-01, E1-03)

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| B1 | Safari, une page, avec une note de capture, en Wi-Fi | « Enregistré » ; log `form=link`, `user_note_length` > 0 | | |
| B2 | Même essai **en 4G**, Wi-Fi coupé, Tailscale actif | idem | | |
| B3 | Instagram, partager une publication | l'extension apparaît ; noter ce qui arrive (`form`, `host`, texte pré-rempli dans la note) | | |
| B4 | Instagram, partager un reel | idem | | |
| B5 | Notes, partager un texte sans lien | aperçu en note, pas de champ « note de capture » ; log `form=note` | | |
| B6 | Photos, partager une capture d'écran | log `form=image`, `image_type=image/png` ; le fichier de `received/` s'ouvre et est intact | | |
| B7 | Photos, partager une grande photo (HEIC) | envoi réussi ; noter la mémoire affichée | | |
| B8 | Un partage avec « Privé » coché | log `is_private=true` | | |

### C. Échecs (RG-06)

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| C1 | Mode avion, partager une page Safari | message clair « Serveur injoignable » ; « Copier le lien » met l'URL dans le presse-papiers | | |
| C2 | Tailscale coupé, réseau actif | même message | | |
| C3 | « Réessayer » après avoir réactivé le réseau | « Enregistré » | | |

### D. Appli et réseau

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| D1 | Onglet Réseau, `/ping`, en 4G par Tailscale | « Joignable · HTTP 200 » ; noter la latence | | |
| D2 | Appli loin du Mac (Metro éteint) | elle s'ouvre normalement (JavaScript embarqué) | | |

### E. Rendu natif (choix de la stack, E5)

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| E1 | Version d'iOS de l'iPhone | — | | |
| E2 | Onglet Verre : carte et pastilles en verre liquide, effet au toucher, styles et teintes | rendu jugé « natif » | | |
| E3 | Contrôles SwiftUI (sélecteurs, interrupteur, curseur, boutons verre) | rendu et réactivité jugés « natifs » | | |
| E4 | Barre d'onglets native | verre liquide, se réduit au défilement | | |

### F. SideStore (E1-02)

| # | Essai | Attendu | Observé | OK |
| --- | --- | --- | --- | --- |
| F1 | Installer SideStore (runbook §5) | SideStore s'ouvre, rafraîchi une fois | | |
| F2 | Supprimer l'appli installée par Xcode, puis installer le `.ipa` dans SideStore (runbook §6) | l'appli **et** l'extension marchent (refaire B1) | | |
| F3 | Identifiants de bundle après installation par SideStore | noter s'ils ont changé (SideStore › Réglages › App IDs) | | |
| F4 | Nombre d'App ID consommés | noter le compteur (l'extension compte-t-elle ?) | | |
| F5 | Rafraîchissement avec la bascule Tailscale ↔ LocalDevVPN (runbook §7) | rafraîchi ; puis B2 marche à nouveau ; noter la durée | | |
| F6 | Rafraîchissement automatique en arrière-plan, quelques jours plus tard | noter s'il a eu lieu | | |

## Mesures

| Mesure | Lien (B1) | Lien en 4G (B2) | Capture d'écran (B6) | Grande photo (B7) |
| --- | --- | --- | --- | --- |
| Temps depuis l'ouverture de l'extension | | | | |
| Mémoire de l'extension | | | | |

Repère : la capture doit tenir en moins de 5 secondes (objectif de la spec V1). La limite de mémoire d'une extension de partage est d'environ 120 Mo.

## Résultats

*À remplir après les essais.*

## Décision (E1-04)

*À remplir après les essais.*
