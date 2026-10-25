# ADR-0026 — Documentation en français, code en anglais

- **Statut** : Accepté — complété par [ADR-0028](0028-langues-du-developpement.md)
- **Date** : 2026-10-13

## Contexte

Le domaine est pensé en français (fiche, revue, épingle). Le code, lui, utilise des librairies et des conventions anglophones. Mélanger les deux dans les identifiants rend le code incohérent ; tout traduire dans la doc la rend moins naturelle pour Ghislain.

## Décision

- **Documentation, interface de l'appli et fiches** : en français.
- **Code** (identifiants, tables, champs, routes, valeurs d'énumération, commentaires) : en anglais. La correspondance entre termes français et identifiants est tenue dans le [glossaire](../glossary.md).
- **Exception** : le terme `fiche`, gardé tel quel dans le code (voir le glossaire pour la justification).
- Les messages de commit et le format des branches seront fixés lors du setup du projet, en cohérence avec l'autre projet de Ghislain.

## Conséquences

- Le glossaire devient une référence obligatoire pour nommer quoi que ce soit.
- Les clés de types de contenu sont en anglais (`recipe`, `place`…), leurs noms affichés en français.

## Alternatives écartées

- **Tout en français, y compris le code** : jure avec l'écosystème et les librairies.
- **Tout en anglais, y compris la doc** : moins naturel pour décrire le métier.
