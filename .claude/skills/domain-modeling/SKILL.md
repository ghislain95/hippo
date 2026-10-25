---
name: domain-modeling
description: Build and sharpen Hippo's domain vocabulary while framing or designing — challenge terms that conflict with docs/glossary.md, propose canonical terms for fuzzy ones, stress-test concepts with edge-case scenarios, and update the glossary as terms resolve. Use when a framing/design discussion introduces, renames or disputes a domain concept, or when editing the glossary.
---

# Domain Modeling (Hippo)

Adapted from mattpocock/skills `domain-modeling` (MIT).

This is the *active* discipline: challenging terms, inventing edge-case scenarios, and writing
the glossary down the moment a term crystallises. Merely *reading* the glossary for vocabulary
is not this skill — every framing and design already does that.

## Files

- Glossary: **`docs/glossary.md`** — French term, English code identifier, definition, grouped in
  thematic tables, plus the « Mots à éviter » table. Format in [GLOSSARY-FORMAT.md](GLOSSARY-FORMAT.md).
- Business rules: `docs/metier.md` (`RG-xx`), the source of truth for behaviour. Detailed
  behaviour for development: `openspec/specs/`. Decisions: `docs/adr/NNNN-<titre>.md`.
- A term marked *(V1.5)* or *(V2)* is reserved: never reuse it for something else.

## During the session

**Challenge against the glossary.** A term used in a way that conflicts with the glossary is
called out immediately: "The glossary defines *catégorie* as the single place of an item in the
tree, but you seem to mean its *type de contenu* (the shape of the fiche). Which is it?"

**Sharpen fuzzy language.** Overloaded word → propose the canonical term: "By *traiter* do you
mean *extraire* (local, free) or *analyser* (AI, paid)? They're different steps."

**Discuss concrete scenarios.** Stress-test relationships with specific edge cases — a private item
re-captured with the same URL, a proposed category merged while items are being analysed, a
reanalysis of an item whose category was corrected by hand, a tag that normalises to an
existing one — to force precise boundaries.

**Cross-reference with code.** When the user states how something works, check the code (graph
first: `graphify query` / `explain`). Contradiction → surface it: "The code does X (`file:line`), but you
just said Y. Which is right?"

**Update the glossary inline.** When a term is resolved — *by the user*, not by you alone —
update `docs/glossary.md` right away. The glossary holds no implementation details: no
thresholds, table names, migrations or algorithms. It is not a spec.

**Code identifiers.** Code stays English. When a new concept gets a code name, record the
FR ↔ EN pair in the glossary so product and code keep the same concept under one name each.

## Decisions worth recording

Offer to record a decision as an ADR (`docs/adr/`) only when all three hold:
1. **Hard to reverse** — changing your mind later has a real cost;
2. **Surprising without context** — a future reader will wonder "why this way?";
3. **A real trade-off** — there were genuine alternatives.

If any is missing, skip it: the decision lives in the change's `design.md`.
