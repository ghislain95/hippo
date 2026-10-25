<!-- Titre : sujet du commit principal, en anglais (feat(<change-id>): …). Description en français. -->

## Contexte

<!-- Pourquoi ce changement ; story de la backlog (E?-??) ; change OpenSpec `openspec/changes/<id>/` ou « hotfix ». -->

## Règles et décisions

- Règles métier : RG-…
- ADR : ADR-…
- Choix techniques notables :

## Impacts

<!-- « aucun » si rien ne change. -->

- Coût IA :
- Éléments privés (RG-12) :
- Données persistées / migration :
- Contrat d'API (`api/openapi.yaml`) :
- Prompt ou schéma de fiche (version, rapport `/eval-prompt`) :

## Tests

<!-- Ce qui a été lancé et son résultat (make check, tests ciblés, QA). -->

## Définition de « terminé » (`docs/dev/workflow.md`)

- [ ] `make check` vert en local
- [ ] Tâches du change toutes cochées, `design.md` à jour, `openspec validate` OK
- [ ] `RG-xx` et ADR cités ; aucune règle métier inventée
- [ ] Un test par scénario ; tests obligatoires présents (privé, `user_id`, budget) si le chemin existe
- [ ] Contrat d'abord ; code généré à jour ; prompt ou schéma versionné et évalué si touché
- [ ] Aucune baisse de qualité masquée (test sauté, assertion affaiblie, `nolint`, `any`…)
- [ ] Doc à jour (glossaire, spec, `docs/dev/`, `CLAUDE.md`) ; story cochée dans la backlog ; change archivé
- [ ] Relecture `code-reviewer` sans 🛑 ni 🔴
- [ ] QA de Ghislain faite (si « User QA: yes »)
