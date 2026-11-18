import type { ApiClient } from '@/api/client';
import { ServerStatus } from '@/features/health/ServerStatus';
import { Screen } from '@/ui/Screen';
import { ThemedText } from '@/ui/ThemedText';

type Props = {
  client?: ApiClient;
};

/**
 * Accueil, screen 1 of the change's mockups: a temporary home screen that tells how to capture
 * and shows the server's state, until the library (E5-03) replaces it.
 */
export function HomeScreen({ client }: Props) {
  return (
    <Screen testID="home-screen">
      <ThemedText size="title" weight="bold" accessibilityRole="header">
        Hippo
      </ThemedText>
      <ServerStatus client={client} />
      <ThemedText>
        Partage un lien, un texte ou une image depuis n&apos;importe quelle appli. Hippo le range
        dans la nuit.
      </ThemedText>
      <ThemedText size="caption" color="textMuted">
        La bibliothèque arrive avec la prochaine version.
      </ThemedText>
    </Screen>
  );
}
