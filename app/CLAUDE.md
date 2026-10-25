# app/ — appli Expo

Appli iPhone de capture et de lecture, Expo + React Native + TypeScript strict, development build ([ADR-0012](../docs/adr/0012-appli-expo.md)). Conventions complètes : [`docs/dev/conventions-app.md`](../docs/dev/conventions-app.md). Tests : [`docs/dev/testing.md`](../docs/dev/testing.md).

## Règles critiques

- Tout HTTP passe par `src/api/client.ts` (openapi-fetch) ; `src/api/schema.gen.ts` ne s'édite jamais (`make generate`).
- État serveur dans TanStack Query ; configuration (URL, token) injectée au build par `app.config.ts`, jamais en dur.
- Textes de l'interface en français, vocabulaire du glossaire ; identifiants en anglais.
- Pas d'`any`, de `@ts-ignore` ni d'`eslint-disable` sans raison écrite.
- Couleurs et tailles depuis le thème ; accessibilité (labels, Dynamic Type, contraste) ; écrans maquettés avant d'être codés.
- Un test de rendu par template de fiche, sur l'exemple de son type.
- Gestionnaire de paquets : **npm** (`package-lock.json`).

## Commandes (depuis la racine du dépôt)

```bash
make app                    # serveur Expo pour le development build
make check-app              # eslint + tsc --noEmit + jest
make test-app               # jest
cd app && npx expo run:ios  # compile et installe le development build (Xcode requis)
```
