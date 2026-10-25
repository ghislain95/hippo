# ADR-0009 — Classement : l'IA propose, l'utilisateur dispose

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Le classement doit être automatique : Ghislain ne veut pas ranger à la main. Mais une IA qui réorganise librement l'arborescence la rendrait instable et imprévisible, et pourrait défaire ses corrections.

## Décision

- L'IA reçoit l'arborescence (catégories actives **et** proposées, avec descriptions), les types et les tags existants, et **range dans l'existant en priorité**.
- Si rien ne convient, elle **propose** une catégorie ou un type. La proposition est créée avec le statut `proposed` et l'élément y est rangé tout de suite, pour rester trouvable dès le matin.
- L'IA **ne peut jamais** renommer, déplacer, fusionner ou supprimer quoi que ce soit (RG-56). Le backend n'expose d'ailleurs aucune opération de ce genre au pipeline d'analyse.
- L'utilisateur traite les propositions dans la **revue** : accepter, renommer, fusionner. La revue n'est jamais bloquante.
- Les corrections manuelles sont enregistrées (`user_edited`) et jamais écrasées.

## Conséquences

- L'arborescence ne change que par des actions de l'utilisateur ou par ajout.
- Il faut un dédoublonnage des propositions (RG-54) : sans lui, la même catégorie pourrait être proposée plusieurs fois dans un même batch.
- Le risque est une arborescence qui grossit trop ; la revue quotidienne et la fusion en un geste le contiennent. À réévaluer après un mois d'usage.

## Alternatives écartées

- **Liste de catégories fermée** : l'IA forcerait des contenus nouveaux dans des catégories inadaptées.
- **Catégories entièrement libres** : arborescence instable, doublons, aucune maîtrise.
- **Proposition sans rangement immédiat** (élément en attente jusqu'à validation) : l'élément serait introuvable tant que la revue n'est pas faite, contraire au principe « rien de bloquant ».
