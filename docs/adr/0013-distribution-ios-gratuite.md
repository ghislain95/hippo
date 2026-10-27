# ADR-0013 — Distribution iOS avec un compte Apple gratuit et SideStore

- **Statut** : Accepté, confirmé par le prototype E1 le 2026-10-27 (connexion de SideStore à surveiller : voir le runbook iOS)
- **Date** : 2026-10-10

## Contexte

Le programme Apple Developer coûte environ 99 $ par an. Ghislain ne veut pas le payer pour ce seul projet. Avec un identifiant Apple gratuit (« Personal Team »), une appli installée sur son propre iPhone expire au bout de 7 jours, et certaines capacités sont limitées.

## Décision

- Signature avec un **compte Apple gratuit**.
- Installation et **rafraîchissement hebdomadaire depuis l'iPhone** avec **SideStore** (premier appairage une seule fois depuis le Mac).
- Pas de notifications push : les rappels (V2) passent par des **notifications locales** programmées sur le téléphone.
- L'appli ne dépend d'aucune capacité réservée aux comptes payants ; en particulier, l'extension de partage n'utilise pas les App Groups (ADR-0014).

## Conséquences

- Coût fixe nul.
- Rafraîchissement toutes les semaines (quelques secondes), à faire en coupant Tailscale le temps de l'opération : SideStore et Tailscale sont tous deux des VPN et iOS n'en garde qu'un actif.
- Limite de 3 applis installées de cette façon.
- Les données sont sur le NAS : une appli expirée ne fait rien perdre.
- Si d'autres projets iOS justifient un jour le compte payant, on pourra basculer sans changer le code (nouvel ADR).

## Alternatives écartées

- **Compte payant** : refusé pour l'instant, pour 99 $ par an.
- **Réinstallation depuis Xcode chaque semaine** : possible, mais demande le Mac ; SideStore évite cette dépendance.
