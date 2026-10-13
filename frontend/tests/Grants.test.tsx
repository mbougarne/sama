import { requestURL } from './settings-fixture';
import { beforeEach, expect, it } from '@jest/globals';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Grants } from '../src/features/connections/Grants';
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
