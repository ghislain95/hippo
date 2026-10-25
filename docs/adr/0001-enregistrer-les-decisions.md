# ADR-0001 — Enregistrer les décisions d'architecture

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Hippo est développé presque entièrement par des agents IA. Un agent qui démarre une session ne connaît pas l'historique : sans trace écrite, il peut « améliorer » un choix délibéré (remplacer SQLite par Postgres, ajouter un framework web) ou rouvrir un débat déjà tranché.

## Décision

Chaque décision structurante fait l'objet d'un ADR court dans `docs/adr/`, au format :

- **Statut** : Proposé, Accepté, Remplacé par ADR-xxxx, ou Abandonné.
- **Contexte** : le problème et les contraintes.
- **Décision** : ce qu'on fait.
- **Conséquences** : ce que ça implique, y compris les inconvénients assumés.
- **Alternatives écartées** : et pourquoi.

Les fichiers sont numérotés (`0001-titre-court.md`) et listés dans [`README.md`](README.md). Un ADR accepté n'est pas réécrit : on en crée un nouveau qui le remplace.

## Conséquences

- Les agents doivent lire les ADR concernés avant de modifier une partie du système, et ne jamais contredire un ADR accepté sans en proposer un nouveau.
- Changer d'avis reste possible, mais explicite et daté.

## Alternatives écartées

- **Décisions uniquement dans la spec** : la spec décrit l'état voulu, pas le pourquoi ni les options rejetées, qui sont justement ce qui évite les retours en arrière.
