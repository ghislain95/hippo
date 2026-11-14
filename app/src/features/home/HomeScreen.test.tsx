import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';
import * as ReactNative from 'react-native';
import { StyleSheet } from 'react-native';

import { createApiClient } from '@/api/client';
import tokens from '@/ui/theme/tokens.json';

import { HomeScreen } from './HomeScreen';

// The appearance iOS reports, read by useColorScheme (design D11).
let mockColorScheme: 'light' | 'dark' | null = 'light';

const TEST_TOKEN = 'test-token-0123456789abcdef012345';
const CAPTURE_EXPLANATION =
  "Partage un lien, un texte ou une image depuis n'importe quelle appli. Hippo le range dans la nuit.";
const LIBRARY_NOTICE = 'La bibliothèque arrive avec la prochaine version.';

function fakeServer(status: number, seen: Request[] = []) {
  return createApiClient({
    baseUrl: 'http://hippo.test',
    token: TEST_TOKEN,
    fetch: async (input) => {
      const request = input instanceof Request ? input : new Request(input);
      seen.push(request);
      const body =
        status === 200
          ? { status: 'ok', version: 'dev' }
          : { type: 'about:blank', title: 'Service unavailable', status };
      return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': status < 400 ? 'application/json' : 'application/problem+json' },
      });
    },
  });
}

async function renderWithQuery(ui: ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return await render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

async function showHome(status = 200, seen: Request[] = []) {
  await renderWithQuery(<HomeScreen client={fakeServer(status, seen)} />);
  await screen.findByText(status === 200 ? 'Serveur joignable' : 'Serveur injoignable');
}

beforeEach(() => {
  mockColorScheme = 'light';
  jest.spyOn(ReactNative, 'useColorScheme').mockImplementation(() => mockColorScheme);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('Every call of the app carries the build-time token', () => {
  test('Health check sent to the configured server', async () => {
    const seen: Request[] = [];
    await showHome(200, seen);

    expect(seen).toHaveLength(1);
    expect(seen[0]?.url).toBe('http://hippo.test/healthz');
    expect(seen[0]?.headers.get('Authorization')).toBe(`Bearer ${TEST_TOKEN}`);
  });
});

describe("The home screen explains how to capture and shows the server's state", () => {
  test('Server reachable', async () => {
    await showHome(200);

    expect(screen.getByRole('header', { name: 'Hippo' })).toBeVisible();
    expect(screen.getByText('Serveur joignable')).toBeVisible();
    expect(screen.getByText(CAPTURE_EXPLANATION)).toBeVisible();
    expect(screen.getByText(LIBRARY_NOTICE)).toBeVisible();
    expect(screen.queryByText('Serveur injoignable')).toBeNull();
  });

  test('Server unreachable', async () => {
    await showHome(503);

    expect(screen.getByRole('alert')).toHaveTextContent('Serveur injoignable');
    expect(screen.getByText('Serveur injoignable')).toBeVisible();
    expect(screen.getByText(CAPTURE_EXPLANATION)).toBeVisible();
    expect(screen.queryByText('Serveur joignable')).toBeNull();
  });
});

describe('The theme follows the iOS appearance', () => {
  test('Dark appearance', async () => {
    mockColorScheme = 'dark';
    await showHome();

    expect(screen.getByTestId('home-screen')).toHaveStyle({
      backgroundColor: tokens.colors.dark.background,
    });
    expect(screen.getByText('Hippo')).toHaveStyle({ color: tokens.colors.dark.text });
    expect(screen.getByText(CAPTURE_EXPLANATION)).toHaveStyle({ color: tokens.colors.dark.text });
    expect(screen.getByText(LIBRARY_NOTICE)).toHaveStyle({ color: tokens.colors.dark.textMuted });
  });

  test('Light appearance', async () => {
    mockColorScheme = 'light';
    await showHome();

    expect(screen.getByTestId('home-screen')).toHaveStyle({
      backgroundColor: tokens.colors.light.background,
    });
    expect(screen.getByText('Hippo')).toHaveStyle({ color: tokens.colors.light.text });
    expect(screen.getByText(LIBRARY_NOTICE)).toHaveStyle({ color: tokens.colors.light.textMuted });
  });

  test('No appearance reported', async () => {
    mockColorScheme = null;
    await showHome();

    expect(screen.getByTestId('home-screen')).toHaveStyle({
      backgroundColor: tokens.colors.light.background,
    });
    expect(screen.getByText('Hippo')).toHaveStyle({ color: tokens.colors.light.text });
  });
});

describe('Text sizes follow the iOS text size setting', () => {
  test('Texts of the home screen scale', async () => {
    await showHome();

    const sizes: number[] = Object.values(tokens.textSizes);
    const texts = screen.root?.queryAll((node) => node.type === 'Text') ?? [];
    expect(texts.length).toBeGreaterThanOrEqual(4);
    for (const text of texts) {
      const style = StyleSheet.flatten(text.props.style) ?? {};
      expect(sizes).toContain(style.fontSize);
      expect(text.props.allowFontScaling).not.toBe(false);
      expect(text.props.maxFontSizeMultiplier).toBeUndefined();
    }
  });
});
