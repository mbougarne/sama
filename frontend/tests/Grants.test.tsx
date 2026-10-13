import { requestURL } from './settings-fixture';
import { beforeEach, expect, it } from '@jest/globals';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Grants } from '../src/features/connections/Grants';
import Onboarding from '../src/features/connections/Onboarding';
import {
  setup,
  show,
  scope,
  actor,
  target,
  fetchMock,
  response,
} from './settings-fixture';
beforeEach(setup);
it('starts denied, renders server ceilings and refetches instead of escalating after stale denial', async () => {
  let changed = false;
  fetchMock.mockImplementation((url, options) => {
    if (options?.method === 'PUT') {
      changed = true;
      return Promise.resolve(response(403, { code: 'permission_denied' }));
    }
    if (requestURL(url).includes('/members'))
      return Promise.resolve(
        response(200, { data: [actor, target], next_cursor: null }),
      );
    return Promise.resolve(
      response(200, {
        user_id: target.user_id,
        actions: [],
        allowed_actions: changed ? [] : ['read'],
        editable: true,
      }),
    );
  });
  show(<Grants scope={scope} id={scope.userId} />);
  await screen.findByRole('option', { name: '<Member> · viewer' });
  await userEvent.selectOptions(
    screen.getByLabelText('Member'),
    target.user_id,
  );
  const read = await screen.findByRole('checkbox', { name: 'read' });
  expect(read).not.toBeChecked();
  expect(screen.getByRole('checkbox', { name: 'operate' })).toBeDisabled();
  await userEvent.click(read);
  await userEvent.click(screen.getByRole('button', { name: 'Save grants' }));
  await screen.findByRole('alert');
  await waitFor(() => expect(scope.refreshAccess).toHaveBeenCalled());
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
});

it('offers explicit grants after save without granting provider access automatically', async () => {
  let granted = false;
  fetchMock.mockImplementation((url, options) => {
    if (options?.method === 'POST')
      return Promise.resolve(
        response(201, {
          id: scope.userId,
          sync_id: scope.workspace.id,
          account_identity: 'saved-account',
          verified_at: '2026-10-13T00:00:00Z',
          capabilities: [],
        }),
      );
    if (options?.method === 'PUT') {
      granted = true;
      return Promise.resolve(response(204));
    }
    if (requestURL(url).includes('/members'))
      return Promise.resolve(
        response(200, { data: [actor], next_cursor: null }),
      );
    return Promise.resolve(
      response(200, {
        user_id: actor.user_id,
        actions: granted ? ['read'] : [],
        allowed_actions: ['read'],
        editable: true,
      }),
    );
  });
  show(<Onboarding {...scope} />);
  await userEvent.type(
    screen.getByLabelText('Connection label'),
    'New connection',
  );
  await userEvent.type(screen.getByLabelText('token'), 'synthetic-token');
  await userEvent.click(
    screen.getByRole('button', { name: 'Save connection' }),
  );
  await screen.findByRole('option', { name: 'Owner · owner' });
  expect(
    fetchMock.mock.calls.some(([, options]) => options?.method === 'PUT'),
  ).toBe(false);
  await userEvent.selectOptions(screen.getByLabelText('Member'), actor.user_id);
  const read = await screen.findByRole('checkbox', { name: 'read' });
  expect(read).not.toBeChecked();
  await userEvent.click(read);
  await userEvent.click(screen.getByRole('button', { name: 'Save grants' }));
  await waitFor(() => expect(granted).toBe(true));
});
