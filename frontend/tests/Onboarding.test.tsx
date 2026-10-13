import { beforeEach, expect, it } from '@jest/globals';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import Onboarding from '../src/features/connections/Onboarding';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
beforeEach(setup);
const preview = {
  account_identity: 'preview-account',
  verified_at: '2026-10-13T00:00:00Z',
  capabilities: [
    { action: 'compute.server.read', available: true, reason: 'available' },
  ],
};
it('clears submitted secrets, invalidates preview on edits and saves the current credential with authoritative facts', async () => {
  fetchMock.mockResolvedValueOnce(response(200, preview)).mockResolvedValueOnce(
    response(201, {
      ...preview,
      account_identity: 'save-account',
      id: scope.userId,
      sync_id: scope.workspace.id,
    }),
  );
  const view = show(<Onboarding {...scope} />);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText('Connection label'), 'Compute');
  await user.type(screen.getByLabelText('token'), 'synthetic-old');
  await user.click(screen.getByRole('button', { name: 'Test connection' }));
  await screen.findByText(/Preview · verified account preview-account/);
  expect(screen.getByLabelText('token')).toHaveValue('');
  await user.type(screen.getByLabelText('token'), 'synthetic-new');
  expect(screen.queryByText(/preview-account/)).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Save connection' }));
  await screen.findByText(/Saved · verified account save-account/);
  expect(JSON.parse(fetchMock.mock.calls[1]![1]!.body as string)).toEqual({
    family: 'hetzner.cloud',
    label: 'Compute',
    credential: { type: 'bearer_v1', token: 'synthetic-new' },
  });
  expect(screen.getByLabelText('token')).toHaveValue('');
  expect(JSON.stringify(view.client.getQueryCache().getAll())).not.toContain(
    'synthetic-',
  );
});
it('keeps safe fields after failure and clears typed credentials on family changes and unmount', async () => {
  fetchMock.mockResolvedValue(
    response(422, { code: 'unsupported_capability' }),
  );
  const view = show(<Onboarding {...scope} />);
  await userEvent.type(screen.getByLabelText('Connection label'), 'Compute');
  await userEvent.type(screen.getByLabelText('token'), 'synthetic-secret');
  await userEvent.click(
    screen.getByRole('button', { name: 'Test connection' }),
  );
  await screen.findByRole('alert');
  expect(screen.getByLabelText('Connection label')).toHaveValue('Compute');
  expect(screen.getByLabelText('token')).toHaveValue('');
  expect(screen.queryByText(/verified account/)).not.toBeInTheDocument();
  await userEvent.selectOptions(
    screen.getByLabelText('Provider and API family'),
    'contabo.compute',
  );
  expect(screen.getByLabelText('client secret')).toHaveValue('');
  view.unmount();
  show(<Onboarding {...scope} />);
  expect(screen.getByLabelText('token')).toHaveValue('');
});
