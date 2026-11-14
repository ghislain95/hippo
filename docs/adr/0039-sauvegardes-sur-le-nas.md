# ADR-0039 — Sauvegardes sur le NAS, sans copie externe

- **Statut** : Accepté
- **Date** : 2026-11-12
- **Remplace en partie** : [ADR-0020](0020-sauvegardes.md), sur la copie externe chiffrée seulement

## Contexte

L'[ADR-0020](0020-sauvegardes.md) (11 octobre 2026) prévoit une sauvegarde locale chaque nuit et une copie chiffrée chaque nuit vers un stockage externe, dont le service restait un point ouvert.

Hippo tourne sur le NAS depuis le change `deploy-nas` (12 novembre 2026), sans aucune sauvegarde. Le RAID 1 protège d'une panne de disque, pas d'une migration ou d'un bug qui abîme la base, ni d'une erreur de manipulation. La mise à jour automatique prévue ensuite (change `nightly-update`) n'est sûre qu'avec une sauvegarde prise juste avant.

Le 12 novembre 2026, Ghislain a écarté tout service externe : il ne veut ni service cloud ni son coût.

## Décision

- **Les sauvegardes restent sur le NAS.** Aucune copie hors du NAS ([RG-92](../metier.md#données-et-multi-utilisateur), [RG-98](../metier.md#données-et-multi-utilisateur)).
- **Emplacement** : le dossier partagé `Sauvegardes`, sous-dossier `hippo`, à part du dossier du projet `docker/hippo`. Supprimer ou réinstaller le projet laisse les sauvegardes intactes.
- **Moments** : chaque nuit à `BACKUP_SCHEDULE` (3 h, heure de Paris, par défaut) et avant chaque mise à jour.
- **Un seul chemin de code** : le service le lance la nuit, la commande `hippo backup` à la demande, dans le conteneur en marche (`docker compose exec`).
- **Contenu** : une copie cohérente de la base par `VACUUM INTO`, sur une connexion en lecture seule, puis les images ; un petit manifeste. La sauvegarde n'écrit jamais dans la base.
- **Images** : une image inchangée depuis la sauvegarde précédente y est liée en dur (*hard link*), sinon copiée. Jamais de lien vers les images des données vivantes.
- **Conservation** : 14 jours ; les 3 sauvegardes les plus récentes sont toujours gardées.
- **Restauration** : procédure manuelle de [`docs/runbook-nas.md`](../runbook-nas.md), testée une fois avant la mise en service, comme le demande l'ADR-0020.
- Le reste de l'ADR-0020 tient : sauvegarde locale chaque nuit à 3 h par `VACUUM INTO`, images comprises, 14 jours.

Le détail (verrou, dossier partiel, contrôle de l'espace libre, journal) est dans le design du change `nightly-backup`.

## Conséquences

- **Risques acceptés** par Ghislain le 12 novembre 2026 :
  - un vol, un incendie, une surtension ou un rançongiciel qui atteint le NAS emporte Hippo et ses sauvegardes ensemble ;
  - une image n'existe qu'en un seul exemplaire sur le disque pour toutes les sauvegardes qui la contiennent (liens en dur) : un dommage à ce fichier les atteint toutes.
- Le RAID 1 et les sommes de contrôle de Btrfs couvrent une panne de disque, pas ces risques.
- La restauration se fait à la main ; Hippo n'a ni route ni commande de restauration.
- Aucun coût, aucune clé de chiffrement à garder hors du NAS.
- Les sauvegardes occupent le même volume que les données : l'espace libre est vérifié avant d'écrire, et une image n'est stockée qu'une fois.
- Le dossier `Sauvegardes` doit rester hors de toute synchronisation et de toute sauvegarde cloud d'UGOS ; sinon RG-92 est rompue hors de portée de Hippo. Le runbook le vérifie.
- Un élément privé est sauvegardé comme les autres, sur le NAS seulement (RG-98).

## Alternatives écartées

- **Copie chiffrée vers un stockage compatible S3, ou restic vers un service existant** (la copie externe de l'ADR-0020) : un coût et un service que Ghislain ne veut pas.
- **Instantanés Btrfs d'UGOS** : hors de Hippo et de ses tests ; reste un complément possible, géré par Ghislain.
- **Copie complète des images chaque nuit** : 14 copies de chaque image pour une protection à peine meilleure, puisque toutes restent sur le même NAS.
