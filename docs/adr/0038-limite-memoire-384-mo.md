# ADR-0038 — Limite mémoire du conteneur à 384 Mo

- **Statut** : Accepté
- **Date** : 2026-11-06
- **Amende** : [ADR-0019](0019-deploiement-docker-nas.md), sur la limite mémoire seulement

## Contexte

L'[ADR-0019](0019-deploiement-docker-nas.md) fixe la limite mémoire du conteneur à 128 Mo. Le batch de nuit (E3-01) envoie à l'IA une copie réduite de chaque image, sans métadonnées. Pour la produire, il décode l'image dans un processus enfant (`hippo copy-image`), une image à la fois.

Pics mesurés le 6 novembre 2026, sur darwin/arm64 (design du change `submit-nightly-batch`, D10) :

| Image | Pic résident |
| --- | --- |
| JPEG 12 Mpx | 44 Mo |
| Capture d'écran | 40 Mo |
| JPEG 48 Mpx | 91 Mo |
| HEIC 48 Mpx | environ 120 Mo |
| PNG 48 Mpx | 209 Mo |

L'enfant est borné à 256 Mio (limite souple 200 Mio). Au-delà de 50 Mpx, l'image est refusée sans être décodée ; ce plafond admet les photos 48 Mpx des iPhone récents.

Dans le même conteneur tournent le service lui-même (environ 60 à 80 Mo pendant une passe) et l'enfant d'extraction des pages et PDF (48 Mio). Le corps de la requête n'est jamais en mémoire : il est écrit dans un fichier temporaire, puis envoyé en flux.

## Décision

- Limite mémoire du conteneur à **384 Mo** (`mem_limit: 384m`), dans le compose du NAS et dans le compose local.
- Ce chiffre est la somme du budget mémoire du design (D15) : service, enfant d'extraction, enfant de copie d'image.
- Le reste de l'ADR-0019 tient.

## Conséquences

- Sur un NAS de 8 Go, l'écart est négligeable.
- Si la mémoire manque malgré tout, le noyau tue le plus gros processus, l'enfant de copie : l'élément part sans image, le service continue.
- Une mesure dans le conteneur est prévue (tâche 6.6 du change) : un HEIC 48 Mpx pendant l'extraction d'un PDF d'environ 50 Mo. Si la limite est dépassée, le plafond de pixels baisse.

## Alternatives écartées

- **256 Mo avec un plafond à 24 Mpx** : la plupart des photos 48 Mpx d'iPhone partiraient sans image.
- **Un conteneur séparé pour la copie des images** : plus de pièces à déployer et à surveiller, pour un seul usage.
- **Garder 128 Mo et envoyer les HEIC sans image** : Ghislain veut qu'un HEIC soit traité comme les autres images.
