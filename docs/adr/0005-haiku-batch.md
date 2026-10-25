# ADR-0005 — Analyse par Claude Haiku, en batch de nuit

- **Statut** : Accepté
- **Date** : 2026-10-10

## Contexte

L'analyse (classement et rédaction des fiches) demande un vrai modèle de langage, avec une bonne qualité en français et la lecture d'images. Le NAS a 8 Go de RAM, pas de GPU, et ne doit pas faire tourner un LLM en permanence. Le coût doit rester faible et prévisible. Le besoin n'est pas temps réel : un résultat le lendemain matin suffit.

## Décision

- Modèle : **Claude Haiku 4.5**, via l'API d'Anthropic.
- Mode : **API Message Batches**, un batch par nuit (2 h par défaut), ce qui divise le prix par deux.
- Une **analyse immédiate** reste possible à la demande, en appel direct au plein tarif.
- Le code métier dépend d'une interface `Analyzer` et pas directement du SDK, pour pouvoir changer de modèle ou de fournisseur (y compris un modèle local plus tard) sans toucher au reste.
- Le nom du modèle est une variable de configuration.

## Conséquences

- Coût estimé entre 2 et 4 $ par mois pour une vingtaine d'éléments par jour (voir ADR-0021 pour les garde-fous).
- Aucune charge IA sur le NAS.
- Un batch peut mettre jusqu'à 24 h ; en pratique souvent moins d'une heure. Le backend le suit par interrogations régulières.
- Les contenus non privés sont envoyés à Anthropic (voir ADR-0023 pour l’option Privé).
- L'abonnement Claude de Ghislain ne couvre pas l'API : une clé dédiée au projet, facturée au token, est nécessaire.

## Alternatives écartées

- **LLM local via Ollama sur le NAS** : un modèle de 3 à 4 milliards de paramètres tient dans la RAM, mais la qualité des fiches en français est insuffisante et la mémoire est disputée avec les autres services. Reste envisageable plus tard grâce à l'interface `Analyzer`.
- **Appels unitaires au fil de l'eau** : deux fois plus cher, sans bénéfice puisque le besoin n'est pas temps réel.
- **Modèles plus gros (Sonnet, Opus)** : meilleurs, mais plusieurs fois plus chers pour une tâche de classement et de synthèse que Haiku réussit bien. À réévaluer si le jeu de test montre des pertes d'information.
