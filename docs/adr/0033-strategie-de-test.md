# ADR-0033 — Stratégie de test : Go standard et jest-expo

- **Statut** : Accepté
- **Date** : 2026-10-25

## Contexte

geomni et wifisense testent leurs règles métier en Gherkin (Cucumber), avec Testcontainers. Hippo a un backend Go sur SQLite et une appli Expo. ADR-0004 interdit de remplacer SQLite par un mock dans les tests.

## Décision

- **Backend** : tests Go standard (`testing`, `httptest`), sans framework. Les tests d'intégration tournent sur une **vraie base SQLite** sur fichier temporaire, migrations réelles appliquées. Chaque scénario d'une spec OpenSpec devient un sous-test nommé d'après lui.
- **Appli** : jest-expo et React Native Testing Library ; un test de rendu par template de fiche.
- **Prompt** : jeu de test dédié (`/eval-prompt`), hors CI car payant.
- Aucun réseau en test ; l'IA est remplacée par une fausse implémentation de l'interface `Analyzer`.
- Détail : [`docs/dev/testing.md`](../dev/testing.md).

## Conséquences

- Les tests se lancent avec `go test`, sans conteneur ni dépendance : rapides en local et en CI.
- La traçabilité scénario → test se fait par le nom du sous-test, pas par un fichier `.feature`.
- Les scénarios Gherkin des specs OpenSpec restent lisibles par Ghislain ; leur traduction en Go est littérale.

## Alternatives écartées

- **godog (Gherkin en Go)** : cohérent avec les autres projets, mais une couche de glue (steps, contexte partagé) qui double le code de test pour un seul développeur.
- **Vitest pour l'appli** (comme les deux autres projets) : mal adapté à React Native ; jest-expo est l'outil de référence d'Expo.
- **Base en mémoire ou mock du store** : interdit par ADR-0004.
