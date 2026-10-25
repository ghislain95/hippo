# Stratégie de test

Adaptée des playbooks de geomni et wifisense à la stack de Hippo ([ADR-0033](../adr/0033-strategie-de-test.md)). Les principes sont les mêmes : on teste le comportement observable, avec le vrai moteur de base de données, sans réseau, et chaque scénario de spec devient un test. La mécanique change : Go standard au lieu de Cucumber, SQLite sur fichier au lieu de Testcontainers, jest-expo au lieu de Vitest.

## Pyramide

| Couche | Outils | Périmètre | Quand |
| --- | --- | --- | --- |
| **Intégration backend (principale)** | `testing`, `net/http/httptest`, vraie base SQLite sur fichier temporaire, migrations réelles | Un scénario = un comportement observable par l'API : requête HTTP → réponse + état relu par l'API ou la base | Par défaut, pour tout ce que décrit un delta OpenSpec |
| **Unitaire backend** | `testing`, tests en tableau | Calcul pur : normalisation d'URL (RG-05) et de tags (RG-58), coût d'un batch, découpage, validation | Quand un test HTTP serait plus lourd que la règle testée |
| **Appli** | jest-expo, React Native Testing Library | Écrans et composants vus par l'utilisateur ; rendu de chaque template de fiche | Pour tout écran, template ou hook de l'appli |
| **Jeu de test du prompt** | `/eval-prompt` ([eval-set.md](../prompt/eval-set.md)) | Fidélité des fiches, classement, coût, sur ~22 vrais contenus figés | À chaque changement du prompt ou d'un schéma ; **payant**, sur accord |
| **Non testé directement** | — | Code généré (`*.gen.go`, `schema.gen.ts`), `main.go` | Couvert par les tests d'intégration |

**Règle non négociable : jamais de mock de SQLite** ([ADR-0004](../adr/0004-sqlite.md)). Un test qui touche au stockage ouvre une vraie base. C'est rapide : SQLite est un fichier, sans conteneur.

## Backend

### Base de test

- Chaque test (ou sous-test qui écrit) ouvre sa base par le helper du store, sur un fichier de `t.TempDir()`, avec **toutes les migrations** appliquées. Aucune base partagée entre tests : pas de nettoyage à écrire, pas de dépendance à l'ordre.
- Les données sont créées par les fonctions du store ou par l'API elle-même, jamais par du SQL recopié dans le test (le SQL suit les migrations, le test non).
- Pour vérifier un état, on le relit par l'API quand c'est observable ; sinon par une requête du store, jamais par une copie du schéma.

### Serveur de test

Les tests d'intégration démarrent le vrai handler (`httpapi.NewServer(...)`) dans un `httptest.Server`, avec une configuration de test (token connu, base temporaire, fausses dépendances externes). Le token de test est une constante du package de test, jamais une vraie valeur.

### Dépendances externes

- **Aucun accès réseau en test.** Les pages web et transcriptions sont des fichiers enregistrés dans `backend/testdata/` (servis par un `httptest.Server` local si le code fait une requête HTTP).
- **IA** : le pipeline dépend de l'interface `Analyzer` ([ADR-0005](../adr/0005-haiku-batch.md)) ; les tests utilisent une fausse implémentation qui rejoue des réponses `classify_item` enregistrées et **enregistre les requêtes reçues**. Ce n'est pas un mock du stockage : c'est la frontière avec un service tiers.
- **Horloge** : injectée (`func() time.Time`), jamais `time.Now()` dans le code testé quand le résultat en dépend (batch de 2 h, budget du mois, dates de revue).

### Tests obligatoires

Dès que le chemin existe, ces tests sont exigés par la relecture :

| Garantie | Test |
| --- | --- |
| RG-12 — un élément privé n'est jamais envoyé à l'IA | Avec des éléments privés à chaque statut, la fausse `Analyzer` ne reçoit aucune requête qui les contient, en batch comme en analyse immédiate |
| RG-90 — isolation des utilisateurs | Avec deux utilisateurs, aucune route ne lit ni ne modifie les données de l'autre (E4-12) |
| RG-81 — budget | Budget atteint : aucun envoi, ni batch ni immédiat |
| RG-35 — réponse invalide | Rien n'est écrit, une tentative est comptée |
| RG-61 — corrections manuelles | Une réanalyse ne modifie aucun champ de `user_edited` |

### Nommage

- Un scénario OpenSpec = un sous-test dont le nom reprend le titre du scénario : `t.Run("rejects a capture without token", …)`. Retrouver le test d'un scénario se fait par `grep`.
- Tests en tableau (`[]struct{ name string; … }`) quand seuls les cas varient.

### Commandes

```bash
make test-backend                                  # go test -race -cover ./...
cd backend && go test -race -run 'TestCapture/rejects' ./internal/httpapi/
cd backend && go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```

## Appli

- jest-expo + React Native Testing Library. On interroge l'écran comme l'utilisateur : `getByRole`, `getByText`, `getByLabelText`.
- Le réseau est simulé **au niveau du client généré** (réponse conforme au contrat), jamais par un vrai serveur.
- **Templates de fiches** : un test de rendu par template, sur l'exemple du type (`docs/prompt/schemas/examples/<type>.json`). Il vérifie que les informations de la fiche sont affichées (code recopié tel quel, ingrédients et quantités, adresse…), pas une capture d'écran.
- Pas de snapshot géant : il passe quand tout est cassé à l'identique, et casse à chaque espace.

```bash
make test-app            # jest
cd app && npx jest src/features/health
```

## Jeu de test du prompt

Décrit dans [eval-set.md](../prompt/eval-set.md) ; lancé par `/eval-prompt`. Les cas sont figés dans `backend/testdata/eval/<nn-nom>/` (contenu extrait, attendus) et ne dépendent pas d'Internet. Un échec sur la validité du schéma, `must_contain`/`must_not_contain` ou `code_verbatim` bloque le changement. Le passage coûte quelques dizaines de centimes : **accord de Ghislain avant chaque passage**. Il ne tourne pas en CI.

## Fixtures et données de test

| Où | Quoi |
| --- | --- |
| `backend/testdata/` | Pages HTML et transcriptions enregistrées, réponses `classify_item`, images de test |
| `backend/testdata/eval/` | Cas du jeu de test et rapports |
| `docs/prompt/schemas/examples/` | Un exemple de réponse `classify_item` par type de fiche, validé contre le schéma assemblé ; réutilisé par les tests de templates et d'export |
| `app/src/**/__fixtures__/` | Réponses d'API pour les tests de l'appli |

- Données **fictives** uniquement : le dépôt est public. Aucun contenu personnel, aucun vrai token.
- Une fixture qui doit prouver un tri arrive **dans le désordre**.

## Ce qui bloque en CI

Chaque job alimente la porte `ci-ok` ([ADR-0029](../adr/0029-workflow-git-autonome.md)) :

| Job | Bloquant |
| --- | --- |
| backend | golangci-lint, `go test -race ./...`, build |
| app | ESLint, `tsc --noEmit`, jest, `expo-doctor` |
| api | lint Redocly, code Go et TypeScript générés à jour |
| schemas | exemples valides, schéma assemblé à jour |
| openspec | `openspec validate --all`, garde « specs modifiées à la main » |
| migrations | aucune migration existante modifiée, supprimée ou renommée |
| docs | liens internes valides |
| tooling | suites de tests des hooks, shellcheck, actionlint |
| secrets | gitleaks sur l'historique de la PR |
| image | l'image Docker se construit |

## Couverture

Mesurée et affichée (`go test -cover`, `jest --coverage`), **sans seuil bloquant** : un seuil pousse à tester ce qui est facile plutôt que ce qui compte. La relecture vérifie qu'aucun scénario n'est orphelin et que les tests obligatoires existent.

## Anti-patterns

- ❌ Mock du store, base en mémoire partagée, ou SQL recopié pour préparer l'état.
- ❌ Appel réseau réel (site web, API Anthropic) dans un test automatique.
- ❌ `time.Sleep` pour attendre un traitement en arrière-plan : synchroniser (canal, attente bornée sur une condition) ou injecter l'horloge.
- ❌ Test sans assertion utile, ou qui affirme deux choses sans lien.
- ❌ Assertion d'ordre que le contrat n'impose pas ; tri prouvé sur une fixture déjà triée.
- ❌ `t.Skip`, `it.skip`, assertion affaiblie, seuil relevé « pour faire passer » (relecture : 🛑).
- ❌ Test qui dépend de l'ordre d'exécution, de l'heure réelle ou du fuseau de la machine.
- ❌ Snapshot d'écran à la place d'assertions sur ce qui est affiché.
