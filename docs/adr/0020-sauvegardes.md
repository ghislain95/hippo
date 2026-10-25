# ADR-0020 — Sauvegardes locale et externe chiffrée

- **Statut** : Accepté (service externe à choisir)
- **Date** : 2026-10-11

## Contexte

Les disques du NAS sont en miroir (RAID 1) : ça protège d'une panne de disque, pas d'un vol, d'un incendie, d'une erreur de manipulation ou d'un bug qui corrompt la base.

## Décision

- **Chaque nuit à 3 h** (après le batch), copie cohérente de la base par `VACUUM INTO` vers un autre dossier du NAS, avec les images ; **14 jours** conservés.
- **Chaque nuit également**, une copie **chiffrée** vers un stockage externe. Le chiffrement se fait sur le NAS avant l'envoi ; la clé n'est jamais stockée chez le fournisseur.
- Le service externe est un **point ouvert** (stockage objet compatible S3 bon marché, ou un outil comme restic ou rclone vers un service existant).
- Une restauration est testée au moins une fois avant la mise en service.

## Conséquences

- Volume faible (quelques dizaines à quelques centaines de Mo), donc coût quasi nul.
- La clé de chiffrement doit être gardée en lieu sûr hors du NAS (gestionnaire de mots de passe), sinon la sauvegarde externe est inutilisable.

## Alternatives écartées

- **Se fier au RAID** : ce n'est pas une sauvegarde.
- **Copie externe en clair** : les contenus personnels n'ont pas à être lisibles par un fournisseur.
