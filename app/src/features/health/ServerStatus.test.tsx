import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';

import { createApiClient } from '@/api/client';

import { ServerStatus } from './ServerStatus';

async function renderWithQuery(ui: ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return await render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

function pendingServer() {
  return createApiClient({
    baseUrl: 'http://hippo.test',
    token: 'test-token-0123456789abcdef012345',
    fetch: () => new Promise<Response>(() => {}),
  });
}

describe('ServerStatus', () => {
  it('tells that the server is being checked until it answers', async () => {
    await renderWithQuery(<ServerStatus client={pendingServer()} />);

    expect(screen.getByText('Vérification du serveur…')).toBeVisible();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
