import { beforeEach, expect, it } from '@jest/globals';
import { act, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import OverviewPage from '../src/features/overview/OverviewPage';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';
beforeEach(setup);
const value = {
  connection_count: 1,
  resource_count: 12,
  pending_operations: 2,
  unknown_operations: 1,
  stale_connections: 1,
  connections_truncated: false,
  connections: [
    {
      id: scope.userId,
      label: 'Stale project',
      provider: 'hetzner',
      account_identity: 'project',
      status: 'active',
      permissions_verified_at: null,
      inventory: {
        state: 'stale',
        last_success_at: '2026-10-01T00:00:00Z',
        failure_code: 'provider_unavailable',
      },
    },
  ],
};
it('loads local observations once without rendering loading as zero or scheduling refresh', async () => {
  let resolve!: (value: Response) => void;
  fetchMock.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  show(
    <MemoryRouter>
      <OverviewPage {...scope} />
    </MemoryRouter>,
  );
  expect(screen.getByRole('status')).toHaveTextContent(
    'Loading workspace observations',
  );
  expect(screen.queryByText('0')).not.toBeInTheDocument();
  await act(async () => {
    resolve(response(200, value));
    await Promise.resolve();
  });
  await screen.findByText('12');
  expect(screen.getByRole('link', { name: 'Stale project' })).toBeVisible();
  expect(
    screen.getByText(/Last successful provider observation: 2026-10-01/),
  ).toBeVisible();
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(requestURL(fetchMock.mock.calls[0]![0])).toContain('/overview');
  expect(fetchMock.mock.calls[0]![1]?.method).toBe('GET');
});
it('clears cached totals and shows failure after permission loss', async () => {
  fetchMock
    .mockResolvedValueOnce(response(200, value))
    .mockResolvedValue(response(403, { code: 'permission_denied' }));
  const view = show(
    <MemoryRouter>
      <OverviewPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByText('12');
  await act(async () => {
    await view.client.invalidateQueries();
  });
  await screen.findByRole('alert');
  expect(screen.queryByText('12')).not.toBeInTheDocument();
  expect(screen.queryByText('0')).not.toBeInTheDocument();
  expect(view.client.getQueryCache().getAll()).toHaveLength(0);
});
