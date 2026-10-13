import { requestURL } from './settings-fixture';
import { beforeEach, expect, it } from '@jest/globals';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ConnectionsPage from '../src/features/connections/ConnectionsPage';
import { ConnectionControls } from '../src/features/connections/Controls';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
beforeEach(() => {
  setup();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
const row = {
  id: scope.userId,
  workspace_id: scope.workspace.id,
  provider: 'hetzner',
  api_family: 'hetzner.cloud',
  account_identity: 'project',
  label: 'First',
  status: 'active' as const,
  region_policy: [],
  active_credential_version: 1,
  permissions_verified_at: null,
  capabilities: [
    {
      action: 'compute.server.read',
      available: false,
      reason: 'unsupported' as const,
    },
  ],
  inventory: {
    state: 'never_observed' as const,
    last_success_at: null,
    failure_code: 'provider_unavailable' as const,
  },
};
it('preserves separate rows and distinct unavailable reasons during a provider outage', async () => {
  fetchMock.mockResolvedValue(
    response(200, {
      data: [
        row,
        {
          ...row,
          id: scope.workspace.id,
          label: 'Second',
          capabilities: [
            {
              action: 'compute.server.read',
              available: false,
              reason: 'denied',
            },
          ],
        },
      ],
      next_cursor: null,
    }),
  );
  show(
    <MemoryRouter>
      <ConnectionsPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByRole('link', { name: 'Second' });
  expect(screen.getByRole('link', { name: 'First' })).toBeVisible();
  expect(screen.getByText(/Not available in this integration/)).toBeVisible();
  expect(screen.getByText(/Your connection grant/)).toBeVisible();
});
it('uses write-only rotation, clears the secret and confirms disable with its impact', async () => {
  fetchMock.mockResolvedValue(response(200, { version: 2 }));
  show(<ConnectionControls scope={scope} row={row} />);
  await userEvent.type(screen.getByLabelText('token'), 'synthetic-rotation');
  await userEvent.click(
    screen.getByRole('button', { name: 'Rotate credential' }),
  );
  expect(screen.getByLabelText('token')).toHaveValue('');
  expect(JSON.parse(fetchMock.mock.calls[0]![1]!.body as string)).toEqual({
    credential: { type: 'bearer_v1', token: 'synthetic-rotation' },
  });
  await userEvent.click(
    screen.getByRole('button', { name: 'Disable connection' }),
  );
  expect(screen.getByRole('dialog')).toHaveTextContent('does not revoke');
  await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(fetchMock).toHaveBeenCalledTimes(1);
  await userEvent.click(
    screen.getByRole('button', { name: 'Disable connection' }),
  );
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  expect(requestURL(fetchMock.mock.calls[1]![0])).toContain('/disable');
});
