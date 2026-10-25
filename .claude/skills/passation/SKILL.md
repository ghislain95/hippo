---
name: passation
description: Écrit un document de passation pour reprendre le travail dans une session neuve (/clear ou nouvelle conversation) sans traîner les impasses du contexte actuel. Fichier dans .claude/passation/ + copie dans le presse-papier.
argument-hint: "[objectif de la prochaine session]"
disable-model-invocation: true
allowed-tools: Write, Bash(mkdir -p .claude/passation), Bash(date *), Bash(LC_ALL=en_US.UTF-8 pbcopy *), Bash(LC_ALL=en_US.UTF-8 pbpaste), Bash(LC_ALL=en_US.UTF-8 pbpaste *), Bash(cmp *), Bash(echo OK), Bash(git status *), Bash(git log *), Bash(git branch *), Bash(openspec list), Bash(openspec list *)
---

# Passation

Repris de wifisense (2026-10-25), adapté à Hippo.

Pourquoi : `/compact` résume la conversation, mais en garde la forme : impasses, hypothèses fausses et debugs ratés restent dedans. Une passation repart de zéro avec seulement ce qui compte. La session suivante doit pouvoir reprendre **exactement** là où on en est, sans relire cette conversation.

Si `$ARGUMENTS` est fourni, il décrit l'objectif de la prochaine session : adapter tout le document à cet objectif.

## 1. Vérifier avant d'écrire (pas de mémoire seule)

- `git status --short`, `git log --oneline -5` et `git branch --show-current` pour l'état réel.
- `openspec list` si un change OpenSpec est en cours.
- Toute affirmation « c'est fait » est confirmée par un fichier, un test lancé ou un commit. Sinon elle est marquée **supposé**.

## 2. Écrire le fichier

Chemin : `.claude/passation/AAAA-MM-JJ-HHMM-<sujet-kebab>.md` (dossier gitignoré ; `mkdir -p` si absent). L'heure se lit avec `date +%Y-%m-%d-%H%M`, jamais de mémoire. Prose en **français** ; identifiants, chemins et commandes tels quels.

Structure fixe. Une section vide s'écrit « — » ; on ne la supprime pas.

```md
# Passation — <sujet> (<AAAA-MM-JJ HH:MM>)

> Session suivante : lis ce document, reformule-moi l'objectif et la prochaine étape,
> puis **attends mon GO** avant d'agir.

## Objectif
<1–2 phrases : ce qu'on cherche à obtenir, et pourquoi.>

## Où on en est
- Change OpenSpec : `<id>` — étape : cadrage | passe adversariale | ff | apply | review | verify | commit | archive | post-archive | PR (ou « aucun »)
- Story de la backlog : `<E?-??>`
- Branche : `<branche>` — état git : <propre | fichiers modifiés non commités : lesquels>
- <2–4 puces d'état factuel>

## Décisions prises
- <décision> — pourquoi : <raison> — ✅ validée par Ghislain | 🟡 proposée, pas validée

## Tenté et raté (ne pas refaire)
- <tentative> → <pourquoi ça a échoué / ce que ça a appris>

## Faits établis
- ✔ vérifié : <fait> (preuve : `<fichier:ligne>` | `<commande>` | commit `<sha>`)
- ? supposé : <fait non vérifié>

## Fichiers clés
- `<chemin>` — <rôle en une ligne>

## Questions ouvertes (pour Ghislain)
- <question>

## Prochaine étape
1. <action concrète et immédiate>

## Mode de travail impératif
- Doc et échanges en français ; code, commits, artefacts OpenSpec en anglais (ADR-0026, ADR-0028).
- La doc fait foi : `docs/metier.md` pour le comportement, les ADR pour la technique ; une règle manquante se demande, ne s'invente pas.
- Cadrage avec Ghislain, proposal validé avant `/opsx:ff` ; ensuite, mener le change jusqu'à la fusion sans demander de GO entre les tranches, sauf cas d'arrêt de `docs/dev/workflow.md`.
- `code-reviewer` sur le diff avant `/opsx:verify` ; `/dod-check` avant la PR.
- Git selon `docs/dev/git.md` : branche `<type>/<change-id>`, jamais sur `main`, aucune mention de Claude, fusion `--merge` seulement CI verte (`ci-ok`) et revue sans 🛑/🔴.
- `/eval-prompt` coûte de l'argent : accord de Ghislain avant chaque passage.
- `graphify check-update .` avant de se fier au graph.
```

## Règles de contenu

- **Ne pas recopier ce qui existe déjà** (proposal, design, tasks, specs, ADR, commits) : pointer par chemin. La passation contient ce qui n'est écrit nulle part ailleurs : décisions orales, impasses, hypothèses, intentions.
- **Impasses = section la plus précieuse.**
- **Statuts honnêtes** : une décision évoquée mais pas confirmée est 🟡.
- Masquer les secrets (clés, tokens) et tout contenu personnel d'élément.
- Dense : 40 à 120 lignes, pas de récit chronologique.

## 3. Livrer

1. `LC_ALL=en_US.UTF-8 pbcopy < <chemin>` (le préfixe `LC_ALL` est obligatoire : sans locale, `pbcopy` lit l'UTF-8 comme du MacRoman). Vérifier : `LC_ALL=en_US.UTF-8 pbpaste | cmp -s - <chemin> && echo OK` ; s'il échoue, le dire.
2. Répondre en 3 lignes : le chemin, « copié dans le presse-papier (vérifié) », et la phrase à coller : `Lis .claude/passation/<fichier>.md et reprends exactement où on en était.`

Ne lancer aucun autre travail : la passation clôt la session.
