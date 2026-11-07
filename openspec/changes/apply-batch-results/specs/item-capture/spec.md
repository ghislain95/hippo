# Spec Delta

## ADDED Requirements

### Requirement: A duplicate of an analysed link answers its classification
A duplicate link capture of an `analyzed` item SHALL answer `200` with the item's content type key, category id, tag ids and fiche, as the item is stored (RG-04, RG-34, ADR-0035).

#### Scenario: Duplicate of an analysed link
- **GIVEN** an `analyzed` item for `https://example.com/a` with the type `recipe`, a category, two tags and a `recipe` fiche
- **WHEN** the user captures `{"url": "https://example.com/a"}`
- **THEN** the answer is `200` with `content_type` `recipe`, its `category_id`, its two tag ids and its fiche, and the item is unchanged
