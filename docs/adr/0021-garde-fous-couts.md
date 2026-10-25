# ADR-0021 — Garde-fous de coût de l'IA

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

L'API est facturée au token. Un bug (boucle de réanalyse, import massif) ou un contenu inattendu (pages énormes) ne doit jamais produire une mauvaise surprise sur la facture.

## Décision

Trois verrous indépendants :

1. **Crédits prépayés** sur la console Anthropic, **recharge automatique désactivée**. Crédit épuisé, les appels s'arrêtent.
2. **Budget mensuel applicatif** (`MONTHLY_BUDGET_USD`, 5 $ par défaut), calculé à partir des tokens réellement consommés enregistrés pour chaque batch et chaque analyse immédiate. Budget atteint : plus aucune analyse jusqu'au mois suivant (RG-81), alerte dans Réglages.
3. **Plafonds techniques** : 200 éléments par batch, ~120 000 caractères d'entrée par élément, 4 000 tokens de sortie (8 000 pour la relance unique d'une fiche incomplète), images redimensionnées avant envoi.

Une **clé d'API dédiée** au projet permet de suivre sa consommation et de la révoquer indépendamment.

## Conséquences

- Coût attendu : environ 2 à 4 $ par mois pour une vingtaine d'éléments par jour.
- Le calcul du coût dépend des tarifs du modèle, à garder en configuration et à mettre à jour si les tarifs changent.
- Le budget est vérifié avant chaque envoi, pas après.

## Alternatives écartées

- **Se fier uniquement à la console Anthropic** : alerte tardive, pas de visibilité dans l'appli.
- **Recharge automatique** : supprime la limite dure.
