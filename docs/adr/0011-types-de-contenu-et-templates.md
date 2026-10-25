# ADR-0011 — Types de contenu avec une fiche et un template dédiés

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Un résumé uniforme ne convient pas à tout : une recette doit garder ses ingrédients et ses étapes, un article technique ses versions et son code, un lieu son adresse. L'appli doit aussi être agréable à lire, avec une vraie mise en page par contenu.

## Décision

- **7 types de départ** : `tech_article`, `tutorial`, `tip`, `recipe`, `place`, `resource`, `note`, plus un type d'affichage `generic`.
- Chaque type a **son schéma de fiche** (voir ADR-0006) et **son template** dans l'appli (code coloré et copiable, ingrédients à cocher, bouton « Ouvrir dans Plans »…).
- L'IA choisit le type ; elle peut en **proposer** un nouveau (ADR-0009). Un type proposé produit une fiche au format générique et s'affiche avec le template générique, jusqu'à ce qu'on lui donne un schéma et un template dédiés.
- Les clés de type sont en anglais (convention de code), les noms affichés en français.

## Conséquences

- Ajouter un type est une opération transverse : schéma, prompt, template, générateur Markdown, tests. Une commande d'agent (`/new-content-type`) la guidera.
- La qualité visuelle repose sur les templates : ils seront maquettés avant d'être codés.

## Alternatives écartées

- **Un format unique (résumé + points clés)** : perd l'essentiel pour les recettes, tutos et lieux.
- **Markdown libre rendu tel quel** : pas de mise en page riche ni d'interactions.
