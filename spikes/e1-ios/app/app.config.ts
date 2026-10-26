import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

import type { ConfigContext, ExpoConfig } from 'expo/config';

// Throwaway E1 prototype (openspec/changes/ios-prototype). The server URL and the token come
// from `.spike.json`, written by `npm run configure` (design D4).
type SpikeConfig = { url: string; token: string; local: boolean };

const configFile = join(__dirname, '.spike.json');
if (!existsSync(configFile)) {
  throw new Error('Run `npm run configure` first: .spike.json is missing.');
}
const spike = JSON.parse(readFileSync(configFile, 'utf8')) as SpikeConfig;

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: 'Hippo E1',
  slug: 'hippo-e1-spike',
  scheme: 'hippo-e1',
  version: '0.0.1',
  orientation: 'portrait',
  icon: './assets/icon.png',
  userInterfaceStyle: 'automatic',
  ios: {
    // Fixed identifiers: a free account allows 10 App IDs per 7 days (design, risks).
    bundleIdentifier: 'com.ghislain95.hippo.spike',
    supportsTablet: false,
    appleTeamId: process.env.HIPPO_APPLE_TEAM_ID,
    infoPlist: spike.local ? { NSAppTransportSecurity: { NSAllowsLocalNetworking: true } } : {},
  },
  plugins: ['expo-router', '@bacons/apple-targets'],
  extra: {
    serverUrl: spike.url,
    token: spike.token,
  },
});
