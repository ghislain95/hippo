// Share extension of Hippo (ADR-0036, design D2). No `entitlements`: no App Group and no shared
// keychain (ADR-0013, ADR-0014). The Info.plist next to this file is versioned and never
// rewritten by the plugin.
/** @type {import('@bacons/apple-targets/app.plugin').ConfigFunction} */
module.exports = () => ({
  type: 'share',
  name: 'HippoShare',
  displayName: 'Hippo',
  bundleIdentifier: '.share',
  deploymentTarget: '18.0',
});
