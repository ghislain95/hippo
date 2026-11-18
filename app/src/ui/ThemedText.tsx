import { StyleSheet, Text, type TextProps } from 'react-native';

import type { ColorName, TextSize } from './theme/theme';
import { useTheme } from './theme/useTheme';

type Props = Omit<TextProps, 'allowFontScaling' | 'maxFontSizeMultiplier'> & {
  size?: TextSize;
  color?: ColorName;
  weight?: 'regular' | 'semibold' | 'bold';
};

const weights = { regular: '400', semibold: '600', bold: '700' } as const;

/**
 * Every text of the app: its size from the theme, its colour from the tokens. Font scaling is
 * never turned off, so the iOS text size setting (Dynamic Type) applies.
 */
export function ThemedText({
  size = 'body',
  color = 'text',
  weight = 'regular',
  style,
  ...props
}: Props) {
  const theme = useTheme();
  return (
    <Text
      {...props}
      style={StyleSheet.compose(
        {
          fontSize: theme.textSizes[size],
          fontWeight: weights[weight],
          color: theme.colors[color],
        },
        style,
      )}
    />
  );
}
