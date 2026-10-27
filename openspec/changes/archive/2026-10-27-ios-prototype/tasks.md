# Tasks

## 1. Echo server

- [x] 1.1 Create the Go module `spikes/e1-ios/echo/` (standard library only) with `GET /ping` and `POST /api/items` (Bearer token read from `spikes/e1-ios/.spike-token`, JSON link or note, multipart image saved to `received/`), listening on `127.0.0.1:8787`, logging one line per request without token, note text or URL query (design D6); verify with `go run .` and a `curl` of each route
- [x] 1.2 Add the Go test (bad token → 401, link, note, image, malformed body → 400) and verify `go test ./...` passes in `spikes/e1-ios/echo/`
- [x] 1.3 Add `.gitignore` entries for `spikes/e1-ios/.spike-token`, `echo/received/` and the generated config files, and verify `git status` shows none of them after a run

## 2. Expo prototype app

- [x] 2.1 Create `spikes/e1-ios/app/` as an Expo SDK 58 project (same React Native version as `app/`, own `node_modules`), bundle id `com.ghislain95.hippo.spike`, and verify `npx expo-doctor` reports no blocking issue
- [x] 2.2 Add `@bacons/apple-targets`, `expo-glass-effect` and `@expo/ui` with `npx expo install`, and verify they resolve to SDK 58 versions
- [x] 2.3 Write `npm run configure` (design D4): token file, `targets/share/SpikeConfig.swift`, the extension `Info.plist` with the activation rule, `.spike.json` read by `app.config.ts`, and ATS local networking only for an `http` URL; verify by running it with an `http` and an `https` URL and reading the generated files

## 3. Share extension

- [x] 3.1 Declare `targets/share/expo-target.config.js` (`type: "share"`, no entitlements) and verify `npx expo prebuild --clean -p ios` creates the share target with no App Group entitlement
- [x] 3.2 Write `ShareViewController` hosting a SwiftUI view that reads the shared URL, text (URL detected with `NSDataDetector`, else note) or image (design D2), and verify it compiles with `xcodebuild` for the simulator
- [x] 3.3 Write the mini form (preview, capture note hidden for a note, Privé unchecked, Enregistrer) and the sending (JSON or multipart, Bearer token, design D3), with a clear error and a "Copier le lien" button when the server cannot be reached (RG-06); verify it compiles
- [x] 3.4 Show the elapsed time and the memory footprint after a send (design D8), and verify it compiles

## 4. Native look test screen

- [x] 4.1 Make the app root a `NativeTabs` layout with the tabs Verre and Réseau, and verify `npx tsc --noEmit` passes
- [x] 4.2 Write the Verre tab (`GlassView` styles, tint, interactive, `isLiquidGlassAvailable()`, a `Host` with SwiftUI controls) and verify `npx tsc --noEmit` passes
- [x] 4.3 Write the Réseau tab (server URL, `/ping` status and latency) and verify `npx tsc --noEmit` passes

## 5. Builds and runbook

- [x] 5.1 Ask Ghislain to accept the Xcode license and switch `xcode-select` (both need `sudo`), and verify `xcodebuild -license check` passes
- [x] 5.2 Build and run on the simulator (`npx expo run:ios`), share a page from Safari to the extension with the echo server on `http://127.0.0.1:8787`, and verify the echo server logs the link, a note and an image
- [x] 5.3 Check the glass screen and the network tab on the simulator, and verify with screenshots
- [x] 5.4 Write `npm run ipa` (unsigned Release archive packed as `Payload/*.app`), and verify the `.ipa` holds the app and its `PlugIns/*.appex`
- [x] 5.5 Write `docs/runbook-ios.md` (in French): Tailscale account and HTTPS certificates, `tailscale serve`, Apple ID in Xcode and developer mode, install through Xcode, SideStore pairing, install and weekly refresh with the Tailscale switch, bundle identifiers to keep. Verify every command in it against what was run, and verify `make check-docs` passes
- [x] 5.6 Write the QA checklist in `openspec/changes/ios-prototype/results.md` (each step, expected result, a blank for the observed result and the measures), and give it to Ghislain

## 6. On-device QA (Ghislain)

- [x] 6.1 Signing and install through Xcode with the free account: the app and the extension run on the iPhone
- [x] 6.2 Sharing from Safari and Instagram (link with a note, text, screenshot) reaches the echo server through `tailscale serve` in HTTPS, on Wi-Fi then on 4G
- [x] 6.3 Offline sharing shows the clear error and copies the link (RG-06)
- [x] 6.4 Install through SideStore, then a refresh from the iPhone with the Tailscale switch
- [x] 6.5 Glass screen judged on the iPhone; measures noted for a link and a large photo

## 7. Results and decision

- [x] 7.1 Fill `results.md` with Ghislain's observations and measures, and fix the runbook wherever reality differed
- [x] 7.2 Record the decision of E1-04: the ADR-0014 status, plus a new ADR through `/new-adr` for the Swift extension if it holds (or for the alternative chosen). Update `docs/spec-v1.md` (libraries, risks) and `docs/backlog.md` (E1 done), and verify `make check-docs` passes
- [x] 7.3 Remove `spikes/` in its own commit, and verify `git ls-files spikes` is empty and `make check` passes
- [x] 7.4 Run the `code-reviewer` pass on the branch, then `/opsx:verify`, and verify no 🛑 nor 🔴 is left
