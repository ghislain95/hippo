import type { ConfigContext, ExpoConfig } from 'expo/config';

import { resolveApiUrl, usesPlainHttp } from './plugins/with-share-config';

// The server URL and the API token are injected at build time (ADR-0014, ADR-0016): set
// HIPPO_API_URL and HIPPO_API_TOKEN in the shell that runs `expo start`, `make build-ios-sim` or
// `make ipa`. They are never written in the code nor committed. The share extension gets the same
// values from ./plugins/with-share-config.js, which refuses a missing token or an insecure URL at
// prebuild (design D1). HIPPO_APPLE_TEAM_ID is only needed for a signed device build.
const apiUrl = resolveApiUrl(process.env);

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: 'Hippo',
  slug: 'hippo',
  scheme: 'hippo',
  version: '0.1.0',
  orientation: 'portrait',
  icon: './assets/icon.png',
  userInterfaceStyle: 'automatic',
  ios: {
    bundleIdentifier: 'com.ghislain95.hippo',
    supportsTablet: false,
    appleTeamId: process.env.HIPPO_APPLE_TEAM_ID,
    // Plain HTTP only reaches the local backend on the simulator; an https URL needs no exception.
    infoPlist: usesPlainHttp(apiUrl)
      ? { NSAppTransportSecurity: { NSAllowsLocalNetworking: true } }
      : {},
  },
  // The share extension's target comes from targets/share/ (ADR-0036); its configuration is written
  // after it, at prebuild.
  plugins: ['expo-router', '@bacons/apple-targets', './plugins/with-share-config'],
  experiments: {
    typedRoutes: true,
  },
  extra: {
    apiUrl,
    apiToken: process.env.HIPPO_API_TOKEN ?? '',
  },
});
