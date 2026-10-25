import type { ConfigContext, ExpoConfig } from 'expo/config';

// The server URL and the API token are injected at build time (ADR-0014, ADR-0016): set
// HIPPO_API_URL and HIPPO_API_TOKEN in the shell that runs `expo start` or `expo run:ios`.
// They are never written in the code nor committed.
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
  },
  plugins: ['expo-router'],
  experiments: {
    typedRoutes: true,
  },
  extra: {
    apiUrl: process.env.HIPPO_API_URL ?? 'http://127.0.0.1:8080',
    apiToken: process.env.HIPPO_API_TOKEN ?? '',
  },
});
