import Constants from 'expo-constants';
import { useState } from 'react';
import { Button, ScrollView, StyleSheet, Text, View } from 'react-native';

// Network tab (design D5): which server the build was configured for, and whether the app
// reaches it (on 4G through Tailscale for E1-03).
const { serverUrl, token } = (Constants.expoConfig?.extra ?? {}) as { serverUrl: string; token: string };

type Result = { ok: boolean; text: string; ms: number };

export default function NetworkScreen() {
  const [result, setResult] = useState<Result | null>(null);
  const [busy, setBusy] = useState(false);

  async function ping() {
    setBusy(true);
    const start = Date.now();
    try {
      const response = await fetch(`${serverUrl}/ping`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      setResult({ ok: response.ok, text: `HTTP ${response.status}`, ms: Date.now() - start });
    } catch (error) {
      setResult({ ok: false, text: String(error), ms: Date.now() - start });
    } finally {
      setBusy(false);
    }
  }

  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <Text style={styles.label}>Serveur injecté au build</Text>
      <Text selectable style={styles.value}>
        {serverUrl}
      </Text>
      <Button title={busy ? 'Appel en cours…' : 'Appeler /ping'} onPress={ping} disabled={busy} />
      {result && (
        <View style={styles.result}>
          <Text style={[styles.value, { color: result.ok ? '#2fa36b' : '#d9480f' }]}>
            {result.ok ? 'Joignable' : 'Injoignable'} · {result.text}
          </Text>
          <Text style={styles.label}>{result.ms} ms</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: { padding: 20, gap: 12 },
  label: { fontSize: 13, opacity: 0.6 },
  value: { fontSize: 16, fontWeight: '500' },
  result: { gap: 4, marginTop: 8 },
});
