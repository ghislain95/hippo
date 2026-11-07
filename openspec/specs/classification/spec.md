# classification Specification

## Purpose
Describes the content types, categories and tags an item is classified with: their starting set, who owns them, how they are listed for the AI in the référentiel, how the categories and types the AI proposes are created (parent corrected, normalised name matched) and how the tags it gives are normalised and reused, without the pipeline ever changing what exists. Their management by the user is added by later stories.

## Requirements

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
The référentiel SHALL be a `<referentiel>` block holding, in this order: a `<types>` block with one `key | name` line per non-deleted type by creation order, a proposed type ending with `| proposed`; a `<categories>` block with one `id | path | status | description` line per non-deleted category of the user, each root followed by its sub-categories, both by creation order, the path joining names with ` › `; and the `<tags>` block of the user's tags (RG-52).

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
- **GIVEN** a user without any tag and with one `extracted` public item
- **WHEN** the pass sends their batch
- **THEN** the référentiel ends with an empty `<tags>` block followed by `</referentiel>`

### Requirement: The tags block lists the tags of the user's public items
The `<tags>` block SHALL hold one line of the names of the user's non-deleted tags carried by at least one non-private, non-deleted item, separated by `, `, most carried first, then by name, 300 at most; nothing else (RG-12, RG-52, RG-90).

#### Scenario: Tags are listed most used first, then by name
- **GIVEN** a user whose public items carry `go` three times, `docker` once and `ansible` once
- **WHEN** the pass sends their batch
- **THEN** the `<tags>` block holds the single line `go, ansible, docker`

#### Scenario: At most 300 tags are listed
- **GIVEN** a user with 301 tags, each carried by a public item, the tag `zzz-rare` carried once and all others twice
- **WHEN** the pass sends their batch
- **THEN** the `<tags>` block lists 300 names and not `zzz-rare`

#### Scenario: A tag carried only by private items is not listed
- **GIVEN** a user whose tag `sante` is carried only by an item checked Privé after its analysis, and whose tag `go` is carried by a public item
- **WHEN** the pass sends their batch
- **THEN** the `<tags>` block holds `go` and not `sante`

#### Scenario: A tag carried only by deleted items is not listed
- **GIVEN** a user whose tag `ancien` is carried only by a deleted item
- **WHEN** the pass sends their batch
- **THEN** the `<tags>` block does not hold `ancien`

#### Scenario: A deleted tag is not listed
- **GIVEN** a user whose tag `golang` was deleted while still attached to a public item
- **WHEN** the pass sends their batch
- **THEN** the `<tags>` block does not hold `golang`

#### Scenario: Another user's tags are never listed
- **GIVEN** two users, the second of whom has the tag `jardin-secret` on a public item
- **WHEN** the pass sends the first user's batch
- **THEN** the first user's `<tags>` block does not hold `jardin-secret`

### Requirement: A proposed category is created and the item filed in it
When a valid response proposes a category that no existing one matches, the backend SHALL create it for the item's user with the status `proposed`, the proposed name, the description, the reason and no colour of its own, and SHALL file the item in it (RG-53, RG-59, RG-90).

#### Scenario: A proposed category is created
- **GIVEN** a valid response proposing « Kubernetes » under « Infra & DevOps », with a description and a reason
- **WHEN** the batch is applied
- **THEN** the user has a category « Kubernetes » under « Infra & DevOps », `proposed`, with that description and reason, no colour of its own, a `change_seq`, and the item is filed in it

#### Scenario: A proposed root category is created
- **GIVEN** a valid response proposing « Photo » with no parent
- **WHEN** the batch is applied
- **THEN** the user has a root category « Photo », `proposed`, and the item is filed in it

#### Scenario: A proposed category is offered at the next batch
- **GIVEN** a category « Kubernetes » created by a proposal under « Infra & DevOps »
- **WHEN** the pass sends the user's next batch
- **THEN** its line reads `<id> | Infra & DevOps › Kubernetes | proposed | <its description>`

### Requirement: The parent of a proposed category is corrected, never a reason to reject
A proposed category whose parent is a sub-category SHALL be placed under that sub-category's root; a parent that does not exist, is deleted, or belongs to another user SHALL make it a root. Such a proposal is never rejected for its parent (RG-51, RG-55, RG-90).

#### Scenario: A sub-category as parent is replaced by its root
- **GIVEN** a valid response proposing « Kubernetes » under the id of « Conteneurs & orchestration »
- **WHEN** the batch is applied
- **THEN** « Kubernetes » is created under « Infra & DevOps » and the item is `analyzed`

#### Scenario: An unknown parent makes a root
- **GIVEN** a valid response proposing « Photo » under a category id that does not exist
- **WHEN** the batch is applied
- **THEN** « Photo » is created as a root and the item is `analyzed`

#### Scenario: A deleted parent makes a root
- **GIVEN** a valid response proposing « Bonsaï » under the id of the user's deleted category « Plantes »
- **WHEN** the batch is applied
- **THEN** « Bonsaï » is created as a root

#### Scenario: Another user's category as parent makes a root
- **GIVEN** a valid response proposing « Photo » under the id of another user's root category
- **WHEN** the batch is applied
- **THEN** « Photo » is created as a root of the item's user, and the other user's categories are unchanged

### Requirement: A proposed category reuses a category of the same normalised name
After the parent is corrected, a proposed category SHALL be matched against the user's non-deleted categories, active or proposed, under the same parent: same name once lower-cased, accents removed, inner spaces collapsed and edges trimmed. A match SHALL be reused, the item filed in it unchanged; otherwise one is created (RG-54, RG-90).

#### Scenario: An existing active category is reused
- **GIVEN** a valid response proposing «  ménage » under « Maison & quotidien », which holds the active « Ménage »
- **WHEN** the batch is applied
- **THEN** no category is created and the item is filed in « Ménage »

#### Scenario: An existing proposed category is reused
- **GIVEN** a user with the proposed category « Kubernetes » under « Infra & DevOps », and a valid response proposing « KUBERNETES » under « Infra & DevOps »
- **WHEN** the batch is applied
- **THEN** no category is created and the item is filed in the existing « Kubernetes »

#### Scenario: Inner spaces are collapsed
- **GIVEN** a valid response proposing « Java  &  Spring » under « Dev »
- **WHEN** the batch is applied
- **THEN** no category is created and the item is filed in « Java & Spring »

#### Scenario: Different words make another category
- **GIVEN** a valid response proposing « Java et Spring » under « Dev »
- **WHEN** the batch is applied
- **THEN** a proposed category « Java et Spring » is created under « Dev »

#### Scenario: The same name under another parent makes another category
- **GIVEN** a user with « Kubernetes » under « Infra & DevOps », and a valid response proposing « Kubernetes » as a root
- **WHEN** the batch is applied
- **THEN** a root category « Kubernetes » is created

#### Scenario: The corrected parent is used for the match
- **GIVEN** a user with « Kubernetes » under « Infra & DevOps », and a valid response proposing « Kubernetes » under the id of « Conteneurs & orchestration »
- **WHEN** the batch is applied
- **THEN** no category is created and the item is filed in the existing « Kubernetes »

#### Scenario: Two items of a batch proposing the same category get one
- **GIVEN** a batch whose two valid responses both propose « Kubernetes » under « Infra & DevOps »
- **WHEN** the batch is applied
- **THEN** one category « Kubernetes » is created and both items are filed in it

#### Scenario: A deleted category of the same name is not reused
- **GIVEN** a user whose category « Plantes » was deleted, and a valid response proposing « Plantes » as a root
- **WHEN** the batch is applied
- **THEN** a new proposed root « Plantes » is created, and the deleted one stays deleted

### Requirement: A proposed type is created without template and keeps a generic fiche
When a valid response proposes a type whose key no type holds, the backend SHALL create it `proposed` with its key, name and reason and without template, the item keeping its `generic` fiche. When the key already exists, the item SHALL be filed under the existing type with its `generic` fiche. Types stay shared by all users (RG-57, RG-72, ADR-0022).

#### Scenario: A proposed type is created
- **GIVEN** a valid response proposing the type `event` « Évènement » with a reason, and a `generic` fiche
- **WHEN** the batch is applied
- **THEN** a type `event` exists, `proposed`, without template, with that name and reason and a `change_seq`, and the item has the type `event` and its `generic` fiche

#### Scenario: Two items of a batch proposing the same type get one
- **GIVEN** a batch whose two valid responses both propose the type `event`
- **WHEN** the batch is applied
- **THEN** one type `event` exists and both items have it

#### Scenario: A proposed type whose key exists is filed under the existing type
- **GIVEN** a valid response proposing the type `recipe` « Recette maison » with a `generic` fiche
- **WHEN** the batch is applied
- **THEN** no type is created, the type `recipe` keeps its name and template, and the item has the type `recipe` and its `generic` fiche

#### Scenario: A proposed type is offered to every user
- **GIVEN** a type `event` proposed from an item of a first user
- **WHEN** the pass sends a second user's batch
- **THEN** the second user's `<types>` block holds the line `event | Évènement | proposed`

### Requirement: The analysis never changes an existing category, type or tag
Applying results SHALL only create categories, types and tags and attach them to items; it SHALL never rename, move, merge, delete or otherwise change one that existed before (RG-56, ADR-0009).

#### Scenario: Existing classification rows are left as they were
- **GIVEN** a user with categories, types and tags, and a batch whose valid responses reuse an existing category with another description, propose an existing type key with another name, and reuse existing tags
- **WHEN** the batch is applied
- **THEN** every category, type and tag that existed before has the same name, parent, status, description, colour, deletion mark and `change_seq` as before

### Requirement: Tags are normalised before the response is validated
Each tag of a response SHALL be lower-cased, stripped of accents, its spaces and `_` replaced by `-` (a run giving one `-`, none at the edges), cut to 40 characters, and duplicates dropped, before the envelope schema is checked. Fewer than 2 distinct tags left SHALL make the response invalid (RG-34, RG-35, RG-58).

#### Scenario: Spaces and capitals are normalised
- **GIVEN** a valid response whose tags are `Spring Boot` and `Java`
- **WHEN** the batch is applied
- **THEN** the item is `analyzed` with the tags `spring-boot` and `java`

#### Scenario: Accents and underscores are normalised
- **GIVEN** a valid response whose tags are `Électronique` and `machine_learning`
- **WHEN** the batch is applied
- **THEN** the item has the tags `electronique` and `machine-learning`

#### Scenario: A long tag is cut to 40 characters
- **GIVEN** a valid response whose tags are a 45-character tag and `go`
- **WHEN** the batch is applied
- **THEN** the item has the first 40 characters of the long tag, without a trailing `-`, and `go`

#### Scenario: Duplicates after normalisation are dropped
- **GIVEN** a valid response whose tags are `Go`, `go` and `golang`
- **WHEN** the batch is applied
- **THEN** the item has the two tags `go` and `golang`

#### Scenario: Fewer than two distinct tags make the response invalid
- **GIVEN** a `submitted` item with `attempts` 0 and a response whose tags are `Go` and `go`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with `attempts` 1, `last_error` `invalid_response`, and no tag is created

#### Scenario: A tag still outside the allowed form makes the response invalid
- **GIVEN** a response whose tags are `c++` and `go`
- **WHEN** the batch is applied
- **THEN** the item is `extracted` with one more attempt and nothing written

### Requirement: Tags are reused before they are created
An existing non-deleted tag of the user with the normalised name SHALL be reused; otherwise a tag SHALL be created for that user once, with a `change_seq`. Tags are never shared between users (RG-58, RG-90).

#### Scenario: An existing tag is reused
- **GIVEN** a user with the tag `go`, and a valid response with the tags `go` and `testing`
- **WHEN** the batch is applied
- **THEN** the item carries the existing `go` tag and a new `testing` tag, and the user has one tag named `go`

#### Scenario: A new tag of two items of a batch is created once
- **GIVEN** a batch whose two valid responses both carry the new tag `kubernetes`
- **WHEN** the batch is applied
- **THEN** the user has one tag `kubernetes`, carried by both items

#### Scenario: Another user's tag is not reused
- **GIVEN** another user with the tag `go`, and a valid response of the user with the tags `go` and `testing`
- **WHEN** the batch is applied
- **THEN** the user gets their own tag `go`, and the other user's tag carries no item of the user
