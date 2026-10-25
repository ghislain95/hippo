# docs/ — documentation

La doc **fait foi** et est rédigée en **français** (ADR-0026). Gardien : l'agent `doc-keeper`.

## Hiérarchie

- `metier.md` : comportement et règles `RG-xx` (gagne en cas de contradiction).
- `adr/` : choix techniques ; `prompt/schemas/` : format des fiches.
- `glossary.md` : vocabulaire, identifiants de code.
- `spec-v1.md`, `backlog.md`, `prompt/`, `dev/` : s'alignent sur les précédents.

## Règles

- Une information vit à un seul endroit ; les autres pages la lient.
- Un numéro `RG-xx` n'est jamais réutilisé ; une règle supprimée est barrée, pas renumérotée.
- Un ADR accepté n'est jamais réécrit : un nouvel ADR le remplace ou l'amende (`/new-adr`) ; seul le statut de l'ancien change.
- Une règle métier nouvelle n'entre dans `metier.md` qu'avec l'accord de Ghislain.
- Toute info datée est datée explicitement ; phrases courtes, tableaux de référence.
- `openspec/specs/` n'est pas de la doc éditable : il bouge par `/opsx:archive`.
- Dépôt public : aucun contenu personnel ni secret.

## Commandes

```bash
make check-docs   # liens internes (lychee, hors ligne)
```
