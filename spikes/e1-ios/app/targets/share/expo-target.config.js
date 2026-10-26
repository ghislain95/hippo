// Share extension of the E1 prototype (design D2). No `entitlements`: no App Group (ADR-0014).
/** @type {import('@bacons/apple-targets/app.plugin').ConfigFunction} */
module.exports = () => ({
  type: 'share',
  name: 'HippoShare',
  displayName: 'Hippo E1',
  bundleIdentifier: '.share',
  deploymentTarget: '18.0',
});
