# Décisions d'architecture (ADR)

Chaque décision structurante du projet, avec son contexte et les alternatives écartées. Format et règles : [ADR-0001](0001-enregistrer-les-decisions.md).

**Pour les agents** : avant de modifier une partie du système, lire les ADR qui la concernent. Ne jamais contredire un ADR accepté sans proposer un nouvel ADR qui le remplace.

| N° | Décision | Statut | Concerne |
| --- | --- | --- | --- |
| [0001](0001-enregistrer-les-decisions.md) | Enregistrer les décisions d'architecture | Accepté | Projet |
| [0002](0002-monorepo.md) | Un seul dépôt (monorepo) | Accepté, visibilité amendée par 0027 | Projet |
| [0003](0003-backend-go.md) | Backend en Go, sans framework web | Accepté | Backend |
| [0004](0004-sqlite.md) | SQLite comme base de données | Accepté | Backend |
| [0005](0005-haiku-batch.md) | Analyse par Claude Haiku, en batch de nuit | Accepté | IA |
| [0006](0006-sortie-structuree.md) | Sortie structurée forcée et validée | Accepté | IA |
| [0007](0007-fiche-json-source-de-verite.md) | La fiche JSON est la source de vérité, le Markdown est généré | Accepté | Données |
| [0008](0008-extraction-et-reanalyse.md) | Extraction locale et conservation du contenu original | Accepté | Backend, IA |
| [0009](0009-ia-propose-utilisateur-dispose.md) | Classement : l'IA propose, l'utilisateur dispose | Accepté | Métier, IA |
| [0010](0010-categorie-unique-et-tags.md) | Une seule catégorie par élément, deux niveaux, des tags | Accepté | Métier |
| [0011](0011-types-de-contenu-et-templates.md) | Types de contenu avec une fiche et un template dédiés | Accepté | Métier, appli |
| [0012](0012-appli-expo.md) | Appli mobile en Expo (React Native) | Accepté | Appli |
| [0013](0013-distribution-ios-gratuite.md) | Distribution iOS avec un compte Apple gratuit et SideStore | Accepté, confirmé par E1 | Appli |
| [0014](0014-extension-de-partage-autonome.md) | Extension de partage autonome | Accepté, confirmé par E1, en Swift (0036) | Appli |
| [0015](0015-reseau-tailscale.md) | Accès réseau par Tailscale, en HTTPS, sans port ouvert | Accepté | Infra |
| [0016](0016-authentification-token.md) | Authentification par token d'API statique | Accepté | Backend |
| [0017](0017-contrat-openapi.md) | Contrat d'API OpenAPI comme source de vérité | Accepté, précisé par 0031 et 0035 | Backend, appli |
| [0018](0018-lecture-hors-ligne.md) | Lecture hors ligne par cache local synchronisé | Accepté, synchronisation précisée par 0035 | Appli, backend |
| [0019](0019-deploiement-docker-nas.md) | Déploiement Docker Compose sur le NAS | Accepté, visibilité de l'image amendée par 0027 | Infra |
| [0020](0020-sauvegardes.md) | Sauvegardes locale et externe chiffrée | Accepté, service à choisir | Infra |
| [0021](0021-garde-fous-couts.md) | Garde-fous de coût de l'IA | Accepté | IA, backend |
| [0022](0022-preparation-multi-utilisateur.md) | Préparer le multi-utilisateur dès le modèle de données | Accepté | Données |
| [0023](0023-option-prive.md) | Option « Privé » : jamais envoyé à l'IA | Accepté | Métier, IA |
| [0024](0024-recherche.md) | Recherche plein texte en V1, sémantique en V1.5 | Accepté | Backend |
| [0025](0025-reels-instagram.md) | Reels Instagram : lien en V1, extraction en V1.5 | Accepté (V1.5) | Backend |
| [0026](0026-langues-doc-et-code.md) | Documentation en français, code en anglais | Accepté | Projet |
| [0027](0027-depot-public.md) | Dépôt et image publics | Accepté | Projet, infra |
| [0028](0028-langues-du-developpement.md) | Langues des artefacts de développement | Accepté | Projet |
| [0029](0029-workflow-git-autonome.md) | Workflow git autonome des agents | Accepté, moment de la fusion amendé par 0034 | Projet |
| [0030](0030-outillage-qualite.md) | Outillage de qualité | Accepté | Projet |
| [0031](0031-generation-depuis-les-contrats.md) | Génération de code depuis les contrats | Accepté, précisé par 0035 | Backend, appli |
| [0032](0032-migrations-goose.md) | Migrations SQLite avec goose | Accepté | Backend |
| [0033](0033-strategie-de-test.md) | Stratégie de test : Go standard et jest-expo | Accepté | Projet |
| [0034](0034-auto-merge-github.md) | Fusion différée par l'auto-merge de GitHub | Accepté | Projet |
| [0035](0035-contrat-identifiants-et-synchronisation.md) | Contrat d'API : fiches générées, identifiants et synchronisation | Accepté | Backend, appli |
| [0036](0036-extension-de-partage-swift.md) | Extension de partage native en Swift | Accepté | Appli |
