# Conventions de l'appli Expo

L'appli est une appli de lecture avant tout ([spec, « Application mobile »](../spec-v1.md#application-mobile)) : Expo, React Native et TypeScript strict, avec un development build ([ADR-0012](../adr/0012-appli-expo.md)). Commandes : `app/CLAUDE.md`.

## Organisation

```
app/
├── src/
│   ├── app/                routes expo-router (un fichier = un écran) et _layout.tsx
│   ├── api/
│   │   ├── schema.gen.ts   types générés depuis api/openapi.yaml (jamais édité)
│   │   └── client.ts       client openapi-fetch : seul point d'accès HTTP
│   ├── config.ts           configuration injectée au build (URL du serveur, token)
│   ├── features/<feature>/ hooks, composants et tests d'une fonctionnalité (health, library, fiche, capture, review…)
│   ├── templates/          un template par type de fiche + le template generic
│   └── ui/                 composants transverses, thème (couleurs, typographie, espacements)
├── app.config.ts           configuration Expo (nom, bundle id, plugins, variables de build)
└── jest.config.js, eslint.config.js, tsconfig.json
```

- Une route (`src/app/`) reste mince : elle assemble des composants de `src/features/` ; aucun code hors route dans `src/app/` (convention d'Expo SDK 58).
- Imports internes par l'alias `@/` (`@/api/client`, `@/features/health/ServerStatus`).
- Ce qui sert à plusieurs fonctionnalités va dans `src/ui/` ; rien n'est dupliqué entre deux features.

## Données

- **Tout appel HTTP passe par `src/api/client.ts`** (openapi-fetch typé par `schema.gen.ts`, [ADR-0031](../adr/0031-generation-depuis-les-contrats.md)). Pas de `fetch` direct.
- État serveur : **TanStack Query** (clés de requête par ressource, invalidation après une mutation). Pas de copie des données serveur dans un état local.
- État d'interface local (filtre ouvert, saisie en cours) : `useState` ou contexte, au plus près de l'usage.
- Cache hors ligne : `expo-sqlite`, synchronisé par `/api/sync` ([ADR-0018](../adr/0018-lecture-hors-ligne.md)) ; arrive avec E5-09.
- Les erreurs de l'API sont des problem+json : l'appli affiche `detail` et reconnaît les `type` métier (`urn:hippo:problem:<slug>`).

## Configuration

- L'URL du serveur et le token sont **injectés au build** ([ADR-0014](../adr/0014-extension-de-partage-autonome.md), [ADR-0016](../adr/0016-authentification-token.md)) : variables `HIPPO_API_URL` et `HIPPO_API_TOKEN` lues par `app.config.ts` et exposées dans `extra`, lues dans `src/config.ts`. Jamais écrites en dur, jamais commitées.
- En développement sur le simulateur : `HIPPO_API_URL=http://127.0.0.1:8080` (backend local).

## Fiches et templates

- Le format d'une fiche vient de son schéma (`docs/prompt/schemas/fiches/`) ; le template l'affiche, il ne l'interprète pas.
- Un template par type de contenu, plus `generic` pour les types proposés ou sans template ([ADR-0011](../adr/0011-types-de-contenu-et-templates.md)). Un template doit savoir afficher toutes les versions de schéma encore présentes en base.
- Code recopié tel quel, coloré et copiable ; versions en badges ; mentions « contenu partiel », « contenu tronqué », « fiche incomplète » visibles (RG-36, RG-37, RG-24).
- La fiche n'est jamais éditable (RG-44) ; l'original est à un geste (RG-45).
- Chaque template a un test de rendu sur l'exemple de son type ([testing.md](testing.md)).

## Interface

- **Textes en français**, vocabulaire du [glossaire](../glossary.md) (élément, fiche, épingle, revue, À trier ; jamais « favori », « article », « dossier »).
- Thèmes clair et sombre (automatique, forçable) ; couleurs, tailles et espacements uniquement depuis le thème (`src/ui/theme`).
- La taille du texte suit le réglage iOS (Dynamic Type).
- Accessibilité : `accessibilityLabel` sur tout contrôle sans texte visible, `accessibilityRole`, cibles d'au moins 44 points, contraste suffisant dans les deux thèmes.
- Les écrans sont maquettés et validés avant d'être codés ; finition avec les skills de design ([agent-toolbox.md](agent-toolbox.md)).

## TypeScript et style

- `strict` activé ; pas d'`any`, pas de `@ts-ignore` ni d'`eslint-disable` sans commentaire qui donne la raison.
- ESLint (`eslint-config-expo`) et Prettier ; le hook formate après chaque édition.
- Composants en fonctions, nommés en PascalCase, un composant exporté par fichier ; hooks préfixés `use`.
- Identifiants et commentaires en anglais.

## Code généré

`src/api/schema.gen.ts` est produit par `make generate` depuis `api/openapi.yaml`, par openapi-typescript installé dans `api/` (il exige TypeScript 5, l'appli est en TypeScript 6). Le hook refuse son édition ; la CI vérifie qu'il est à jour.

## Dépendances

- Toujours `npx expo install <paquet>` (pas `npm install`) : il choisit la version compatible avec le SDK. Une dépendance nouvelle se demande à Ghislain (`docs/dev/workflow.md`).
- Expo change à chaque SDK : avant d'écrire du code qui touche une API Expo ou React Native, lire la doc **de la version installée** (`https://docs.expo.dev/versions/v58.0.0/`), et la doc embarquée des librairies (`node_modules/<lib>/README.md`, `docs/`), plutôt que sa mémoire.
- Pas d'EAS : l'appli se compile sur le Mac et s'installe avec SideStore (ADR-0013).

## Development build

L'appli ne tourne pas dans Expo Go (l'extension de partage l'exige, ADR-0012). `npx expo run:ios` compile le development build (Xcode requis), puis `make app` lance le serveur de développement. Les dossiers natifs `ios/` et `android/` sont générés (`expo prebuild`) et ne sont pas versionnés.
