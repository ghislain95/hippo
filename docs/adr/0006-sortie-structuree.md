# ADR-0006 — Sortie structurée forcée et validée

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Le backend doit pouvoir enregistrer chaque réponse de l'IA sans interprétation : type, catégorie, tags, fiche. Une réponse en texte libre, ou un JSON approximatif, casserait l'affichage et la recherche.

## Décision

- L'IA répond **obligatoirement via un outil** (`classify_item`), imposé par `tool_choice`. La réponse est l'entrée de l'outil, donc un objet JSON.
- Le schéma de l'outil est assemblé à partir des fichiers de [`docs/prompt/schemas/`](../prompt/schemas/) : une enveloppe commune et un schéma de fiche par type, distingués par le champ `template`.
- Le backend **valide** chaque réponse contre ce schéma. Une réponse invalide est rejetée entièrement et compte comme une tentative échouée (RG-35).
- Les schémas sont versionnés : chaque type a son `schema_version`, enregistré avec la fiche.

## Conséquences

- Pas de texte superflu facturé, pas de parsing fragile.
- Le schéma devient le contrat entre le prompt, le backend et les templates de l'appli : toute modification d'une fiche commence par lui.
- Les schémas sont la source des types Go et TypeScript des fiches (génération ou vérification en CI).

## Alternatives écartées

- **Demander du JSON dans le texte de la réponse** : fonctionne la plupart du temps, mais sans garantie ; chaque cas raté devient un bug d'affichage.
- **Markdown libre** : impossible à afficher avec des templates riches (ingrédients à cocher, code copiable).
