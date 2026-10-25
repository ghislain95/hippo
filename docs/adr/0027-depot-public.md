# ADR-0027 — Dépôt et image publics

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Amende** : [ADR-0002](0002-monorepo.md) et [ADR-0019](0019-deploiement-docker-nas.md), sur la visibilité seulement

## Contexte

ADR-0002 prévoyait un dépôt GitHub privé et ADR-0019 une image privée sur GHCR. Le dépôt `ghislain95/hippo` a été créé public, et Ghislain a confirmé ce choix lors du setup. En offre gratuite, un dépôt public dispose de la protection de branche côté serveur, ce qui n'est pas le cas d'un dépôt privé. Le prompt système contient le profil de Ghislain, et le jeu de test contiendra des contenus qu'il choisit.

## Décision

- Le dépôt reste **public**.
- L'image `ghcr.io/ghislain95/hippo` est **publique** elle aussi : elle ne contient aucun secret, et le NAS la tire sans identifiant.
- **Aucun contenu personnel ni secret dans le dépôt** : données de test fictives, contenus du jeu de test validés par Ghislain avant commit, secrets uniquement dans `.env` (non versionné) et dans les secrets GitHub.
- `main` est protégée côté serveur (PR obligatoire, check `ci-ok`, pas de force-push), en plus du hook des agents ([ADR-0029](0029-workflow-git-autonome.md)).

## Conséquences

- Gitleaks tourne au pré-commit et en CI : un secret commité serait public immédiatement.
- Le profil de Ghislain dans le prompt est visible ; il le juge acceptable.
- Le reste d'ADR-0002 (monorepo) et d'ADR-0019 (Docker Compose, distroless, GHCR, mise à jour manuelle) est inchangé.

## Alternatives écartées

- **Dépôt privé** (ADR-0002) : pas de protection de branche côté serveur en offre gratuite ; le hook resterait le seul verrou.
- **Image privée avec dépôt public** : un token de lecture de plus à gérer sur le NAS, pour protéger un binaire dont le code source est public.
