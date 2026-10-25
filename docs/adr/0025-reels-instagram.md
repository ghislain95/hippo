# ADR-0025 — Reels Instagram : lien en V1, extraction en V1.5

- **Statut** : Accepté (mise en œuvre en V1.5)
- **Date** : 2026-10-11

## Contexte

Beaucoup d'astuces arrivent par des reels Instagram. Instagram bloque les téléchargements anonymes : récupérer la vidéo demande les cookies d'un compte connecté, ce qui est contraire à ses conditions d'utilisation et peut entraîner le bannissement du compte.

## Décision

- **V1** : un reel partagé est enregistré comme lien, avec la note de capture et, si l'utilisateur en ajoute une, une capture d'écran. L'IA analyse ce qu'elle a ; la fiche est souvent marquée « contenu partiel ».
- **V1.5** : un module expérimental côté NAS récupère le reel avec **yt-dlp** et les cookies d'un **compte Instagram secondaire dédié**, jamais le compte principal. Il transcrit l'audio la nuit (Whisper sur CPU) et extrait quelques images de la vidéo pour que l'IA lise le texte affiché.
- Le partage se fait toujours depuis le compte principal, vers Hippo ; le compte secondaire n'est utilisé que par le serveur.
- En cas d'échec (cookies expirés, compte banni, yt-dlp cassé), **repli automatique** sur le comportement V1, sans rien perdre.

## Conséquences

- Risque accepté : le compte secondaire peut être banni ; les cookies sont à renouveler de temps en temps.
- Instagram change régulièrement ses protections : le module peut casser jusqu'à la mise à jour de yt-dlp.
- Rester attentif aux outils qui simplifieraient l'extraction d'ici la V1.5.

## Alternatives écartées

- **Utiliser le compte principal** : risque de bannissement du compte personnel.
- **Abandonner l'extraction des reels** : perd une source importante d'astuces.
