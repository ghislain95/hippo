# Prototype iOS E1 — QA et résultats

Checklist des essais de Ghislain sur l'iPhone, puis les résultats observés. La procédure durable est dans [`docs/runbook-ios.md`](../../../../docs/runbook-ios.md). Les commandes ci-dessous portent sur le code jetable de `spikes/e1-ios/`, ajouté par le commit 372efc3 et retiré par 672cc3e : il reste dans l'historique de `main`.

## Préparation sur le Mac

1. Runbook §1 (Xcode) et §2 (Tailscale), en publiant le serveur d'écho : `serve --bg 8787` à la place de 8080. Noter l'adresse `https://<machine>.<tailnet>.ts.net`.
2. Lancer le serveur d'écho, dans un terminal qui reste ouvert :

   ```bash
   cd spikes/e1-ios/echo && go run .
   ```

3. Configurer le build pour l'adresse Tailscale (le token est créé au premier passage) :

   ```bash
   cd spikes/e1-ios/app && HIPPO_SPIKE_URL=https://<machine>.<tailnet>.ts.net npm run configure
   ```

4. Installer depuis Xcode, iPhone branché (runbook §3) :

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
| F1 | Installer SideStore (runbook §4) | SideStore s'ouvre, rafraîchi une fois | | |
| F2 | Supprimer l'appli installée par Xcode, puis installer le `.ipa` dans SideStore (runbook §5) | l'appli **et** l'extension marchent (refaire B1) | | |
| F3 | Identifiants de bundle après installation par SideStore | noter s'ils ont changé (SideStore › Réglages › App IDs) | | |
| F4 | Nombre d'App ID consommés | noter le compteur (l'extension compte-t-elle ?) | | |
| F5 | Rafraîchissement avec la bascule Tailscale ↔ LocalDevVPN (runbook §6) | rafraîchi ; puis B2 marche à nouveau ; noter la durée | | |
| F6 | Rafraîchissement automatique en arrière-plan, quelques jours plus tard | noter s'il a eu lieu | | |

## Mesures

| Mesure | Lien (B1) | Lien en réseau mobile (B2) | Capture d'écran (B6) | Grande photo (B7) |
| --- | --- | --- | --- | --- |
| Temps depuis l'ouverture de l'extension | non mesuré | non mesuré | non mesuré | non mesuré |
| Durée de l'envoi | non mesuré | 0,18 s (5G) | non mesuré | 0,67 s (5G) |
| Mémoire de l'extension | non mesuré | 26,6 Mo | non mesuré | 28,4 Mo |

Repère : la capture doit tenir en moins de 5 secondes (objectif de la spec V1). La limite de mémoire d'une extension de partage est d'environ 120 Mo. « Temps depuis l'ouverture » compte aussi la saisie de la note ; « Durée de l'envoi » ne compte que le réseau.

## Résultats

### Simulateur (2026-10-26, agent)

iPhone 18 Pro, iOS 27.0, build Debug, serveur d'écho sur `http://127.0.0.1:8787`. Ni Tailscale, ni compte Apple, ni SideStore : ces points restent à l'iPhone.

| Essai | Résultat |
| --- | --- |
| Build de l'appli et de l'extension (`prebuild`, `xcodebuild`) | ✅ sans avertissement dans notre code ; extension sans droits (pas d'App Group) |
| `.ipa` Release non signé (`npm run ipa`) | ✅ 10 Mo, JavaScript embarqué, `PlugIns/HippoShare.appex` présent |
| Onglet Verre | ✅ verre liquide disponible, barre d'onglets native en verre, contrôles SwiftUI |
| Onglet Réseau, `/ping` | ✅ HTTP 200, 23 ms |
| Safari → « Hippo E1 » dans la feuille de partage | ✅ |
| Lien avec une note de capture | ✅ `form=link`, `user_note_length=4` |
| Lien avec « Privé » coché | ✅ `is_private=true` ; envoi 0,05 s ; 24 Mo |
| Photos → capture d'écran PNG de 433 Ko | ✅ `form=image`, `image/png`, fichier reçu identique à l'original (`cmp`) ; 24 Mo |
| Serveur arrêté | ✅ « Serveur injoignable… », « Copier le lien » copie l'URL, « Réessayer » réussit une fois le serveur relancé |
| Texte sans lien (forme note) | non essayé sur le simulateur : B5 sur l'iPhone |

Mémoire mesurée : 24 à 37 Mo, loin de la limite d'environ 120 Mo.

### iPhone (2026-10-26, Ghislain)

iPhone 15, iOS 27.0, build Release installé depuis Xcode avec la Personal Team. Serveur d'écho sur le Mac, publié par `tailscale serve` sur `https://<mac>.<tailnet>.ts.net`. Sources : le journal du serveur d'écho et les relevés de Ghislain.

| # | Essai | Résultat |
| --- | --- | --- |
| A1 | Signature et installation avec le compte gratuit | ✅ un profil « iOS Team Provisioning Profile » pour l'appli et un pour l'extension ; installation par `devicectl` après activation du mode développeur et confiance dans le compte |
| A2 | « Hippo E1 » dans la feuille de partage de Safari | ✅ |
| B1 | Safari, page avec une note de capture, en Wi-Fi | ✅ `form=link`, note de 25 caractères |
| B2 | Partage en 5G par Tailscale | ✅ un lien : envoi 0,18 s, 26,6 Mo |
| B3, B4 | Instagram, publication et reel | ✅ l'extension apparaît ; Instagram partage l'URL elle-même : `form=link`, hôte `www.instagram.com`, rien dans la note |
| B5 | Notes, texte sans lien | ✅ `form=note`, 7 343 caractères, privé |
| B6, B7 | Photos, capture d'écran et grande photo | ✅ `form=image` ; **toutes les images arrivent en `image/jpeg`** (149 Ko, 415 Ko, 1,8 Mo), pas dans leur format d'origine ; grande photo en 5G : envoi 0,67 s, 28,4 Mo |
| B8 | « Privé » coché | ✅ `is_private=true` (deux images) |
| C1 à C3 | Mode avion, Tailscale coupé, Réessayer | ✅ selon Ghislain (l'échec n'atteint pas le serveur ; le nouvel essai arrive) |
| D1 | `/ping` en 5G par Tailscale | ✅ 190 ms |
| D2 | Appli loin du Mac | ✅ build Release, JavaScript embarqué |
| E1 à E4 | Rendu natif | ✅ « l'effet est bon » ; Ghislain préfère un arrondi d'environ un tiers du curseur (environ 16 points au lieu de 24), qui « fait plus fiche » : repère pour les maquettes E5 |
| F1 | Installer SideStore avec iloader | ✅ après un contournement : la connexion de SideStore échouait avec `-80045` (Apple refuse ces connexions depuis mi-octobre 2026) ; le changement de serveur Anisette de [SideStore#1675](https://github.com/SideStore/SideStore/issues/1675) a réglé le problème. Premier rafraîchissement par « 7 DAYS » réussi |
| F2 | Installer le `.ipa` par SideStore, puis partager | ✅ 2026-10-27 : deux liens reçus depuis Safari. Piège : un `.ipa` envoyé par AirDrop est ouvert par iOS comme une appli à installer et donne une icône cassée (« intégrité non vérifiée ») ; l'envoyer dans un `.zip`, que Fichiers décompresse |
| F3 | Identifiants de bundle après SideStore | ⚠️ SideStore ajoute l'identifiant d'équipe : `com.ghislain95.hippo.spike.<équipe>` et `com.ghislain95.hippo.spike.<équipe>.share`. Sans conséquence ici, puisque rien ne dépend de l'identifiant (pas d'App Group, ADR-0014) ; à garder en tête pour toute capacité liée à l'identifiant |
| F4 | App ID consommés | ✅ SideStore signale lui-même que l'extension compte comme un App ID à part (limite de 10 par semaine) ; choix retenu : « Keep app extension (register app ID for each extension) ». Soit 2 App ID par installation d'Hippo |
| F5 | Rafraîchissement avec la bascule Tailscale ↔ LocalDevVPN | ✅ LocalDevVPN, Refresh All, retour à Tailscale : le partage remarche |
| F6 | Rafraîchissement automatique en arrière-plan | à observer : le 2026-10-27, l'installation de la veille affiche « expires in 6 days », sans rafraîchissement automatique pour l'instant ; elle expire le 2026-11-03 |

Constats pour la suite :

- **Capture en moins de 5 s** : l'envoi reste sous 0,7 s en 5G, même pour une grande photo ; le reste du temps est la saisie.
- **Mémoire** : 26 à 29 Mo sur l'iPhone, loin de la limite d'environ 120 Mo.
- **Images converties en JPEG** : l'extension prend le premier type d'image que propose l'app source, et Photos propose du JPEG. Pour E5-02 et RG-02 : décider s'il faut demander le format d'origine (HEIC ou PNG) ou garder le JPEG, plus léger.
- **Instagram** : partage une URL ; le cas « texte avec une URL dedans » n'a pas été rencontré.

## Décision (E1-04)

La chaîne iOS tient avec un compte Apple gratuit. ADR-0013 et ADR-0014 sont confirmés, ADR-0012 est précisé par ADR-0036, et E5 peut avancer.

- **ADR-0014 confirmé** : l'extension envoie elle-même la capture, sans App Group, avec l'URL et le token injectés au build.
- **ADR-0013 confirmé** : installation et rafraîchissement par SideStore, avec la bascule de VPN. La connexion de SideStore reste fragile : l'erreur `-80045` a demandé un contournement.
- **ADR-0036 (nouveau)** : l'extension est écrite en Swift et générée par `@bacons/apple-targets`, à la place de la librairie `expo-share-extension` prévue par la spec.
- **ADR-0015 confirmé en pratique** : `tailscale serve` en HTTPS, sans exception ATS dans l'extension.
- **Pas de compte Apple payant** : rien dans le prototype ne l'a demandé.

Points ouverts pour E5-02 :
- le format des images, toutes reçues en JPEG (RG-02) ;
- les conventions du code Swift (format, lint, tests) ;
- l'arrondi des cartes, plutôt 16 points que 24 d'après Ghislain.
