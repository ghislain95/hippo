import { ActivityIndicator, StyleSheet, View } from 'react-native';

import type { ApiClient } from '@/api/client';
import { ThemedText } from '@/ui/ThemedText';
import type { ColorName } from '@/ui/theme/theme';
import { useTheme } from '@/ui/theme/useTheme';

import { useHealth } from './useHealth';

type Props = {
  client?: ApiClient;
};

/** Shows whether the Hippo server answers, as a pill (screen 1). */
export function ServerStatus({ client }: Props) {
  const health = useHealth(client);
  const theme = useTheme();

  const pill = (state: ColorName) => [
    styles.pill,
    {
      backgroundColor: theme.colors.surface,
      borderColor: theme.colors[state],
      borderRadius: theme.radius.pill,
      gap: theme.spacing.sm,
      paddingHorizontal: theme.spacing.md,
      paddingVertical: theme.spacing.sm,
    },
  ];
  const dot = (state: ColorName) => [styles.dot, { backgroundColor: theme.colors[state] }];

  if (health.isPending) {
    return (
      <View style={pill('border')} accessible>
        <ActivityIndicator color={theme.colors.textMuted} />
        <ThemedText size="caption" color="textMuted">
          Vérification du serveur…
        </ThemedText>
      </View>
    );
  }

  if (health.isError) {
    return (
      <View style={pill('danger')} accessible accessibilityRole="alert">
        <View style={dot('danger')} />
        <ThemedText size="caption" weight="semibold">
          Serveur injoignable
        </ThemedText>
      </View>
    );
  }

  return (
    <View style={pill('success')} accessible>
      <View style={dot('success')} />
      <ThemedText size="caption" weight="semibold">
        Serveur joignable
      </ThemedText>
    </View>
  );
}

const styles = StyleSheet.create({
  pill: {
    alignSelf: 'flex-start',
    alignItems: 'center',
    flexDirection: 'row',
    borderWidth: StyleSheet.hairlineWidth * 2,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
});
