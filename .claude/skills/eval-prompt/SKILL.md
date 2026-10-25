---
name: eval-prompt
description: Run the prompt eval set (docs/prompt/eval-set.md) against the analysis prompt and schemas, produce the report and compare it with the previous one. Use before shipping any change to the system prompt, a fiche schema or the classify_item envelope, and before going live. Costs real money (direct Anthropic API calls): always ask Ghislain first.
argument-hint: "[case ids to run, default: all]"
---

# /eval-prompt — jeu de test du prompt

Le jeu de test est le test le plus important du projet (`docs/prompt/eval-set.md`) : il vérifie que les fiches ne perdent rien, que le classement est juste et que le coût reste maîtrisé.

## 0. Préconditions (s'arrêter si l'une manque, en le disant)

- Le harnais existe : `backend/cmd/evalprompt/` et des cas dans `backend/testdata/eval/<nn-nom>/` (story E3-09). Tant qu'il n'existe pas, répondre « Le harnais du jeu de test n'existe pas encore (E3-09) » et s'arrêter.
- `ANTHROPIC_API_KEY` est disponible dans l'environnement du harnais **sans que l'agent lise `.env`** : le harnais se lance par `make eval-prompt`, qui charge `infra/.env` lui-même.
- La version du prompt (`docs/prompt/system-prompt.md`) et des schémas modifiés est incrémentée, et `make check-schemas` passe.

## 1. Demander l'accord (obligatoire, à chaque passage)

Les appels sont directs, au plein tarif. Avant de lancer, écrire à Ghislain :
- les cas qui vont tourner (tous, ou `$ARGUMENTS`) ;
- l'estimation du coût : somme, par cas, des tokens d'entrée (contenu figé + partie fixe + référentiel) et du plafond de sortie (`max_output_tokens` de `expected.yaml`, sinon 4 000), aux tarifs de `docs/spec-v1.md` (« Coûts et garde-fous ») ; ordre de grandeur attendu : quelques dizaines de centimes ;
- la raison du passage (changement testé).
Attendre un « oui » explicite. Sans accord : s'arrêter.

## 2. Lancer

```bash
make eval-prompt CASES="<ids ou vide>"
```
Le harnais exécute chaque cas en appel direct (pas en batch), applique les contrôles automatiques et écrit le rapport `backend/testdata/eval/reports/<AAAA-MM-JJ>-prompt-v<n>.md`.

## 3. Lire le rapport

- **Bloquant** : un échec sur les points 1 (schéma), 4 (`must_contain` / `must_not_contain`) ou 5 (`code_verbatim`) de la notation. Le changement ne part pas.
- Non bloquant : type, catégorie, `content_ok`, `freshness` ; à expliquer.
- Comparer au rapport précédent : régressions et améliorations par cas, tokens et coût total.

## 4. Rendre compte

En trois blocs : verdict (bloquant ou non), tableau des cas en échec avec la cause, coût réel du passage. Proposer à Ghislain la relecture humaine (fidélité, clarté, classement, notées de 1 à 5) pour les cas changés ; ses notes vont dans le rapport.

Ne jamais : relancer en boucle pour « faire passer » un cas, modifier un `expected.yaml` pour l'aligner sur une réponse, ni committer un contenu de cas sans l'accord de Ghislain (dépôt public).
