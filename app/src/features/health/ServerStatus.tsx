import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';

import type { ApiClient } from '@/api/client';

import { useHealth } from './useHealth';

type Props = {
  client?: ApiClient;
};

/** Shows whether the Hippo server answers. */
export function ServerStatus({ client }: Props) {
  const health = useHealth(client);

  if (health.isPending) {
    return (
      <View style={styles.row}>
        <ActivityIndicator accessibilityLabel="Vérification du serveur" />
        <Text style={styles.text}>Vérification du serveur…</Text>
      </View>
    );
  }

  if (health.isError) {
    return (
      <View style={styles.row} accessibilityRole="alert">
        <Text style={styles.text}>Serveur injoignable</Text>
      </View>
    );
  }

  return (
    <View style={styles.row}>
      <Text style={styles.text}>Serveur joignable</Text>
      <Text style={styles.detail}>Version {health.data.version}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignItems: 'center',
    gap: 8,
  },
  text: {
    fontSize: 17,
    fontWeight: '600',
  },
  detail: {
    fontSize: 15,
    opacity: 0.7,
  },
});
