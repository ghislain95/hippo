# ADR-0012 — Appli mobile en Expo (React Native)

- **Statut** : Accepté — précisé par [ADR-0036](0036-extension-de-partage-swift.md) (extension de partage en Swift, sans `expo-share-extension`)
- **Date** : 2026-10-10

## Contexte

L'usage principal est sur iPhone. L'appli doit recevoir des partages depuis les autres applis (feuille de partage iOS), fonctionner hors ligne et offrir une lecture soignée. Ghislain connaît React et veut pouvoir relire le code.

## Décision

- **Expo (React Native)**, avec un *development build* (pas Expo Go, qui ne permet pas d'extension de partage).
- Librairies prévues : `expo-router` (navigation), TanStack Query (requêtes et cache), `expo-sqlite` (stockage local, ADR-0018), `react-native-reanimated` (animations), `expo-haptics` (retour haptique), `expo-share-extension` (extension de partage), plus une librairie de coloration syntaxique.
- Client d'API TypeScript **généré** depuis le contrat OpenAPI (ADR-0017).

## Conséquences

- Stack familière, code relisible, une base de code qui pourrait aussi viser Android plus tard.
- L'extension de partage passe par un plugin Expo ; c'est le principal risque technique, validé par un prototype jetable avant tout le reste (voir backlog, epic E1). Repli : une extension native Swift minimale.

## Alternatives écartées

- **SwiftUI natif** : meilleure intégration iOS, mais enfermé sur iOS et dans une techno que Ghislain ne peut pas relire. Option de repli si Expo bloquait.
- **PWA** : iOS ne permet pas à une web app de recevoir des partages, ce qui est le cœur de la V1.
