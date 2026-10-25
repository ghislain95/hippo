import { StyleSheet, View } from 'react-native';

import { ServerStatus } from '@/features/health/ServerStatus';

/** Temporary home screen: shows the state of the server until the library lands (E5-03). */
export default function HomeScreen() {
  return (
    <View style={styles.container}>
      <ServerStatus />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
});
