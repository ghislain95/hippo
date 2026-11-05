# ADR-0037 — Vidéos YouTube sans transcription en V1

- **Statut** : Accepté
- **Date** : 2026-11-05

## Contexte

L'[ADR-0008](0008-extraction-et-reanalyse.md) prévoit, pour une vidéo YouTube, les métadonnées et la transcription quand elle existe. YouTube n'offre aucune interface officielle pour lire les sous-titres d'une vidéo dont on n'est pas propriétaire. Tous les outils passent par l'interface interne du lecteur, non documentée. YouTube y ajoute régulièrement des protections (jeton de preuve d'origine, détection de robots), et ces outils cassent sans préavis. Ghislain capture rarement des vidéos (5 novembre 2026).

Un test du 5 novembre 2026, depuis un réseau résidentiel en France, montre que la page publique d'une vidéo contient, sans bandeau de consentement, un bloc JSON (`ytInitialPlayerResponse`) avec le titre, la chaîne, la durée et la description complète. L'interface oEmbed de YouTube, publique et stable, donne le titre et la chaîne.

## Décision

- En V1, une vidéo YouTube est extraite **sans transcription** : titre, chaîne, durée et description (RG-22).
- Les métadonnées sont lues dans la **page de la vidéo**, par le processus enfant de l'extraction (mémoire et temps bornés, comme une page web). Si la page ne les donne pas, **oEmbed** fournit au moins le titre et la chaîne.
- La description tient lieu de contenu, en repli (RG-23) ; la durée est le temps de lecture (RG-25).
- La transcription est reportée en V1.5, avec l'extraction des reels Instagram, qui passera par yt-dlp ([ADR-0025](0025-reels-instagram.md)).
- Amende l'ADR-0008 sur ce seul point ; le reste de l'ADR-0008 tient.

## Conséquences

- Aucune dépendance, aucune clé d'API, aucun secret.
- L'IA analyse une vidéo sur son titre, sa chaîne, sa description et la note de capture : sa fiche est plus pauvre que celle d'une page web lue en entier.
- Si YouTube change le format de sa page, on perd la description et la durée, jamais le titre (oEmbed) ; les logs le montrent par la classe de raison.
- Les vidéos capturées avant la V1.5 n'ont pas de transcription ; il faudra les extraire de nouveau pour l'obtenir.

## Alternatives écartées

- **Transcription par l'interface interne du lecteur, en code maison** : fragile, demande une maintenance au gré des changements de YouTube, pour un usage rare.
- **yt-dlp dès la V1** : robuste et suivi, mais il ajoute Python à l'image (50 à 80 Mo), une dépendance qui change chaque semaine et un processus lourd dans un conteneur de 128 Mo, pour un usage rare.
- **API YouTube Data v3** : officielle et stable pour les métadonnées, mais elle demande un projet Google Cloud, une clé et un quota, et elle ne donne pas la transcription d'une vidéo dont on n'est pas propriétaire.
