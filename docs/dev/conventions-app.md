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
│   └── ui/                 composants transverses (ThemedText, Screen) et thème (theme/tokens.json)
├── plugins/                plugins de configuration Expo locaux (with-share-config.js)
├── scripts/                builds du simulateur et du .ipa (make build-ios-sim, make ipa)
├── targets/                extension de partage en Swift (voir « Swift de l'extension »)
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

## Configuration au build

L'URL du serveur et le token sont **injectés au build**, pour l'appli et pour l'extension ([ADR-0014](../adr/0014-extension-de-partage-autonome.md), [ADR-0016](../adr/0016-authentification-token.md)). Jamais écrits en dur, jamais commités. Procédure de Ghislain pour l'iPhone : [runbook iOS](../runbook-ios.md#5-construire-et-installer-lappli-avec-sidestore).

| Variable | Rôle | Si absente | Règle vérifiée au prebuild |
| --- | --- | --- | --- |
| `HIPPO_API_URL` | URL du serveur | `http://127.0.0.1:8080` (backend local) ; vide vaut absente | `https`, ou `http` sur `127.0.0.1` ou `localhost` seulement (simulateur) ; ni espace ni caractère de contrôle ; slashs finaux retirés |
| `HIPPO_API_TOKEN` | token d'API (`Authorization: Bearer`) | le prebuild échoue | 32 caractères au moins, seulement `A-Z a-z 0-9 - . _ ~ + / =` |
| `HIPPO_APPLE_TEAM_ID` | `ios.appleTeamId`, pour un build signé | avertissement de `@bacons/apple-targets` au prebuild, sans effet sur un build non signé | aucune ; jamais commité |

- **Appli** : `app.config.ts` lit les variables et les expose dans `extra`, lu par `src/config.ts`. Le `Info.plist` de l'appli reçoit `NSAllowsLocalNetworking` seulement quand l'URL est en `http`.
- **Extension** : le plugin local `app/plugins/with-share-config.js` (CommonJS, `expo/config-plugins`, enregistré après `@bacons/apple-targets`) vérifie les variables puis écrit `app/targets/share/BuildConfig.generated.swift` (`BuildConfig.serverURL`, `BuildConfig.apiToken`). Ce fichier contient le token : il est ignoré par git et ne s'édite jamais.
- **Une seule règle d'URL** : `app.config.ts` utilise `resolveApiUrl` du plugin, si bien que l'appli et l'extension reçoivent la même URL.
- **Quand le plugin vérifie** : seulement à `expo prebuild`. Évaluer la configuration (`expo config`, `expo-doctor`, jest) ne vérifie rien : la CI n'a pas ces variables.
- **Échecs du prebuild** : token absent ; token trop court ou avec un caractère hors liste ; URL invalide, avec un espace ou un caractère de contrôle ; URL `http` vers une autre adresse que `127.0.0.1` ou `localhost`. Le message nomme la variable et la règle, jamais la valeur.
- **Changer l'URL ou le token** demande `expo prebuild --clean` puis un build. Les deux cibles ci-dessous font les deux dans une seule commande, avec le même environnement : l'appli et l'extension ne divergent jamais. Le build depuis l'interface de Xcode n'est pas pris en charge : ses phases de build ne voient pas les variables du terminal, et l'appli embarquerait un token vide.
- Le token reste lisible dans l'appli et l'extension construites, comme dans le bundle JavaScript : accepté pour un usage personnel ([ADR-0016](../adr/0016-authentification-token.md), [ADR-0027](../adr/0027-depot-public.md)). Un `.ipa` ne se publie jamais.

| Cible | Ce qu'elle fait | Variables | Sortie |
| --- | --- | --- | --- |
| `make build-ios-sim` | `expo prebuild --clean -p ios`, puis `xcodebuild` Debug pour `generic/platform=iOS Simulator`, sans signature | `HIPPO_API_TOKEN` (n'importe quel token valide, par exemple `openssl rand -hex 32`) ; URL locale par défaut | `app/build/DerivedData/…/Hippo.app` |
| `make ipa` | refuse une `HIPPO_API_URL` qui n'est pas en `https` avant le prebuild ; `expo prebuild --clean -p ios` ; `xcodebuild archive` Release pour `generic/platform=iOS`, sans signature ; échoue si `PlugIns/HippoShare.appex` manque | `HIPPO_API_URL` en `https`, `HIPPO_API_TOKEN` | `app/build/Hippo.ipa` et `app/build/Hippo.ipa.zip` (pour AirDrop) |

Les deux scripts (`app/scripts/`) exportent `LANG=en_US.UTF-8` s'il n'est pas défini (CocoaPods refuse un autre encodage). Tout ce qu'ils produisent va dans `app/build/`, ignoré par git.

La CI ne construit pas la cible de l'extension : un change qui touche l'extension, `app.config.ts`, un plugin ou une dépendance native passe `make build-ios-sim` en local avant la PR.

## Fiches et templates

- Le format d'une fiche vient de son schéma (`docs/prompt/schemas/fiches/`) ; le template l'affiche, il ne l'interprète pas.
- Un template par type de contenu, plus `generic` pour les types proposés ou sans template ([ADR-0011](../adr/0011-types-de-contenu-et-templates.md)). Un template doit savoir afficher toutes les versions de schéma encore présentes en base.
- Code recopié tel quel, coloré et copiable ; versions en badges ; mentions « contenu partiel » et « contenu tronqué » visibles (RG-37, RG-24) ; une fiche trop longue n'arrive jamais dans l'appli, l'élément est en échec avec sa raison (RG-36).
- La fiche n'est jamais éditable (RG-44) ; l'original est à un geste (RG-45).
- Chaque template a un test de rendu sur l'exemple de son type ([testing.md](testing.md)).

## Interface

- **Textes en français**, vocabulaire du [glossaire](../glossary.md) (élément, fiche, épingle, revue, À trier ; jamais « favori », « article », « dossier »).
- Thèmes clair et sombre, qui suivent l'apparence d'iOS (clair quand iOS n'en donne aucune) ; le choix forcé arrive avec Réglages (E5-08).
- Couleurs, tailles et espacements uniquement depuis le thème (`src/ui/theme`, `useTheme()`). `src/ui/theme/tokens.json` est la seule source des couleurs : l'appli le lit, et la configuration de la cible de l'extension en tire ses couleurs (un test jest vérifie qu'elles sont identiques).
- La taille du texte suit le réglage iOS (Dynamic Type) : tout texte passe par `ThemedText` ; jamais `allowFontScaling={false}` ni `maxFontSizeMultiplier`. Un écran défile plutôt que de couper son texte (`Screen`).
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
- **`@bacons/apple-targets` est épinglé à `5.0.0` exactement** ([ADR-0036](../adr/0036-extension-de-partage-swift.md)) : il réécrit le projet Xcode à chaque prebuild. Le monter de version est un change délibéré, avec `make build-ios-sim` et la vérification de l'override ci-dessous. Il apporte un module natif, `ExtensionStorage`, inutilisé.
- **Override `@xmldom/xmldom`** (décidé par Ghislain le 13 novembre 2026) : `@bacons/apple-targets` 5.0.0 tire `@bacons/xcode`, dont `@expo/plist` épingle `@xmldom/xmldom` `~0.7.0`, touché par une faille haute (`npm audit`, avis jusqu'à 0.8.14). `app/package.json` force la version corrigée sur cette seule branche : `"overrides": { "@bacons/xcode": { "@xmldom/xmldom": "0.8.15" } }`, pour ne pas faire descendre la 0.9 utilisée ailleurs. Seul `DOMParser.parseFromString` sert, inchangé en 0.8 ; le prebuild lit `share/Info.plist` correctement avec. **Retirer l'override** quand `@bacons/apple-targets` dépend lui-même d'une version corrigée. Les alertes modérées restantes viennent d'outils du prebuild, rien n'est embarqué dans l'appli.

## Development build

L'appli ne tourne pas dans Expo Go (l'extension de partage l'exige, ADR-0012). `npx expo run:ios` compile le development build (Xcode requis), puis `make app` lance le serveur de développement. Les dossiers natifs `ios/` et `android/` sont générés (`expo prebuild`) et ne sont pas versionnés. Tout prebuild exige `HIPPO_API_TOKEN` ([Configuration au build](#configuration-au-build)).

## Swift de l'extension

L'extension de partage est en Swift et SwiftUI ; sa cible Xcode est générée par `@bacons/apple-targets` à chaque prebuild ([ADR-0036](../adr/0036-extension-de-partage-swift.md)). Son comportement : [spec, « Application mobile »](../spec-v1.md#application-mobile).

```
app/targets/
├── Package.swift          paquet SwiftPM : bibliothèque ShareCore (share/Core) et tests ShareCoreTests
├── .swift-format          règles de swift format, versionnées
├── share/                 la cible HippoShare : tout le dossier est compilé dans l'extension
│   ├── expo-target.config.js, Info.plist   configuration de la cible, règles d'activation
│   ├── ShareViewController, ShareModel, ShareView, AttachmentLoader, CaptureSender   UIKit, SwiftUI, réseau
│   └── Core/              logique pure
└── ShareCoreTests/        tests swift test, hors de la cible
```

### Disposition

- **Tout `share/` est la cible** : `@bacons/apple-targets` y compile chaque fichier, récursivement, sauf `Info.plist` et `expo-target.config.js`. Aucun `Package.swift`, aucun test, aucun import de `XCTest` ou `Testing` sous `share/` : il serait compilé dans l'extension.
- **`share/Core/` est compilé deux fois** : dans l'extension, dans le même module que le reste (aucun `import ShareCore`), et par SwiftPM comme bibliothèque `ShareCore`, que les tests importent par `@testable import ShareCore`. SwiftPM refuse des sources hors de la racine du paquet : c'est pourquoi `Package.swift` est dans `app/targets/`.
- **`Core` ne dépend que de Foundation, UniformTypeIdentifiers et ImageIO**. Il porte toute décision : forme de la capture, choix du format d'image, contrôles de taille, corps des requêtes, identifiant de capture, classement des réponses, transitions de la feuille et textes français des écrans.
- **La couche UIKit et SwiftUI** (`ShareViewController`, `ShareModel`, `ShareView`, `AttachmentLoader`, `CaptureSender`) charge, affiche et envoie, sans décider. Compilée par la cible seule, elle se vérifie par `make build-ios-sim` et la QA sur l'iPhone.
- **Deux modes de langage** : la cible est générée en Swift 5 (`SWIFT_VERSION = 5.0`), SwiftPM compile `Core` en Swift 6. `Core` compile sans avertissement dans les deux.
- **Aucune bibliothèque Swift** : l'identifiant de capture (ULID, [ADR-0035](../adr/0035-contrat-identifiants-et-synchronisation.md)) est écrit à la main. Pas de client généré : les deux formes de requête (JSON, multipart) sont écrites à la main, et des tests comparent leurs membres au contrat (`api/openapi.bundled.gen.json`).
- **`Info.plist` versionné et statique** : il porte les règles d'activation ; le prebuild ne le réécrit jamais ; un test le lit.
- **Aucun App Group, aucun `entitlements`** dans `expo-target.config.js` ([ADR-0013](../adr/0013-distribution-ios-gratuite.md), [ADR-0014](../adr/0014-extension-de-partage-autonome.md)). L'extension ne stocke rien : session réseau éphémère, fichiers temporaires supprimés à la fermeture de la feuille.
- **Couleurs et textes** : les couleurs viennent de `tokens.json` ([Interface](#interface)), écrites en jeux de couleurs au prebuild et lues par `Color("success")` ; les textes utilisent les styles de SwiftUI (`.body`, `.headline`), qui suivent Dynamic Type.

### Aucun log

L'extension n'écrit aucun log : ni `print`, ni `os_log`, ni `Logger`, aucun écran de mesure. Un échec se dit à l'écran. `make lint-swift` ne le vérifie pas : la relecture le fait (`git grep -nE 'print\(|os_log|Logger' app/targets/share` doit être vide).

### Format et tests

- **Format** : `swift format`, fourni avec Xcode (aucun paquet). Ses règles sont dans `app/targets/.swift-format`, configuration complète figée (2 espaces, 100 colonnes) pour ne pas bouger avec Xcode. `make lint-swift` (strict) lit les fichiers `.swift` que git n'ignore pas, suivis ou nouveaux ; `make format-swift` les formate. Le hook lefthook de pre-commit lint les fichiers indexés.
- **Tests** : `make test-swift` (`swift test`, Swift Testing) dans `ShareCoreTests/`. Chaque scénario de la spec `share-extension` a son test. Les images sont générées par le test, sans fichier binaire.
- `make check-swift` (lint et tests) fait partie de `make check`. En CI, le job `swift` tourne sur un runner macOS, quand `app/targets/` ou `api/` change ([testing.md](testing.md#ce-qui-bloque-en-ci)).

### Fichiers générés, ignorés par git

| Chemin | Produit par | Contenu |
| --- | --- | --- |
| `app/targets/share/BuildConfig.generated.swift` | `with-share-config.js`, au prebuild | URL du serveur et **token** |
| `app/targets/share/Assets.xcassets/` | `@bacons/apple-targets`, au prebuild | jeux de couleurs tirés de `tokens.json` |
| `app/targets/.build/`, `app/targets/.swiftpm/` | SwiftPM | build des tests |
| `app/ios/`, `app/build/` | prebuild, scripts de build | projet Xcode, `.app`, `.ipa` |
