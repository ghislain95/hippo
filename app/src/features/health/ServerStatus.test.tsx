import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';

import { createApiClient } from '@/api/client';

import { ServerStatus } from './ServerStatus';

const TEST_TOKEN = 'test-token-0123456789abcdef012345';

async function renderWithQuery(ui: ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return await render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

function fakeServer(status: number, body: unknown, seen: Request[] = []) {
  return createApiClient({
    baseUrl: 'http://hippo.test',
    token: TEST_TOKEN,
    fetch: async (input) => {
      const request = input instanceof Request ? input : new Request(input);
      seen.push(request);
      return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': status < 400 ? 'application/json' : 'application/problem+json' },
      });
    },
  });
}

describe('ServerStatus', () => {
  it('shows the server as reachable with its version', async () => {
    await renderWithQuery(
      <ServerStatus client={fakeServer(200, { status: 'ok', version: '1.2.3' })} />,
    );

    expect(await screen.findByText('Serveur joignable')).toBeVisible();
    expect(screen.getByText('Version 1.2.3')).toBeVisible();
  });

  it('shows the server as unreachable when the health check fails', async () => {
    const problem = { type: 'about:blank', title: 'Service unavailable', status: 503 };
    await renderWithQuery(<ServerStatus client={fakeServer(503, problem)} />);

    expect(await screen.findByText('Serveur injoignable')).toBeVisible();
  });

  it('sends the bearer token configured at build time', async () => {
    const seen: Request[] = [];
    await renderWithQuery(
      <ServerStatus client={fakeServer(200, { status: 'ok', version: 'dev' }, seen)} />,
    );

    await screen.findByText('Serveur joignable');
    expect(seen[0]?.url).toBe('http://hippo.test/healthz');
    expect(seen[0]?.headers.get('Authorization')).toBe(`Bearer ${TEST_TOKEN}`);
  });
});
