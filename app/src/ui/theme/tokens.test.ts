import type { ExpoConfig } from 'expo/config';

import shareTargetConfig from '../../../targets/share/expo-target.config';

import tokens from './tokens.json';

describe("The extension's colours come from the app's theme tokens", () => {
  test('Same tokens in the app and the extension', () => {
    const config: ExpoConfig = { name: 'Hippo', slug: 'hippo' };
    const colors: Record<string, unknown> = shareTargetConfig(config).colors ?? {};

    const names = Object.keys(tokens.colors.light);
    expect(Object.keys(tokens.colors.dark).sort()).toEqual([...names].sort());
    expect(Object.keys(colors).sort()).toEqual([...names].sort());
    for (const name of names) {
      expect(colors[name]).toEqual({
        light: tokens.colors.light[name as keyof typeof tokens.colors.light],
        dark: tokens.colors.dark[name as keyof typeof tokens.colors.dark],
      });
    }
  });
});
