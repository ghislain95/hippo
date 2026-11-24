import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack, ThemeProvider } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useMemo, useState } from 'react';

import { navigationThemeFor } from '@/ui/theme/theme';
import { useTheme } from '@/ui/theme/useTheme';

export default function RootLayout() {
  const [queryClient] = useState(() => new QueryClient());
  const theme = useTheme();
  const navigationTheme = useMemo(() => navigationThemeFor(theme), [theme]);

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider value={navigationTheme}>
        <Stack screenOptions={{ title: 'Hippo' }}>
          <Stack.Screen name="index" options={{ headerShown: false }} />
        </Stack>
        <StatusBar style="auto" />
      </ThemeProvider>
    </QueryClientProvider>
  );
}
