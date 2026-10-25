import { useQuery } from '@tanstack/react-query';

import { apiClient, type ApiClient } from '@/api/client';

/** State of the server, from GET /healthz. */
export function useHealth(client: ApiClient = apiClient) {
  return useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const { data, error, response } = await client.GET('/healthz');
      if (error || !data) {
        throw new Error(`Health check failed with status ${response.status}`);
      }
      return data;
    },
    retry: false,
  });
}
