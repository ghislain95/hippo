# ADR-0036 — Extension de partage native en Swift

- **Statut** : Accepté
- **Date** : 2026-10-27

## Contexte

L'extension de partage est la porte d'entrée de l'appli (ADR-0014). La spec prévoyait la librairie `expo-share-extension`, qui fait tourner React Native dans l'extension. Au cadrage du prototype E1 (octobre 2026), elle ne convenait plus :

- sa table de compatibilité s'arrête au SDK 54 d'Expo, et l'appli est en SDK 58 ;
- elle crée un App Group par défaut, ce qu'ADR-0013 et ADR-0014 évitent ;
- React Native dans une extension pèse sur la limite de mémoire d'iOS, d'environ 120 Mo.

ADR-0014 nommait déjà le repli : une extension native Swift minimale. Le prototype l'a construite avec `@bacons/apple-targets`, qui génère la cible Xcode depuis un dossier `targets/` du projet Expo à chaque `expo prebuild`. Résultats sur un iPhone 15 sous iOS 27 ([`results.md`](../../openspec/changes/ios-prototype/results.md)) :

- signature avec le compte gratuit, par Xcode puis par SideStore ;
- partage depuis Safari, Instagram, Photos et Notes ;
- envoi en moins de 0,7 s en 5G par Tailscale, grande photo comprise ;
- 26 à 29 Mo de mémoire.

## Décision

- L'extension de partage est écrite en **Swift et SwiftUI**, sans React Native.
- Sa cible Xcode est générée par **`@bacons/apple-targets`** depuis `app/targets/share/` : rien n'est réglé à la main dans Xcode, `expo prebuild --clean` la recrée.
- Elle n'a **aucun droit** (pas d'App Group, pas de trousseau partagé) : l'URL du serveur et le token lui sont injectés au build, et elle envoie elle-même `POST /api/items` dans le format du contrat (ADR-0014, ADR-0017).
- Le `Info.plist` de l'extension limite son activation à une URL, un texte ou une image.

## Conséquences

- Un deuxième langage dans l'appli : le Swift de l'extension suit les conventions de `docs/dev/conventions-app.md`, à compléter en E5-02 (format, lint, tests).
- L'extension ne partage pas de code avec l'appli. Le format des requêtes est tenu par le contrat OpenAPI, pas par du code commun.
- Son build demande Xcode sur le Mac, comme le reste de l'appli (ADR-0013).
- `@bacons/apple-targets` devient une dépendance de l'appli, ajoutée en E5-02.
- Observé pendant le prototype, à trancher en E5-02 :
  - Photos fournit les images en JPEG, même une capture d'écran : faut-il garder ce format ou demander l'original (RG-02) ?
  - SideStore ajoute l'identifiant d'équipe aux identifiants de bundle ; rien ne doit en dépendre.

## Alternatives écartées

- **`expo-share-extension`** (React Native dans l'extension) : non compatible avec le SDK 58 d'Expo, App Group par défaut, plus lourde en mémoire.
- **Cible Xcode créée à la main** : écrasée à chaque `expo prebuild --clean`.
- **Extension qui ouvre l'appli principale** : écartée par ADR-0014 (friction, objectif de capture en moins de 5 secondes).
