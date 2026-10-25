# ADR-0024 — Recherche plein texte en V1, sémantique en V1.5

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Retrouver un élément est l'un des usages principaux. La recherche par mots-clés couvre la plupart des cas ; la recherche « par le sens » (« le truc sur les requêtes lentes ») et les éléments liés demandent des vecteurs (embeddings), qu'Anthropic ne fournit pas.

## Décision

- **V1** : recherche plein texte SQLite FTS5 (ADR-0004) sur le titre, la fiche aplatie, la note, les annotations et les tags, combinable avec les filtres.
- **V1.5** : embeddings calculés **localement sur le NAS** par un petit modèle multilingue (100 à 300 Mo, sur CPU), à partir de la **fiche en français** plutôt que de l'original (plus court, une seule langue). Une recherche hybride combinera FTS5 et similarité ; les mêmes vecteurs serviront aux éléments liés et au regroupement de la veille (V2).
- **Plus tard** : questions posées à la base (RAG), via Haiku ou un modèle local si des modèles plus légers et performants apparaissent.

## Conséquences

- La V1 reste simple et sans dépendance supplémentaire.
- La V1.5 ajoutera un composant d'inférence local ; son empreinte mémoire devra rester compatible avec le NAS.

## Alternatives écartées

- **Embeddings via une API tierce** : coût et dépendance supplémentaires pour un calcul faisable localement.
- **Recherche sémantique dès la V1** : retarde la V1 alors que la bibliothèque sera d'abord petite.
