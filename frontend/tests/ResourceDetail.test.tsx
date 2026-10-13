import { requestURL } from './settings-fixture';
import { beforeEach, expect, it } from '@jest/globals';
import { screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ResourceDetail } from '../src/features/resources/ResourceDetail';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
beforeEach(setup);
const detail = {
  id: scope.userId,
  connection_id: scope.userId,
  provider: 'hetzner',
  api_family: 'hetzner.cloud',
  account_identity: 'project',
  type: 'compute.server',
  scope: 'global',
  native_id: '42',
  name: '<Server>',
  region: 'fsn1',
  status: 'unknown',
  native_status: 'future-state',
  observed_at: '2026-10-01T00:00:00Z',
  inventory: { state: 'stale', last_success_at: null, failure_code: null },
  details_version: 1,
  details: { machine_type: 'small' },
  available_actions: ['unsupported', 'denied', 'state_restricted'].map(
    (reason, index) => ({
      action: `action-${index}`,
      reason,
      available: false,
    }),
  ),
};
it('shows native identity, unknown status and distinct unavailable action reasons', async () => {
  fetchMock.mockImplementation((url) =>
    Promise.resolve(
      response(
        200,
        requestURL(url).includes('/grants') ? { actions: ['read'] } : detail,
      ),
    ),
  );
  show(
    <MemoryRouter>
      <ResourceDetail scope={scope} id={scope.userId} />
    </MemoryRouter>,
  );
  await screen.findByRole('heading', { name: '<Server>' });
  expect(screen.getByText(/hetzner · project · fsn1/)).toBeVisible();
  expect(
    screen.getByText(/unknown · Provider status: future-state/),
  ).toBeVisible();
  expect(screen.getByText(/Not available in this integration/)).toBeVisible();
  expect(screen.getByText(/Your connection grant/)).toBeVisible();
  expect(
    screen.getByText(/current resource or connection state/),
  ).toBeVisible();
});
it('clears a previously cached detail after access is revoked', async () => {
  fetchMock
    .mockResolvedValueOnce(response(200, detail))
    .mockResolvedValue(response(404, { code: 'not_found' }));
  const view = show(
    <MemoryRouter>
      <ResourceDetail scope={scope} id={scope.userId} />
    </MemoryRouter>,
  );
  await screen.findByRole('heading', { name: '<Server>' });
  await view.client.invalidateQueries();
  await screen.findByRole('alert');
  await waitFor(() =>
    expect(view.client.getQueryCache().getAll()).toHaveLength(0),
  );
  expect(
    screen.queryByRole('heading', { name: '<Server>' }),
  ).not.toBeInTheDocument();
  expect(scope.refreshAccess).toHaveBeenCalled();
});
