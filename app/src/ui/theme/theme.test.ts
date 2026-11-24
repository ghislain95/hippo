import { navigationThemeFor, themeFor } from './theme';
import tokens from './tokens.json';

describe('navigationThemeFor', () => {
  test.each(['light', 'dark'] as const)(
    'the navigation bar and the screen background take the %s tokens',
    (scheme) => {
      const colors = tokens.colors[scheme];
      const navigation = navigationThemeFor(themeFor(scheme));

      expect(navigation.dark).toBe(scheme === 'dark');
      expect(navigation.colors).toMatchObject({
        background: colors.background,
        card: colors.surface,
        text: colors.text,
        primary: colors.accent,
        border: colors.border,
      });
    },
  );
});
