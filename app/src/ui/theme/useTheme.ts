import { useColorScheme } from 'react-native';

import { themeFor, type Theme } from './theme';

/** The theme that follows the iOS appearance (light when iOS reports none). */
export function useTheme(): Theme {
  return themeFor(useColorScheme());
}
