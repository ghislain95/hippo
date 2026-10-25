# ADR-0010 — Une seule catégorie par élément, deux niveaux, des tags transversaux

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Beaucoup de contenus touchent plusieurs domaines (« l'IA pour automatiser son infra »). Autoriser plusieurs catégories rend le rangement flou ; une arborescence profonde devient un labyrinthe sur un écran de téléphone.

## Décision

- **Une seule catégorie** par élément : celle du sujet principal.
- **Deux niveaux maximum** : catégorie racine et sous-catégorie.
- **2 à 5 tags** par élément pour tout ce qui est transversal, normalisés par le backend (RG-58).

## Conséquences

- Chaque élément a une place unique et prévisible ; la navigation par catégorie reste simple.
- Les tags portent les liens entre domaines, et serviront aussi à calculer les éléments liés en V1.5.
- La profondeur est un **point ouvert** : si le classement le demande, un 3e voire un 4e niveau pourra être introduit par un nouvel ADR. Le modèle de données (`parent_id`) le permet déjà ; seule la règle de validation changerait.

## Alternatives écartées

- **Plusieurs catégories par élément** : chaque élément apparaît partout, les catégories perdent leur sens.
- **Tags uniquement, sans catégorie** : pas de navigation structurée, difficile à parcourir sur mobile.
