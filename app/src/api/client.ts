import createClient, { type Client } from 'openapi-fetch';

import { readConfig } from '@/config';

import type { paths } from './schema.gen';

export type ApiClient = Client<paths>;

type ClientOptions = {
  baseUrl: string;
  token: string;
  fetch?: typeof globalThis.fetch;
};

/**
 * Creates the typed API client. Every HTTP call of the app goes through it
 * (docs/dev/conventions-app.md): the types come from api/openapi.yaml.
 */
export function createApiClient({ baseUrl, token, fetch }: ClientOptions): ApiClient {
  const client = createClient<paths>({ baseUrl, fetch });
  client.use({
    onRequest({ request }) {
      if (token) {
        request.headers.set('Authorization', `Bearer ${token}`);
      }
      return request;
    },
  });
  return client;
}

const config = readConfig();

/** The client of the running app, configured at build time. */
export const apiClient = createApiClient({ baseUrl: config.apiUrl, token: config.apiToken });
