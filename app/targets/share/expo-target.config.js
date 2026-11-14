// Share extension of Hippo (ADR-0036, design D2). No `entitlements`: no App Group and no shared
// keychain (ADR-0013, ADR-0014). The Info.plist next to this file is versioned and never
// rewritten by the plugin.
const tokens = require('../../src/ui/theme/tokens.json');

// One colour set per token, with its light and dark values, from the app's theme (design D10).
// The plugin writes them into Assets.xcassets at prebuild; SwiftUI reads them as Color("name").
const colors = Object.fromEntries(
  Object.keys(tokens.colors.light).map((name) => [
    name,
    { light: tokens.colors.light[name], dark: tokens.colors.dark[name] },
  ]),
);

/** @type {import('@bacons/apple-targets/app.plugin').ConfigFunction} */
module.exports = () => ({
  type: 'share',
  name: 'HippoShare',
  displayName: 'Hippo',
  bundleIdentifier: '.share',
  deploymentTarget: '18.0',
  colors,
});
