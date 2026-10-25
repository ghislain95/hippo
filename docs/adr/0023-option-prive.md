# ADR-0023 — Option « Privé » : jamais envoyé à l'IA

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Ghislain accepte que ses contenus soient envoyés à Anthropic pour analyse. Mais certaines captures (une note personnelle, une capture d'écran avec des informations privées) ne devraient pas quitter le NAS.

## Décision

- Un interrupteur **Privé**, **décoché par défaut**, à la capture (appli et extension) et sur chaque élément.
- Un élément privé n'est **jamais** envoyé à l'IA (RG-12). Le filtre est appliqué au plus près de l'envoi (construction du batch et analyse immédiate), pas seulement dans l'interface.
- Un élément privé n'a pas de fiche générée : il est rangé à la main et reste cherchable par son titre, sa note et ses tags.

## Conséquences

- Un test automatisé garantit qu'aucun élément privé ne peut figurer dans une requête vers l'IA, quel que soit le chemin.
- Les éléments privés ne profitent pas du classement automatique.

## Alternatives écartées

- **Tout envoyer** : pas de solution pour les contenus sensibles.
- **Privé coché par défaut** : contraire à l'objectif de classement automatique.
