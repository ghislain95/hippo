# ADR-0015 — Accès réseau par Tailscale, en HTTPS, sans port ouvert

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

L'iPhone doit joindre le NAS depuis partout. Ouvrir un port sur la box et gérer un nom de domaine et des certificats exposerait le service à Internet. iOS refuse par ailleurs le HTTP non chiffré par défaut (App Transport Security).

## Décision

- **Tailscale** sur le NAS (en conteneur sidecar) et sur l'iPhone.
- Le backend partage le réseau du conteneur Tailscale et est publié en **HTTPS** par `tailscale serve`, avec le certificat `*.ts.net` fourni par Tailscale.
- Aucun port n'est ouvert sur la box.

## Conséquences

- Le service n'est jamais exposé publiquement.
- Pas d'exception ATS à configurer dans l'appli.
- L'iPhone doit avoir Tailscale actif pour joindre le NAS ; hors Tailscale, l'appli fonctionne en lecture hors ligne (ADR-0018).
- Coexistence avec SideStore à gérer lors du rafraîchissement hebdomadaire (ADR-0013).

## Alternatives écartées

- **Port ouvert + reverse proxy + Let's Encrypt** : surface d'attaque, maintenance, nom de domaine à acheter.
- **Cloudflare Tunnel** : pas de port ouvert non plus, mais le trafic passe par un tiers et le service devient joignable depuis Internet.
