import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useLayoutEffect, useState, type PropsWithChildren } from 'react';

type Filters = Record<
  string,
  string | number | boolean | readonly string[] | undefined
>;
export function scopedKey(
  userId: string,
  workspaceId: string,
  feature: string,
  filters: Filters = {},
) {
  const normalized = Object.fromEntries(
    Object.entries(filters)
      .filter(([, value]) => value !== undefined)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, value]) => [
        key,
        Array.isArray(value) ? [...new Set(value)].sort() : value,
      ]),
  );
  return [
    'user',
    userId,
    'workspace',
    workspaceId,
    feature,
    normalized,
  ] as const;
}
function OwnedClient({ children }: PropsWithChildren) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 30_000, gcTime: 300_000, retry: false },
          mutations: { retry: false },
        },
      }),
  );
  useLayoutEffect(
    () => () => {
      void client.cancelQueries();
      client.clear();
    },
    [client],
  );
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
// A changed scope creates a fresh client and remounts feature-local form state.
// Late results remain attached to the old, cancelled client, never the new view.
export function QueryScope({
  userId,
  workspaceId = '',
  children,
}: PropsWithChildren<{ userId: string; workspaceId?: string }>) {
  return <OwnedClient key={`${userId}:${workspaceId}`}>{children}</OwnedClient>;
}
