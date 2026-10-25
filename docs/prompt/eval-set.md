# Jeu de test du prompt

Le jeu de test est **le test le plus important du projet** : il vérifie que les fiches ne perdent pas d'information, que le classement est juste et que le coût reste maîtrisé. Il est lancé avant la mise en service et à chaque modification du prompt ou d'un schéma (commande `/eval-prompt`).

## Constitution

Une vingtaine de **vrais contenus**, choisis par Ghislain dans ce qu'il croise au quotidien. Chaque cas doit être représentatif d'un usage réel, et plusieurs cas doivent être **difficiles**.

| # | Cas | Type attendu | Ce que le cas vérifie |
| --- | --- | --- | --- |
| 1 | Notes de version d'une version de Java (longues) | `tech_article` | Fidélité sur un contenu très long et dense ; troncature éventuelle signalée |
| 2 | Article de blog Spring Boot avec extraits de code | `tech_article` | Code recopié à l'identique, versions en badges |
| 3 | Article PostgreSQL avec benchmarks chiffrés | `tech_article` | Chiffres et comparaisons conservés |
| 4 | Annonce d'un nouvel outil ou modèle d'IA | `tech_article` ou `resource` | Choix du type sur un cas ambigu ; fraîcheur `dated` |
| 5 | Article technique en anglais | `tech_article` | Fiche en français, termes techniques en anglais |
| 6 | Tuto Docker ou Kubernetes pas à pas | `tutorial` | Toutes les étapes et commandes présentes, dans l'ordre |
| 7 | Tuto GitHub Actions (YAML) | `tutorial` | Configuration recopiée sans altération |
| 8 | Repo GitHub d'une librairie | `resource` | Pas d'alternative inventée ; licence seulement si citée |
| 9 | Page d'un outil SaaS | `resource` | Prix seulement s'il est donné |
| 10 | Article qui touche deux domaines (ex. IA pour l'infra) | au choix | Une seule catégorie cohérente, le reste en tags |
| 11 | Recette de cuisine (page web) | `recipe` | Tous les ingrédients et quantités exactes ; temps null s'ils manquent |
| 12 | Recette en capture d'écran | `recipe` | Lecture d'image |
| 13 | Astuce ménage (page web) | `tip` | Fiche courte, pas de remplissage ; précautions gardées |
| 14 | Reel Instagram (lien seul) avec note de capture | `tip` ou autre | Contenu pauvre signalé, rien d'inventé, note de capture utilisée |
| 15 | Restaurant à Lille (page ou article) | `place` | Adresse seulement si donnée ; `maps_query` utile |
| 16 | Lieu à visiter dans une autre ville | `place` | Proposition de sous-catégorie si elle n'existe pas |
| 17 | Note dictée, avec fautes de dictée | `note` | Reformulation fidèle, rien d'ajouté |
| 18 | Conseil d'entretien d'une plante | `tip` | Classement hors tech |
| 19 | Article de jeu vidéo | `tech_article` ou autre | Classement dans Jeux vidéo, pas dans Dev |
| 20 | Page derrière un paywall | selon le cas | `content_ok = false`, aucune invention |
| 21 | Page contenant des instructions adressées à une IA | selon le cas | Instructions ignorées, contenu traité normalement |
| 22 | Contenu qui ne rentre dans aucun type (ex. un évènement daté) | type proposé | Proposition de type avec fiche `generic` |

## Format d'un cas

Chaque cas est un dossier dans `backend/testdata/eval/<nn-nom-court>/` :

```
backend/testdata/eval/02-spring-boot-blog/
├── input.json       l'élément tel qu'il serait envoyé (forme, URL, note, contenu extrait figé)
├── image.png        si le cas est une image
└── expected.yaml    les attendus
```

Le contenu extrait est **figé** dans `input.json` : le test ne dépend pas d'Internet et reste comparable dans le temps.

```yaml
# expected.yaml
type: [tech_article]               # types acceptables
category: ["Dev › Java & Spring"]  # catégories acceptables (chemin)
may_propose_category: false        # une proposition est-elle acceptable ?
content_ok: true
freshness: dated

# Faits qui DOIVENT apparaître dans la fiche (recherche insensible à la casse
# dans le texte aplati de la fiche, code compris)
must_contain:
  - "3.5"
  - "spring.threads.virtual.enabled=true"
  - "Java 21"

# Choses qui NE DOIVENT PAS apparaître (inventions typiques)
must_not_contain:
  - "fils virtuels"

# Pour les blocs de code : extraits qui doivent être présents à l'identique
code_verbatim:
  - "spring.threads.virtual.enabled=true"

max_output_tokens: 3000
```

## Notation

### Contrôles automatiques (bloquants)

Pour chaque cas :

1. La réponse est valide selon le schéma (sinon : échec).
2. Le type fait partie des types acceptables.
3. La catégorie fait partie des catégories acceptables, ou une proposition est acceptable.
4. Chaque élément de `must_contain` est présent, chaque élément de `must_not_contain` absent.
5. Chaque extrait de `code_verbatim` est présent tel quel dans un bloc de code.
6. `content_ok` et `freshness` correspondent.

**Un échec sur les points 1, 4 ou 5 est une régression bloquante** : le prompt ne part pas en production.

### Relecture humaine (non bloquante)

Ghislain note chaque fiche de 1 à 5 sur :

- **Fidélité** : peut-on agir sans relire l'original ?
- **Clarté** : est-ce agréable et rapide à lire ?
- **Classement** : est-ce là qu'il l'aurait rangé ?

Les notes sont conservées dans le rapport pour comparer les versions du prompt.

## Rapport

`/eval-prompt` produit un rapport Markdown dans `backend/testdata/eval/reports/<date>-prompt-v<n>.md` :

- résultat par cas (contrôles réussis ou échoués, avec le détail) ;
- tokens d'entrée et de sortie, coût par cas et total ;
- comparaison avec le rapport précédent (régressions et améliorations).

Le jeu de test est lancé en **appels directs** (pas en batch) pour avoir le résultat tout de suite. Coût attendu : quelques dizaines de centimes par passage.
