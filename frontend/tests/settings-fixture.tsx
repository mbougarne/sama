import { jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
export const actor = {
  user_id: '01900000-0000-7000-8000-000000000001',
  display_name: 'Owner',
  role: 'owner' as const,
  version: 2,
};
export const target = {
  user_id: '01900000-0000-7000-8000-000000000002',
  display_name: '<Member>',
  role: 'viewer' as const,
  version: 3,
};
export const scope = {
  userId: actor.user_id,
  workspace: {
    id: '01900000-0000-7000-8000-000000000003',
    name: 'First',
    role: 'owner' as const,
  },
  refreshAccess: jest.fn(),
};
export const fetchMock = jest.fn<typeof fetch>();
export const response = (status: number, body: unknown = {}): Response =>
  ({
    status,
    ok: status < 300,
    headers: { get: () => 'application/problem+json' },
    json: () => Promise.resolve(body),
  }) as unknown as Response;
export function setup() {
  fetchMock.mockReset();
  scope.refreshAccess.mockReset();
  Object.defineProperty(globalThis, 'fetch', {
    value: fetchMock,
    configurable: true,
  });
}
export function show(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>{children}</QueryClientProvider>,
  );
  return { ...view, client };
}
