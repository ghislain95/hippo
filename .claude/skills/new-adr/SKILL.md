---
name: new-adr
description: Create the next architecture decision record in docs/adr/ with the ADR-0001 format and update the index. Use when a structuring decision is taken or an accepted ADR is challenged (a new ADR replaces it, the old one is never rewritten).
argument-hint: "<short title>"
---

# /new-adr — nouvel ADR

Format et règles : `docs/adr/0001-enregistrer-les-decisions.md`.

## Quand
Seulement si les trois tiennent : **difficile à défaire**, **surprenant sans contexte**, **vrai compromis** (des alternatives réelles). Sinon la décision vit dans le `design.md` du change.

## Étapes
1. Numéro : le plus grand `docs/adr/NNNN-*.md` + 1, sur 4 chiffres.
2. Fichier `docs/adr/NNNN-<titre-court-kebab-sans-accent>.md`, en français :

```md
# ADR-NNNN — <Titre>

- **Statut** : Proposé | Accepté
- **Date** : <AAAA-MM-JJ du jour, lue avec `date +%F`>

## Contexte

<le problème et les contraintes, en quelques phrases>

## Décision

<ce qu'on fait, en liste courte>

## Conséquences

- <ce que ça implique, y compris les inconvénients assumés>

## Alternatives écartées

- **<alternative>** : <pourquoi>
```

3. Si l'ADR en remplace ou en amende un autre : dans l'ancien, **seul** le statut change (`Remplacé par ADR-NNNN`, ou une ligne « Amendé par ADR-NNNN : … ») ; son contenu n'est pas réécrit.
4. Ajouter la ligne dans le tableau de `docs/adr/README.md` (numéro lié, décision, statut, ce qu'il concerne).
5. Statut `Proposé` tant que Ghislain ne l'a pas validé ; `Accepté` ensuite.
6. `make check-docs`.
