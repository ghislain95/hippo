# ADR-0028 — Langues des artefacts de développement

- **Statut** : Accepté
- **Date** : 2026-10-25
- **Complète** : [ADR-0026](0026-langues-doc-et-code.md)

## Contexte

ADR-0026 fixe le français pour la documentation, l'interface et les fiches, et l'anglais pour le code. Le setup introduit des artefacts qu'il ne couvre pas : les changes OpenSpec, les messages de commit, les pull requests, les instructions des agents.

## Décision

| Artefact | Langue |
| --- | --- |
| Code, identifiants, commentaires, noms de fichiers | Anglais (ADR-0026) |
| Artefacts OpenSpec (`proposal.md`, specs, `design.md`, `tasks.md`) | **Anglais** |
| Messages de commit, titres de PR, noms de branches | Anglais |
| Documentation (`docs/`, `README.md`, `CLAUDE.md`), y compris `docs/dev/` | Français |
| Définitions d'agents, skills et commandes de `.claude/` | Français |
| Description de PR, échanges avec Ghislain | Français |
| Textes de l'interface, fiches, prompt d'analyse | Français |

Règle de tri : **ce qui touche au code est en anglais** ; ce que Ghislain lit pour décider ou piloter est en français.

## Conséquences

- Les specs OpenSpec et le code partagent la même langue : un scénario se retrouve tel quel dans le nom de son test.
- Les termes du glossaire servent de pont : le terme français dans la doc, l'identifiant anglais dans le code et les specs.
- `openspec init` est fait sans option de langue (anglais par défaut).

## Alternatives écartées

- **Artefacts OpenSpec en français** (OpenSpec 1.14 le permet, en gardant les titres structurels et `SHALL`/`MUST` en anglais) : écarté par Ghislain, pour que tout ce qui touche au code soit dans une seule langue.
- **Agents et skills en anglais** : ce sont des documents que Ghislain lit et fait évoluer.
