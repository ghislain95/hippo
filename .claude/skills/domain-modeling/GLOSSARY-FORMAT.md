# docs/glossary.md — format

```md
### Fiche et classement   (thematic table under `## Termes`)

| Terme | Code | Définition |
| --- | --- | --- |
| **Proposition** | `status = proposed` | Catégorie ou type créé par l'IA parce que rien d'existant ne convenait. … |
```

Synonyms to avoid go in the `## Mots à éviter` table (Ne pas dire | Dire | Pourquoi).

## Rules

- **French canonical term** (product vocabulary), then the **English code identifier(s)** in
  backticks, or `—`.
- **Be opinionated.** Several words for one concept → pick the best, list the others under `_Éviter_`.
- **Tight definitions.** One or two sentences: what it IS, not what it does. No implementation
  details (thresholds, tables, migrations, classes beyond the identifier).
- **Only Hippo-specific terms.** General programming concepts (timeout, cache, endpoint) don't belong.
- **Group** terms under thematic subheadings.
- Unresolved conflicts are marked `⚠️ À arbitrer` until the user decides.
- A term of a later version is marked *(V1.5)* or *(V2)*: defined now so nobody reuses the word.
