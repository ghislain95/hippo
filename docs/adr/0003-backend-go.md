# ADR-0003 — Backend en Go, sans framework web

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

Le backend tourne en permanence sur un NAS de 8 Go de RAM, à côté d'autres services (Immich, Nextcloud, Jellyfin). Il doit être très léger. Le code est écrit par des agents et relu par Ghislain, développeur Java : il doit rester lisible. La technologie doit être reconnue et outillée (SDK Anthropic, extraction HTML, SQLite).

## Décision

Le backend est un **binaire Go unique** qui porte l'API HTTP, l'extraction, le planificateur nocturne et l'accès à la base.

- HTTP avec la librairie standard (`net/http`, routeur de Go 1.22+), sans framework.
- Logs structurés avec `log/slog`.
- SDK officiel `anthropic-sdk-go` pour l'IA.
- Planification interne (pas de cron système), configurable par variable d'environnement.

## Conséquences

- Empreinte mémoire attendue de quelques dizaines de Mo, image Docker d'environ 20 Mo.
- Démarrage instantané, compilation croisée Mac → NAS (amd64) triviale.
- Moins de « magie » qu'avec Spring : plus de code explicite (routage, validation), mais facile à suivre.

## Alternatives écartées

- **Spring Boot** : connu de Ghislain, mais 200 à 300 Mo de RAM au repos pour un service qui fait peu de choses.
- **Spring ou Quarkus en natif GraalVM** : 50 à 80 Mo, mais builds lents et complexes pour un gain inférieur à Go.
- **Rust** : encore plus léger, mais le backend n'est jamais le goulot d'étranglement ; code plus difficile à relire et itérations plus lentes.
- **PocketBase** : rapide à démarrer, mais moins de maîtrise de l'architecture et projet encore en 0.x, donc exposé à des changements cassants.
