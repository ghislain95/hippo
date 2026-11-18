import type { ReactNode } from 'react';
import { ScrollView, StyleSheet } from 'react-native';

import { useTheme } from './theme/useTheme';

type Props = {
  children: ReactNode;
  testID?: string;
};

/**
 * A scrolling screen on the theme's background, its content kept clear of the notch and the
 * home indicator by iOS, so that a large text size never cuts it.
 */
export function Screen({ children, testID }: Props) {
  const theme = useTheme();
  return (
    <ScrollView
      testID={testID}
      style={{ backgroundColor: theme.colors.background }}
      contentInsetAdjustmentBehavior="automatic"
      contentContainerStyle={[styles.content, { padding: theme.spacing.xl, gap: theme.spacing.lg }]}
    >
      {children}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: {
    flexGrow: 1,
  },
});
