# Tasks

No API contract, no migration, no prompt nor fiche schema, no dependency in this change (design.md, "Packages touched").

## 1. Recognising a video

- [x] 1.1 Write failing table tests for `videoID` per design.md ("Recognising a video": each form, trailing slash, ids of 10 and 12 characters, `watch` without `v`, two `v`, look-alike hosts, channel and playlist URLs), then implement it in `youtube.go` with the canonical page and oEmbed URLs; verify with `-race`

## 2. Fetch options

- [x] 2.1 Write failing fetch tests for the per-call options of design.md ("Requests and time budget"): `modeJSON` reads a JSON body whole up to 64 KiB and marks it too large beyond; `modeHTML` never writes a PDF to the temporary directory; a deadline of the call is classified `timeout` with the worker's context, never `stopped`. Then change `fetch` to take its timeout, `Accept` and mode per call, and move the page path and `export_test.go` `Fetcher` to it with the page options of today; verify every existing fetch, page and PDF test passes unchanged with `-race`

## 3. Reading the player data

- [x] 3.1 Write failing child tests for the `youtube` reader (fields read, token absent, `= null` before the real block, malformed JSON, 10 001 levels of nesting, another id then the right one, empty title, `title` of another type, `lengthSeconds` `"abc"`, `"-5"`, `"0"`, `754`, `"9999999"`, description with `\r\n`, NUL and 2 MiB of `"`), then implement the reader in the child with `formatYouTube` and `childRequest.VideoID`, building the final outcome (cleaning, `cutTitle`, `truncate`, reading time) in the child; add `formatYouTube` to `childReasons`; verify the memory watchdog, the time limit and the output bound apply unchanged

## 4. Worker

- [x] 4.1 Extend the test web of `worker_test.go`: certificate and resolver for `youtube.com`, `*.youtube.com` and `youtu.be`; handlers of `www.youtube.com/watch` and `/oembed` that dispatch on the query; raw queries recorded. Write failing worker tests, one sub-test per scenario of `specs/item-extraction/spec.md` named after it, on fictional pages built by code and a real temporary SQLite; remove the « YouTube link » sub-test with its requirement; verify the new tests fail
- [x] 4.2 Live check, once, from the development machine: request one public video's canonical page and its oEmbed with exactly the fetcher's headers (`User-Agent: Hippo/dev (personal read-later)`, the page `Accept`, `Accept-Language: fr,en`, no cookie) and record in design.md, Context, whether `ytInitialPlayerResponse` with `videoDetails` is present, the page size and the final host; no page content is committed (ADR-0027). If the player data are missing, stop and report; if the machine has no network, mark the check "unverified" in design.md and report it
- [x] 4.3 Implement the video path of `worker.extract` per design.md: the `AttemptStarted` check and the attempt row before any request; the canonical page (2/3 of the timeout, `modeHTML`), the child reading, the oEmbed fallback (1/3 of the timeout, `modeJSON`, final host and status checked), a network outage on either request through `networkOutage`, stops and an unavailable reader as for a page; the reason classes and the `oembed_status` log field; `downloadHost` returning `www.youtube.com` for a video; remove `reasonDeferredYouTube` and the YouTube exception of `downloadHost`. Verify the tests of 4.1 pass with `-race`, the page and PDF tests still pass, and the logs tests find neither the id, the host, the title nor the channel

## 5. Measures

- [x] 5.1 Measure with `MeasureChild` (or the existing measure helper) the child's peak and duration on two fictional video pages of 1.9 MiB: player data of the real shape with a 900-character description, and player data whose description fills the page (ASCII, then 4-byte characters); write the measures in design.md; verify the peak stays under half of `ChildMemoryLimit`, or stop and report

## 6. Documentation

- [x] 6.1 `docs/spec-v1.md`, extraction section: the YouTube row of the table of links (no longer « Laissé en file jusqu'à E2-06 »), the stack row (no transcript), the summary line of the pipeline (no transcript) and the status line (E2-06 delivered without transcript); `docs/glossary.md`: « Extraction » (a video's metadata, no transcript in V1), « Repli » (a video keeps its description and its duration), « Format de la réponse » (`youtube`), « Classe de raison » (`oembed`, `no_metadata` added, `deferred_youtube` removed); verify `make check-docs` passes

## 7. Checks

- [x] 7.1 `make check` green; code-reviewer pass with no 🛑 nor 🔴 open; every scenario has its named sub-test
