---
name: improve-codebase-architecture
description: Periodic architecture audit — scan Hippo's hot spots for deepening opportunities (shallow modules, leaking seams, untestable interfaces), present them as a visual HTML report, then frame the one the user picks. User-invoked, run every few changes, never mid-change.
argument-hint: "[module, subsystem or pain point to focus on]"
disable-model-invocation: true
---

# Improve Codebase Architecture (Hippo)

Adapted from mattpocock/skills `improve-codebase-architecture` + `codebase-design` (MIT).
Surface architectural friction and propose **deepening opportunities**: refactors that turn
shallow modules into deep ones. The aim is testability and agent-navigability.

Read [VOCABULARY.md](VOCABULARY.md) first and use its terms exactly (**module**, **interface**,
**depth**, **seam**, **adapter**, **leverage**, **locality**, the deletion test, "one adapter =
hypothetical seam, two = real"). Domain names come from `docs/glossary.md`.

Read-only on source code: this skill produces a report and a framing, never a refactor.

## Ground rules (Hippo)

- `docs/metier.md` (`RG-xx`) and `openspec/specs/` define observable behaviour; a deepening must
  not change it (HTTP contract in `api/openapi.yaml`, fiche format in `docs/prompt/schemas/`). A
  candidate that would is a product change, not a refactor — say so.
- Respect the conventions and ADRs; don't re-litigate them: `docs/dev/conventions-backend.md`,
  `docs/dev/conventions-app.md`, `docs/adr/`. The depguard rules of `backend/.golangci.yml` are
  enforced invariants: a candidate that needs one relaxed reopens an architecture decision.
- Highest bar on the paths that guard a rule of gold: nothing private sent to the AI (RG-12),
  `user_id` on every query (RG-90), the monthly budget checked before each send (RG-81). A
  candidate there states how the guarantee and its test survive.

## 1. Explore

**Scope before you scan (YAGNI).** If the user named a direction, take it. Otherwise:

1. `graphify check-update .` (then `graphify update .` if stale) — never trust a stale graph.
2. Hot spots: `git log --oneline -60 --name-only` → the files and areas that keep coming back.
3. Hubs and blast radius: `graphify god-nodes` (ignore test helpers, high degree by construction), `graphify affected "<symbol>"` on
   the hot spots, `graphify explain` on suspected shallow modules, `graphify path "<A>" "<B>"` to
   confirm a suspected coupling.

Then dispatch a read-only sub-agent (remind it to query the graph before grepping) to walk the
hot spots and note friction:

- understanding one concept requires bouncing between many small modules;
- **shallow** modules: interface nearly as complex as the implementation;
- pure functions extracted for testability while the bugs live in how they're called (no **locality**);
- tightly-coupled modules leaking across their seams;
- untested parts, or parts hard to test through their current interface.

Apply the **deletion test** to every suspect. See [DEEPENING.md](DEEPENING.md) for dependency
categories (SQLite on a temporary file = local-substitutable; the Anthropic API behind `Analyzer` and
the web pages fetched by the extraction = true external).

## 2. Present candidates as an HTML report

Write a self-contained HTML file to `$TMPDIR/architecture-review-<timestamp>.html` (nothing lands
in the repo), `open` it, and give the absolute path. Follow [HTML-REPORT.md](HTML-REPORT.md):
one card per candidate (files, problem, solution, wins, before/after diagram, strength badge
`Strong` / `Worth exploring` / `Speculative`, ADR callout if any), then a **Top recommendation**.
3–6 candidates max. Don't propose interfaces yet.

Then ask: "Which of these would you like to explore?"

## 3. Framing loop (not a question barrage)

Once the user picks a candidate, frame it **with** them:

- Goal first. Rounds of **at most 3 questions**, each with a concrete example (code excerpt,
  before/after call site) and your recommended answer. Use AskUserQuestion when options are discrete.
- Facts are your job: look them up (graph, code, specs) instead of asking.
- Cover: constraints, dependencies (category per DEEPENING.md), shape of the deepened module,
  what sits behind the seam, which tests survive (interface = test surface; replace, don't layer).
- A wide-blast candidate (`graphify affected` crosses more than one module) is framed as
  add → migrate callers → remove, each step green.
- To compare alternative interfaces, use [DESIGN-IT-TWICE.md](DESIGN-IT-TWICE.md).
- New or sharpened domain term → propose it for `docs/glossary.md` (see the `domain-modeling` skill).

## 4. Outcome

- Accepted candidate → **one line** in `docs/backlog.md`, section « Chantiers techniques »
  (created on first use: chantier | description | taille | origine), to be run later as an
  OpenSpec change.
  No code here.
- Rejected with a load-bearing reason → offer to record it (ADR in `docs/adr/` if it meets the
  three criteria of `domain-modeling`, otherwise a note in the relevant architecture doc) so future
  audits don't re-suggest it. Skip ephemeral reasons ("not now").
