# ADR-0007 — La fiche JSON est la source de vérité, le Markdown est généré

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Une fiche doit être affichée joliment dans l'appli (un template par type), cherchable, et exportable en Markdown. Stocker plusieurs représentations risque de les désynchroniser.

## Décision

- La fiche est stockée **une seule fois**, en JSON validé, dans `items.fiche_json`.
- Les champs utiles aux filtres et au tri (titre, type, catégorie, dates) sont recopiés dans des colonnes classiques.
- Une version texte « aplatie » de la fiche (`search_text`) est calculée à l'écriture, uniquement pour l'index de recherche.
- Le **Markdown n'est jamais stocké** : il est généré à la demande au moment de l'export, à partir du JSON.

## Conséquences

- Une seule source à faire évoluer ; l'export et la recherche suivent automatiquement.
- Le générateur Markdown doit couvrir chaque type, y compris le générique. Il est testé sur des fiches d'exemple.
- `search_text` doit être recalculé à chaque modification de la fiche (réanalyse).

## Alternatives écartées

- **Stocker du Markdown** : simple, mais impossible d'en tirer des templates riches sans le reparser.
- **Stocker JSON et Markdown** : double source, désynchronisation garantie à la première évolution d'un template.
