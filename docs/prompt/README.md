# Prompt et schémas d'analyse

Ce dossier contient tout ce qui définit l'analyse d'un élément par l'IA : le prompt, le format de sortie et le jeu de test. C'est un **contrat** entre trois parties : le prompt, le backend (validation, stockage) et l'appli (templates d'affichage). Toute modification se fait ici d'abord.

| Fichier | Rôle |
| --- | --- |
| [`system-prompt.md`](system-prompt.md) | Prompt système (partie fixe), format du référentiel et du message élément |
| [`schemas/classify-item.schema.json`](schemas/classify-item.schema.json) | Schéma de l'outil `classify_item` : l'enveloppe de la réponse |
| [`schemas/fiches/`](schemas/fiches/) | Un schéma de fiche par type, plus les définitions communes |
| [`eval-set.md`](eval-set.md) | Jeu de test du prompt : contenus, attendus, notation |

Règles métier associées : RG-30 à RG-64 dans [`metier.md`](../metier.md). Décisions : [ADR-0005](../adr/0005-haiku-batch.md), [ADR-0006](../adr/0006-sortie-structuree.md), [ADR-0008](../adr/0008-extraction-et-reanalyse.md), [ADR-0009](../adr/0009-ia-propose-utilisateur-dispose.md).

## Structure d'une requête

```
system:
  [1] partie fixe du prompt                        ← cache
  [2] <referentiel> (types, catégories, tags)      ← cache (identique dans un batch)
tools: [classify_item]                             ← cache
tool_choice: { type: "tool", name: "classify_item" }
max_tokens: 4000   (8000 pour la seconde tentative d'une réponse coupée)
messages:
  user: <element>…</element> + <contenu>…</contenu> ou image(s)
```

- **Batch de nuit** : une requête par élément dans un seul appel à l'API Message Batches, avec `custom_id` = id de l'élément.
- **Analyse immédiate** : la même requête, en appel direct.
- **Cache de prompt** : la partie fixe, le référentiel et l'outil sont identiques d'une requête à l'autre dans un batch ; on pose un point de cache (`cache_control`) après le référentiel. Le gain dans un batch n'est pas garanti mais ne coûte rien.
- **Fiche incomplète** : détectée par `stop_reason = "max_tokens"`. La réponse coupée n'est pas enregistrée ; l'élément est retenté une fois avec un plafond de 8 000, puis passe en échec « fiche trop longue » (RG-36).

## Assemblage du schéma de l'outil

Les fichiers de schéma utilisent des `$ref` entre fichiers pour rester lisibles. Le backend les **assemble au build** en un seul schéma autonome (sans `$ref` externe), qui sert à la validation des réponses et, allégé, d'`input_schema` à l'outil. La CI vérifie que l'assemblage est à jour.

- Outil : `backend/cmd/schemabundle` ([ADR-0031](../adr/0031-generation-depuis-les-contrats.md)). Chaque fichier référencé devient une définition de `$defs` au nom stable : `fiche_<type>` pour une fiche, `common_<nom>` pour une définition de `_common.schema.json`.
- Sortie : `backend/internal/analysis/schema/classify-item.bundled.json`, embarqué dans le binaire. Fichier généré : on ne l'édite jamais, on lance `make generate`.
- **Exemples** : [`schemas/examples/`](schemas/examples/) contient une réponse `classify_item` complète et fictive par type de fiche. L'outil les valide contre le schéma assemblé et échoue s'il manque l'exemple d'un type. Ils servent aussi de fixtures aux tests de rendu des templates et de l'export Markdown.
- **`input_schema` de l'outil** : le schéma assemblé sans son mot-clé `$schema` ni aucun mot-clé `x-*` (`x-schema-version` à la racine et dans chaque fiche de `$defs`), retirés à tous les niveaux par `prompt.ToolSchema` : l'API ne promet qu'« un objet JSON Schema ». Seuls des mots-clés sont retirés : une propriété ou une définition nommée `x-…` et les données de `enum`, `const`, `default`, `examples` restent. Le reste (`$defs`, `$ref`, `title`, `description`, contraintes) garde son sens. La `description` de l'outil est celle de la racine du schéma ; la version de l'enveloppe est lue dans le schéma assemblé embarqué, au démarrage (décidé le 6 novembre 2026, change `submit-nightly-batch`).
- Vérifier : `make check-schemas` (exemples valides, assemblage à jour).

## Copie générée de la partie fixe

`go:embed` n'atteint pas `docs/` : le même outil (`schemabundle -prompt`) écrit `backend/internal/analysis/prompt/prompt.gen.json`, la version (`prompt_version`) et la partie fixe de [`system-prompt.md`](system-prompt.md) : le texte de l'unique bloc ```` ```text ```` de la section « Partie fixe », à l'octet près. L'outil échoue si la version manque ou est en double, ou si la section n'a pas exactement un bloc `text`. Le package `internal/analysis/prompt` l'embarque.

- Fichier généré : on ne l'édite jamais (le hook `protect-files.sh` le refuse), on lance `make generate`.
- `make check-generated` échoue quand la copie n'est plus à jour : modifier la partie fixe sans régénérer casse la CI.

## Validation d'une réponse

Chaque réponse `succeeded` d'un batch est validée dans l'ordre ci-dessous ([ADR-0006](../adr/0006-sortie-structuree.md)). Le premier échec rejette toute la réponse : rien n'est écrit, ni fiche, ni proposition, ni tag. L'élément compte une tentative (`last_error = invalid_response`) et passe en `failed` à la troisième (RG-35). Chaque étape a une classe de motif, entre parenthèses : c'est tout ce qui est journalisé. Ni la réponse ni le message du validateur ne sont journalisés ou stockés (change `apply-batch-results`, design D5).

Deux cas précèdent la validation et ne sont pas des rejets :

- **Version d'enveloppe** : un batch envoyé avec une autre version de l'enveloppe que celle du binaire remet ses éléments dans la file, sans tentative comptée (`envelope_version`).
- **Réponse coupée** : `stop_reason = "max_tokens"`. Elle n'est ni validée ni enregistrée ; l'élément est retenté une fois avec un plafond de 8 000, puis passe en échec « fiche trop longue » (RG-36, voir « Structure d'une requête »).

Sans la base (`internal/analysis`) :

1. `stop_reason` vaut `tool_use` ; toute autre valeur est un rejet (`stop_reason`).
2. La réponse contient exactement un appel d'outil, nommé `classify_item`, quels que soient les blocs de texte (`tool_calls`).
3. L'entrée de l'outil est un seul objet JSON, sans membre en double dans un objet, à aucune profondeur (`schema`). Le schéma ne verrait que la dernière valeur d'un membre répété, alors que la fiche est enregistrée telle que reçue. Les objets d'un tableau peuvent partager leurs membres (les ingrédients d'une recette).
4. Les tags sont normalisés **avant** le schéma (RG-58) : chacun par `naming.Tag`, les résultats vides retirés, les doublons retirés en gardant le premier. Moins de 2 tags distincts restants : rejet (`too_few_tags`). Des `tags` qui ne sont pas un tableau de chaînes sont laissés au schéma.
5. L'entrée, tags remplacés, est valide selon le schéma assemblé (`schema`). Un tag encore hors de la forme permise (`c++`) rend la réponse invalide.
6. Les champs typés sont lus et la version du schéma de la fiche est prise dans `x-schema-version` de `fiche_<template>` (`schema`).

Dans la transaction d'écriture (`store.WriteAnalysis`), avant toute écriture :

7. **Type** (`unknown_type`). `type.key` : un type non supprimé de cette clé. `type.proposed` dont la clé existe déjà : l'élément est rangé sous ce type, qui n'est pas modifié (RG-56, RG-57). Sinon le type est créé `proposed`. Une clé portée seulement par un type supprimé est refusée en attendant la fusion des types (E4).
8. **Template** (`template`). `fiche.template` vaut `type.key` pour un type existant choisi par sa clé qui a un template, `generic` sinon : type sans template, type proposé, ou type proposé dont la clé existe déjà (sa fiche générique est gardée, comme après une fusion, RG-57, RG-72).
9. **Catégorie** (`unknown_category`). `category.id` est une catégorie non supprimée de l'utilisateur (RG-90).

Puis, la réponse étant valide, une catégorie proposée est rangée en deux temps, qui ne rejettent jamais :

10. **Correction du parent** (RG-55), d'abord. Parent absent, inconnu, supprimé ou d'un autre utilisateur : racine. Parent sous-catégorie : sa racine si elle n'est pas supprimée, sinon racine. La profondeur (RG-51) tient donc toujours.
11. **Rapprochement** (RG-54), ensuite. Une catégorie non supprimée de l'utilisateur, sous le parent corrigé, de même nom normalisé, est réutilisée telle quelle (RG-56). Nom normalisé : minuscules, accents retirés, espaces intérieurs réduits à un, bords rognés ; « Ménage » = « menage », « Java & Spring » ≠ « Java et Spring ». Sinon la catégorie est créée `proposed`, sans couleur propre (RG-59).

Un champ corrigé à la main (`user_edited`) est validé comme les autres : une réponse invalide sur ce champ reste rejetée. Il ne crée simplement rien et garde sa valeur (RG-61).

## Ce que le backend calcule lui-même

Pour économiser des tokens et éviter des erreurs, l'IA ne fournit pas :

- le **temps de lecture** de l'original (calculé à l'extraction, RG-25) ;
- la **version de schéma** (lue dans `x-schema-version` du schéma de la fiche) ;
- la **version du prompt** et le **modèle** (configuration du backend) ;
- la **couleur** d'une catégorie proposée (héritée du parent, ou couleur par défaut).

## Versionner

- Le prompt a une version globale (`prompt_version`), en tête de [`system-prompt.md`](system-prompt.md), avec un historique.
- Chaque schéma de fiche a sa version (`x-schema-version`). Modifier un schéma de façon incompatible incrémente sa version ; le template de l'appli doit savoir afficher les versions encore présentes en base, ou les fiches concernées sont réanalysées.
- L'enveloppe a aussi sa version (`x-schema-version` de `classify-item.schema.json`).

## Modifier le prompt ou un schéma

1. Modifier le fichier, incrémenter la version, compléter l'historique.
2. Lancer le jeu de test (`/eval-prompt`) et comparer au dernier rapport.
3. Ne déployer que si aucune régression de fidélité n'apparaît (voir [`eval-set.md`](eval-set.md)).
4. Si un schéma de fiche change : mettre à jour le template de l'appli, le générateur Markdown et leurs tests dans le même changement.
5. Lancer `make generate` : les fiches font partie du contrat d'API ([ADR-0035](../adr/0035-contrat-identifiants-et-synchronisation.md)), le serveur Go et les types TypeScript changent avec elles. Un changement incompatible d'une fiche est un changement cassant de l'API : réanalyser le type avant la nouvelle version de l'appli, ou ajouter un composant versionné.
