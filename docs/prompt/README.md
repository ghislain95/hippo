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

Les fichiers de schéma utilisent des `$ref` entre fichiers pour rester lisibles. Le backend les **assemble au build** en un seul schéma autonome (sans `$ref` externe), utilisé à la fois comme `input_schema` de l'outil et pour la validation des réponses. La CI vérifie que l'assemblage est à jour.

- Outil : `backend/cmd/schemabundle` ([ADR-0031](../adr/0031-generation-depuis-les-contrats.md)). Chaque fichier référencé devient une définition de `$defs` au nom stable : `fiche_<type>` pour une fiche, `common_<nom>` pour une définition de `_common.schema.json`.
- Sortie : `backend/internal/analysis/schema/classify-item.bundled.json`, embarqué dans le binaire. Fichier généré : on ne l'édite jamais, on lance `make generate`.
- **Exemples** : [`schemas/examples/`](schemas/examples/) contient une réponse `classify_item` complète et fictive par type de fiche. L'outil les valide contre le schéma assemblé et échoue s'il manque l'exemple d'un type. Ils servent aussi de fixtures aux tests de rendu des templates et de l'export Markdown.
- Vérifier : `make check-schemas` (exemples valides, assemblage à jour).

## Validation d'une réponse

Dans cet ordre ; le premier échec rejette toute la réponse (RG-35) :

1. La réponse contient exactement un appel à `classify_item`.
2. L'entrée de l'outil est valide selon le schéma assemblé.
3. `fiche.template` est cohérent avec le type : égal à `type.key` pour un type existant qui a un template, `generic` pour un type proposé ou sans template.
4. `type.key` existe ; `category.id` existe et appartient à l'utilisateur.
5. Une proposition de catégorie passe par le dédoublonnage (RG-54) et la correction du parent (RG-55) : ce ne sont pas des motifs de rejet.
6. Les tags sont normalisés (RG-58).

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
