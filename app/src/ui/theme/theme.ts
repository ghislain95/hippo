import { DarkTheme, DefaultTheme, type Theme as NavigationTheme } from 'expo-router';

import tokens from './tokens.json';

/** The appearance iOS reports; null when it reports none. */
export type ColorScheme = 'light' | 'dark' | null | undefined;

export type ThemeColors = typeof tokens.colors.light;
export type ColorName = keyof ThemeColors;
export type TextSize = keyof typeof tokens.textSizes;

/** The app's theme: one token file shared with the share extension (design D10). */
export type Theme = {
  scheme: 'light' | 'dark';
  colors: ThemeColors;
  spacing: typeof tokens.spacing;
  radius: typeof tokens.radius;
  textSizes: typeof tokens.textSizes;
};

function buildTheme(scheme: 'light' | 'dark'): Theme {
  return {
    scheme,
    colors: tokens.colors[scheme],
    spacing: tokens.spacing,
    radius: tokens.radius,
    textSizes: tokens.textSizes,
  };
}

const themes = { light: buildTheme('light'), dark: buildTheme('dark') };

/** The theme for an appearance: dark when iOS reports dark, light otherwise or with none. */
export function themeFor(colorScheme: ColorScheme): Theme {
  return colorScheme === 'dark' ? themes.dark : themes.light;
}

/** The navigation theme (bars, screen background) built from the same tokens. */
export function navigationThemeFor(theme: Theme): NavigationTheme {
  const base = theme.scheme === 'dark' ? DarkTheme : DefaultTheme;
  return {
    ...base,
    colors: {
      ...base.colors,
      primary: theme.colors.accent,
      background: theme.colors.background,
      card: theme.colors.surface,
      text: theme.colors.text,
      border: theme.colors.border,
      notification: theme.colors.danger,
    },
  };
}
