# Workflow de développement

Comment un change passe de l'idée à `main`, en mode autonome. Ghislain participe au **cadrage** et à la **QA** ; tout le reste (spécification détaillée, code, tests, doc, PR, fusion) est fait par les agents, sous les garde-fous décrits ici.

Documents liés : [conventions git](git.md), [stratégie de test](testing.md), [conventions backend](conventions-backend.md), [conventions de l'appli](conventions-app.md), [boîte à outils des agents](agent-toolbox.md).

## Le cycle d'un change

Une story de la [backlog](../backlog.md) = un change OpenSpec (`openspec/changes/<change-id>/`). L'id est en anglais, en kebab-case (`capture-link`, `nightly-batch`).

| # | Étape | Qui | Commande / agent | Sortie |
| --- | --- | --- | --- | --- |
| 1 | **Cadrage** | Ghislain + session principale | `/opsx:explore` puis `/opsx:new <change-id>` | `proposal.md` |
| 2 | **Validation** | Ghislain | passe adversariale par `architect` si structurant, puis `/opsx:ff` | `specs/`, `design.md`, `tasks.md` validés |
| 3 | **Branche** | session | `git switch -c <type>/<change-id>` | commit `docs(<id>)` du cadrage |
| 4 | **Implémentation** | `backend-dev`, `mobile-dev`, `tester`, `prompt-engineer` | `/opsx:apply` | tâches cochées, tests d'abord |
| 5 | **Vérifications locales** | l'agent qui implémente | `make check` | tout vert |
| 6 | **Auto-relecture** | `code-reviewer` | revue du diff non commité, puis `/opsx:verify` | aucun 🛑 ni 🔴 ouvert |
| 7 | **Doc** | `doc-keeper` | tâches de doc du change | glossaire, spec, `docs/dev/` à jour |
| 8 | **Commits et PR** | session | commits `feat|fix(<id>)`, `/opsx:archive` + post-archive, `/dod-check`, push, `gh pr create` | PR ouverte, CI lancée |
| 9 | **Fusion** | session | `gh pr merge <n> --merge` | sous conditions (voir plus bas) |
| 10 | **Archive et nettoyage** | session | archive déjà commitée sur la branche ; `git switch main && git pull --ff-only`, suppression de la branche | `main` à jour, `openspec/specs/` enrichi |

### 1. Cadrage

- Partir de la story de la backlog, des `RG-xx` qu'elle cite et des ADR concernés. **Les faits sont le travail de l'agent** : on ne demande jamais à Ghislain ce que la doc, le code ou le graph peuvent dire.
- Avant le recon : `graphify update .` puis `graphify check-update .`. Un cadrage sur un graph périmé produit un proposal faux.
- Questions à Ghislain : peu, une à la fois quand c'est possible, chacune avec un exemple concret et une recommandation. Un terme flou passe par le skill `domain-modeling`.
- Le proposal (anglais) contient : intention, périmètre, non-objectifs, story, `RG-xx` et ADR, impacts (coût IA, éléments privés, données, contrat, prompt), « User QA: yes/no ».

### 2. Validation

- **Passe adversariale** pour un change structurant (nouveau contrat, migration, changement de prompt, nouveau mécanisme, choix transverse) : `architect` tente de casser le proposal ; les objections sont résolues dans le proposal avant `/opsx:ff`.
- `/opsx:ff` produit les spec deltas, le `design.md` et le `tasks.md`. **Ghislain valide le proposal ; une fois validé, le change va jusqu'à la fusion sans demander de GO entre les tranches**, sauf cas d'arrêt ci-dessous.

### 3. Branche

`<type>/<change-id>` depuis `main` à jour (`git pull --ff-only` d'abord). Premier commit : `docs(<id>): frame <sujet>` avec les artefacts du change. Détail : [git.md](git.md).

### 4. Implémentation

- Avant `/opsx:apply` : `graphify check-update .`.
- Ordre de `tasks.md` : **contrat d'abord** (`api/openapi.yaml`, migration, schéma de fiche) → `make generate` → **tests qui échouent** → implémentation → doc.
- Chaque scénario d'un delta = un test, nommé d'après le scénario ([testing.md](testing.md)).
- Une déviation du design se reporte dans `design.md` dans le même mouvement.
- Le dispatch d'un agent rappelle : le change, les `RG-xx`, d'interroger graphify avant de chercher à l'aveugle.

### 5. Vérifications locales

`make check` lance exactement ce que lance la CI : secrets, liens de la doc, hooks, OpenSpec, lint et tests du backend et de l'appli, lint du contrat, code généré et schéma assemblé à jour. Un agent ne déclare rien « terminé » sans l'avoir lancé et lu.

### 6. Auto-relecture

- `code-reviewer` sur le diff non commité, **avant `/opsx:verify`** et avant chaque commit de code. Les 🛑 et 🔴 retournent à l'agent concerné, puis nouvelle relecture.
- Diff d'interface : aussi `/web-design-guidelines <fichiers>` ; mouvement : `/review-animations`.
- `/opsx:verify` : implémentation conforme aux deltas, tâches cochées, tests réellement exécutés.

### 7. Documentation

Tâches de doc du change (glossaire, `docs/spec-v1.md`, `docs/dev/`, `CLAUDE.md` d'un dossier) par `doc-keeper`. Le comportement détaillé, lui, vit dans `openspec/specs/` et y entre à l'archive.

### 8. Commits, archive et PR

1. Commits `feat|fix|refactor(<id>): …`, un par tranche cohérente, après relecture.
2. `/opsx:archive` puis **post-archive** :
   - story marquée terminée dans `docs/backlog.md` ;
   - impact doc : `doc-keeper`, ou la phrase « post-archive : aucun impact doc » ;
   - `## Purpose` des specs touchées ou créées corrigé dans le commit d'archive ;
   - graph : `graphify update . --force` si des fichiers ont été supprimés ou renommés, sinon `graphify update .`.
3. Commit `chore(<id>): archive change, sync specs, post-archive`.
4. `/dod-check` vert.
5. `git push -u origin <branche>`, puis `gh pr create` (titre = sujet du commit principal, description en français selon le template).

### 9. Fusion

L'agent fusionne lui-même (`gh pr merge <n> --merge --delete-branch`) **seulement si toutes les conditions tiennent** :

- check `ci-ok` vert sur la PR ;
- dernière relecture `code-reviewer` sans 🛑 ni 🔴 ouvert ;
- `/opsx:verify` OK et change archivé sur la branche ;
- si « User QA: yes » : Ghislain a fait sa QA et l'a validée.

Sinon il s'arrête, explique ce qui manque, et demande.

### 10. Nettoyage

`git switch main && git pull --ff-only`, `git branch -d <branche>`. Depuis un worktree : seule la branche distante est supprimée (par `--delete-branch`) ; le worktree se nettoie depuis le checkout principal ou à l'archivage de la session.

### Hotfix

Correction triviale (moins de 30 minutes, comportement spécifié intact, aucun contrat touché) : sans change OpenSpec, mais sur une branche `fix/<scope>-<desc>`, avec un test de régression, la relecture, `make check` et une PR. Un bug qui contredit une spec ou une `RG-xx` est un change (`/opsx:new fix-<bug>`).

### Dépendances (Dependabot)

Dependabot propose chaque lundi la dernière version publiée de chaque dépendance ([`.github/dependabot.yml`](../../.github/dependabot.yml)). Ce n'est pas une recommandation : c'est la CI qui tranche.

- **Correctif ou version mineure** : si `ci-ok` est vert, l'agent fusionne la PR sans demander ; si elle échoue, il corrige ou ferme la PR en expliquant pourquoi.
- **Version majeure d'un paquet lié à un autre outil** : exclue de Dependabot, et faite volontairement quand l'outil dont elle dépend la supporte. Aujourd'hui : les paquets du SDK Expo et leurs compagnons (`jest`, `eslint`, `react`, `react-native`, `typescript` dans `app/`), à monter ensemble dans un change de montée de SDK (`npx expo install --fix`) ; `typescript` dans `api/`, tant qu'openapi-typescript exige TypeScript 5.
- **Autre version majeure** : l'agent s'arrête et demande (cas d'arrêt n° 3 ci-dessous).

## Quand s'arrêter pour demander

L'agent s'arrête et pose la question à Ghislain (une question, un exemple, une recommandation) dans ces cas, et seulement ceux-là :

1. **Contradiction avec la doc** : le besoin, le code ou une spec contredit `metier.md`, un ADR accepté ou un schéma de fiche.
2. **Règle métier manquante ou ambiguë** : la doc ne dit rien ; on n'invente jamais une règle.
3. **Nouvelle dépendance** (module Go, paquet npm, image, service), ou montée de version majeure.
4. **Changement du prompt ou d'un schéma de fiche**, ou du schéma de base non prévu par le `design.md`.
5. **Action qui coûte de l'argent** : `/eval-prompt`, analyse réelle, service payant, runner payant.
6. **Sécurité** : authentification, token, exposition réseau, secret, permission.
7. **Données** : suppression ou migration de données réelles, envoi de données hors du NAS, contenu personnel dans le dépôt (il est public).
8. **GitHub hors PR** : réglages du dépôt, secrets, protection de `main`, push sur `main`.
9. **ADR à remettre en cause**, ou décision structurante non couverte (elle donnera un ADR).
10. **Conditions de fusion non remplies** après deux tentatives de correction.

Pour tout le reste : prendre la décision la plus raisonnable, la noter dans le `design.md` (ou la PR), et continuer.

## Définition de « terminé »

Un change est terminé quand **chaque** point est vrai et vérifié (`/dod-check` les reprend un par un) :

1. `make check` est vert en local, et `ci-ok` est vert sur la PR.
2. `openspec validate` passe ; `tasks.md` est entièrement coché ; `design.md` décrit ce qui a été fait.
3. Le proposal et les deltas citent les `RG-xx` et ADR concernés ; aucune règle métier n'a été inventée.
4. Chaque scénario des deltas a son test automatisé ; les tests obligatoires du périmètre existent (élément privé jamais envoyé à l'IA, isolation des utilisateurs, budget).
5. Le contrat `api/openapi.yaml` porte toute évolution d'API ; le code généré et le schéma assemblé sont à jour ; un prompt ou un schéma modifié a sa version incrémentée et un rapport `/eval-prompt` sans échec bloquant.
6. Aucune baisse de qualité masquée (test sauté, assertion affaiblie, `nolint`, `eslint-disable`, `any`, erreur avalée) sans justification écrite.
7. La doc est à jour : glossaire, spec, `docs/dev/`, `CLAUDE.md` concernés ; liens valides.
8. La story est marquée terminée dans la backlog ; le change est archivé.
9. Les commits suivent [git.md](git.md) ; aucune mention de Claude ; aucun secret.
10. La relecture `code-reviewer` est sans 🛑 ni 🔴 ; la QA de Ghislain est faite si elle est demandée.

## Garde-fous automatiques

Ce que les agents ne peuvent pas faire, même par erreur ([`.claude/settings.json`](../../.claude/settings.json) et [`.claude/hooks/`](../../.claude/hooks/)) :

- **Git** (`block-git-writes.sh`) : commit ou push sur `main`, force-push, réécriture d'historique, staging en masse, `--no-verify`, fusion autre qu'en merge commit.
- **Fichiers** (`protect-files.sh`) : lire ou écrire `.env` et `.claude/settings.local.json`, y compris par une commande qui les affiche sans les nommer (`grep -r`, `rg --no-ignore`, `docker compose config`, `docker inspect` ; utiliser `git grep` ou `rg`) ; éditer un fichier généré ; écrire dans un dépôt voisin ; `rm -r` hors du dépôt.
- **Formatage** (`format-on-edit.sh`) : chaque fichier Go ou de l'appli est formaté après édition.
- **Pré-commit** (`lefthook.yml`) : gitleaks, format Go, ESLint et Prettier sur les fichiers indexés.
- **CI** : porte `ci-ok`, qui agrège tous les jobs ; seule exigence de la protection de `main`.

Un faux positif se contourne par la bonne pratique, jamais en désactivant le garde-fou : par exemple, un message de commit qui cite un fichier protégé passe par `git commit -F <fichier>`.

## Sessions et contexte

- Contexte qui s'alourdit, changement de sujet : `/passation`, puis session neuve (préféré à `/compact`).
- Après un change difficile : `/retro` (améliorer l'environnement, pas le produit).
- Tous les cinq changes environ, jamais en cours de change : `/improve-codebase-architecture`.
- Les skills de process génériques (`superpowers:brainstorming`, `writing-plans`) ne remplacent jamais `/opsx:new` et `/opsx:ff` ; `systematic-debugging`, `test-driven-development` et `verification-before-completion` restent valables partout.
