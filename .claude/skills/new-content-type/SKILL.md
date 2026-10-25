---
name: new-content-type
description: Guide the addition of a content type with its own fiche schema and template (ADR-0011) — schema, prompt, envelope, example, app template, Markdown export, tests, glossary — or the promotion of an AI-proposed type to a dedicated template. Use when Ghislain asks for a new type of fiche, or accepts a proposed type and wants a real template for it.
argument-hint: "<key> [display name]"
---

# /new-content-type — ajouter un type de contenu

Ajouter un type est une opération transverse (ADR-0011) : schéma, prompt, template, export Markdown, tests. Elle se fait **dans un change OpenSpec** (`/opsx:new add-content-type-<key>`), avec Ghislain au cadrage : la structure de la fiche est une décision produit.

## 1. Cadrer (avec Ghislain)

- Clé en anglais `snake_case` (`$ARGUMENTS`), nom affiché en français.
- Quand l'utiliser, et ce que la fiche contient (comme le tableau de `docs/metier.md` §7). Champs obligatoires, champs nullables (« inconnu = null, ne pas inventer »), listes.
- Le type existe-t-il déjà en `proposed` ? Alors ses fiches actuelles sont au format `generic` : prévoir leur réanalyse.
- Coût : un type de plus allonge l'outil `classify_item`, donc chaque requête (tokens en cache).

## 2. Liste des fichiers à toucher (dans cet ordre)

1. `docs/prompt/schemas/fiches/<key>.schema.json` : `template` en `const`, `x-schema-version: 1`, `additionalProperties: false`, descriptions en français qui guident le modèle, définitions communes de `_common.schema.json`.
2. `docs/prompt/schemas/classify-item.schema.json` : ajout dans `fiche.anyOf`, `x-schema-version` de l'enveloppe incrémenté.
3. `docs/prompt/schemas/examples/<key>.json` : un exemple complet, fictif.
4. `docs/prompt/system-prompt.md` : ligne du référentiel `<key> | <nom>`, règles du type dans « Règles par type », version du prompt et historique.
5. `make generate && make check-schemas`.
6. Migration : insertion du type dans `content_types` (`status = active`, `has_template = 1`), ou passage de `proposed` à `active` avec `has_template = 1`.
7. Backend : `search_text` (texte aplati) et export Markdown du type, avec leurs tests sur l'exemple.
8. Appli : template `app/src/templates/<Key>Template.tsx`, enregistrement dans la table des templates, test de rendu sur l'exemple.
9. Doc : `docs/metier.md` §7 (tableau), `docs/glossary.md` (clé ajoutée à la liste des types), `docs/spec-v1.md` si les templates y sont décrits, ADR-0011 n'est pas réécrit.
10. Jeu de test : au moins un cas du type dans `docs/prompt/eval-set.md` et `backend/testdata/eval/`, puis `/eval-prompt` (avec l'accord de Ghislain).

## 3. Vérifier

`make check` vert, `/eval-prompt` sans échec bloquant, `/dod-check`.
