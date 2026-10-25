# ADR-0014 — Extension de partage autonome

- **Statut** : Accepté, à confirmer par le prototype (epic E1)
- **Date** : 2026-10-10

## Contexte

L'extension de partage iOS est la porte d'entrée principale de l'appli. Habituellement, une extension partage ses réglages avec l'appli principale via les **App Groups**, une capacité dont le fonctionnement avec un compte Apple gratuit est incertain.

## Décision

- L'extension **envoie elle-même** la capture au NAS (`POST /api/items`). L'appli principale n'a pas besoin d'être ouverte.
- L'URL du serveur et le token d'API sont **injectés au build** dans l'appli et dans l'extension. Aucun partage de données entre les deux.
- L'extension accepte les URL, le texte et les images, affiche un mini formulaire (note, interrupteur Privé, Enregistrer) et un message clair si le serveur est injoignable.

## Conséquences

- Fonctionne même sans App Groups.
- Changer d'URL de serveur ou de token demande un nouveau build, ce qui est acceptable pour un usage personnel.
- Pas de file d'attente hors ligne dans l'extension (RG-06) : sans réseau, elle propose de copier le lien.
- À confirmer en tout premier par le prototype. Repli si le plugin Expo pose problème : une extension native Swift minimale.

## Alternatives écartées

- **Extension qui délègue à l'appli via App Groups** : dépend d'une capacité incertaine avec un compte gratuit.
- **Ouvrir l'appli principale depuis l'extension** : friction supplémentaire, contraire à l'objectif de capture en moins de 5 secondes.
