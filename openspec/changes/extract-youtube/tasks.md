# Tasks

No API contract, no migration, no prompt nor fiche schema, no dependency in this change (design.md, "Packages touched").

## 1. Recognising a video

- [ ] 1.1 Write failing table tests for `videoID` per design.md ("Recognising a video"), then implement it in `youtube.go` with the canonical page and oEmbed URLs; verify with `-race`

## 2. Reading the player data

- [ ] 2.1 Write failing child tests for the `youtube` reader (fields read, marker absent, malformed JSON, another id, empty title, `lengthSeconds` cases), then implement the reader in the child with `formatYouTube`; verify the memory watchdog and time limit apply unchanged

## 3. Worker

- [ ] 3.1 Write failing worker tests, one sub-test per scenario of `specs/item-extraction/spec.md` named after it, on fictional pages built by code and a real temporary SQLite; verify they fail
- [ ] 3.2 Implement the video path of `worker.extract` (canonical page under 10 s, child reading, write per design.md), the oEmbed fallback (64 KiB, time left of the 15 s), the reason classes; remove `reasonDeferredYouTube` and the YouTube exception of `downloadHost`; verify the tests of 3.1 pass with `-race`, the page and PDF tests still pass, and the logs test finds neither the id, the title nor the channel

## 4. Measures

- [ ] 4.1 Measure with `MeasureChild` (or the existing measure helper) the child's peak and duration on a fictional 1.9 MiB video page; write the measure in design.md; verify the peak stays under half of `ChildMemoryLimit`, or stop and report

## 5. Documentation

- [ ] 5.1 `docs/spec-v1.md`, extraction section: the YouTube row of the table of links (no longer « Laissé en file jusqu'à E2-06 »), the stack row (no transcript), the summary line of the pipeline; `docs/glossary.md`: reason classes (`oembed`, `no_metadata` added, `deferred_youtube` removed); verify `make check-docs` passes

## 6. Checks

- [ ] 6.1 `make check` green; code-reviewer pass with no 🛑 nor 🔴 open; every scenario has its named sub-test
