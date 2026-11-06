# Spec Delta

## Purpose

Describes the content types and categories an item is classified with: their starting set, who owns them, and how they are listed for the AI in the référentiel. Proposals, tags and their management by the user are added by later stories.

## ADDED Requirements

### Requirement: The starting content types are shared by all users
The backend SHALL hold, for every user, the seven starting content types of `metier.md` §7, active and each with its own template, in this order: `tech_article` « Article technique », `tutorial` « Tuto », `tip` « Astuce », `recipe` « Recette », `place` « Lieu », `resource` « Ressource », `note` « Note perso ». Content types are shared between users (RG-57, ADR-0022).

#### Scenario: The types are listed for the AI
- **GIVEN** a user with one `extracted` public item
- **WHEN** the pass sends their batch
- **THEN** the `<types>` block of the référentiel holds, one per line and in this order, `tech_article | Article technique`, `tutorial | Tuto`, `tip | Astuce`, `recipe | Recette`, `place | Lieu`, `resource | Ressource`, `note | Note perso`

#### Scenario: Two users see the same types
- **GIVEN** two users, each with one `extracted` public item
- **WHEN** the pass sends their batches
- **THEN** both référentiels have the same `<types>` block

### Requirement: Every user starts with the categories of metier.md §6
A new user SHALL be created with the starting tree of `metier.md` §6: nine active root categories with the description given to the AI, and their active sub-categories, without description. A user that existed before the categories SHALL get this tree once, at the next start-up; a user who later deletes every category SHALL NOT get it again (RG-50, RG-51, RG-90).

#### Scenario: A new user gets the starting tree
- **GIVEN** a user just created
- **WHEN** the pass sends their batch
- **THEN** the `<categories>` block lists 24 categories: the 9 roots of §6 with their descriptions and the 15 sub-categories, all `active`

#### Scenario: A user created before the categories gets them once
- **GIVEN** a database whose user was created before the categories existed
- **WHEN** the backend starts twice
- **THEN** the user has the 24 starting categories once, not twice

#### Scenario: Deleting every category does not bring them back
- **GIVEN** a user whose starting categories were all deleted
- **WHEN** the backend starts again
- **THEN** the user has no category, and the `<categories>` block of their next batch is empty

#### Scenario: A user never sees another user's categories
- **GIVEN** two users, the second of whom has an extra category « Jardin secret »
- **WHEN** the pass sends both batches
- **THEN** the first user's référentiel does not contain « Jardin secret », and every category id it lists belongs to the first user

#### Scenario: The starting tree has two levels
- **GIVEN** a user just created
- **WHEN** their categories are listed
- **THEN** every sub-category's parent is a root category

### Requirement: The référentiel lists types and categories in a stable order
The référentiel SHALL be a `<referentiel>` block holding, in this order: a `<types>` block with one `key | name` line per non-deleted type by creation order, a proposed type ending with `| proposed`; a `<categories>` block with one `id | path | status | description` line per non-deleted category of the user, each root followed by its sub-categories, both by creation order, the path joining names with ` › `; and an empty `<tags>` block (RG-52).

#### Scenario: A sub-category follows its root with its path
- **GIVEN** a user just created
- **WHEN** the pass sends their batch
- **THEN** the line of « Java & Spring » reads `<id> | Dev › Java & Spring | active |` and comes after the line of « Dev » and before the next root

#### Scenario: A root category carries its description
- **GIVEN** a user just created
- **WHEN** the pass sends their batch
- **THEN** the line of « Cuisine » reads `<id> | Cuisine | active | Recettes, techniques et astuces de cuisine.`

#### Scenario: Proposed categories are listed with their status
- **GIVEN** a user with a proposed category « Kubernetes » under « Infra & DevOps »
- **WHEN** the pass sends their batch
- **THEN** its line reads `<id> | Infra & DevOps › Kubernetes | proposed | <its description>` and follows the starting sub-categories of « Infra & DevOps »

#### Scenario: A deleted category is not listed
- **GIVEN** a user whose category « Plantes » was deleted
- **WHEN** the pass sends their batch
- **THEN** no line of the référentiel names « Plantes »

#### Scenario: The tags block is empty
- **GIVEN** a user with one `extracted` public item
- **WHEN** the pass sends their batch
- **THEN** the référentiel ends with an empty `<tags>` block followed by `</referentiel>`
