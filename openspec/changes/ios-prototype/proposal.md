# Proposal: ios-prototype

## Why

Backlog epic **E1** (stories E1-01 to E1-04). The whole app rests on a chain nobody has tested yet with a free Apple account: a share extension that signs without paid capabilities, an app installed and refreshed with SideStore, and HTTPS calls to the NAS through Tailscale while SideStore also needs a VPN slot. If any link breaks, ADR-0012 to ADR-0014 must be revisited before E5. The epic exists to find out cheaply, with throwaway code, before any real app screen is built.

## What Changes

- **Throwaway prototype** in `spikes/e1-ios/`, never shipped: a minimal Expo app (SDK 58, development build) and a tiny Go echo server that logs what it receives.
- **Share extension in Swift**, generated into the Expo project by `@bacons/apple-targets` (decided during framing): a SwiftUI mini form (preview, capture note, private toggle, Enregistrer) that sends the URL, text or image itself to the server, with the server URL and token injected at build time and **no App Group** (ADR-0014). The library `expo-share-extension` listed in `docs/spec-v1.md` is not used: its compatibility table stops at SDK 54 and it creates an App Group by default.
- **Native look test screen** (decided during framing): native tab bar (`expo-router/native-tabs`), an iOS glass view (`expo-glass-effect`) and a SwiftUI component (`@expo/ui/swift-ui`), for Ghislain to judge on the iPhone before the E5 mock-ups.
- **On-device checks run by Ghislain**, following a step-by-step runbook: signing with a free Apple account, sharing from Safari and Instagram (URL, note, screenshot), install and weekly refresh through SideStore, the server reached in HTTPS through `tailscale serve` over 4G, and the Tailscale / SideStore VPN coexistence.
- **Kept on `main`**:
  - `docs/runbook-ios.md`, the exact install and refresh procedure (E1-02);
  - a results report with what worked, what did not, and the measured numbers (share time, memory);
  - the decision of E1-04: ADR-0014 confirmed, or a new ADR if an alternative is chosen.
  The prototype code is removed by a commit before the pull request, so it stays reachable in the history only.

## Non-goals

- Any production code in `app/` or `backend/`: the real share extension is E5-02, the real capture endpoint E2-01.
- Offline queueing, the backend API, authentication beyond a static test token, polishing the UI.
- Android.
- Choosing the paid Apple Developer account; the prototype only records whether it would be needed.

## Capabilities

### New Capabilities

None. The prototype has no durable behavior to specify (`skip_specs: true`); E5-02 will specify the share extension.

### Modified Capabilities

None.

## Impact

- **Stories**: E1-01, E1-02, E1-03, E1-04. **Business rules** exercised by the prototype: RG-01, RG-03, RG-06 (offline failure of the extension), RG-10. **ADRs**: ADR-0012, ADR-0013, ADR-0014 (to confirm or replace), ADR-0015, ADR-0016.
- **AI cost**: none. **Private items**: none sent anywhere but the local echo server. **Persisted data**: none.
- **API contract**: none (the echo server is outside the contract).
- **New dependencies, prototype only**: `@bacons/apple-targets`, `expo-glass-effect`, `@expo/ui` in `spikes/e1-ios/`; none reaches `app/` or `backend/` in this change. Their adoption in the real app will be decided by E5's changes.
- **Accounts and devices, Ghislain's actions**: the existing free Tailscale account (the NAS and two iPhones are already on it), the Mac added to it, HTTPS certificates enabled, the free Apple ID signed in Xcode, the iPhone in developer mode, SideStore and its first pairing from the Mac. No cost.
- **Docs**: `docs/runbook-ios.md` (new), the results report, ADR-0014 status or a new ADR, `docs/backlog.md` (E1 done), `docs/spec-v1.md` ("Risques et points à vérifier", and its libraries line if the Swift extension is kept).
- **User QA**: yes — the on-device checks are the QA, run by Ghislain with the runbook.
