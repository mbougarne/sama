import { beforeEach, expect, it } from '@jest/globals';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ActivityPage from '../src/features/activity/ActivityPage';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';
beforeEach(setup);
const event = {
  id: scope.userId,
  actor_kind: 'user',
  actor_id: scope.userId,
  action: 'operation.accepted',
  created_at: '2026-10-01T00:00:00Z',
  metadata: { resource_id: scope.userId, operation_id: scope.workspace.id },
};
it('uses keyboard filters, backend timestamps, scoped links and cursor paging', async () => {
  fetchMock.mockResolvedValue(
    response(200, { data: [event], next_cursor: 'history-next' }),
  );
  show(
    <MemoryRouter>
      <ActivityPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByRole('heading', { name: 'operation.accepted' });
  expect(screen.getByText(/2026-10-01T00:00:00Z/)).toBeVisible();
  expect(
    screen.getByRole('link', { name: `Resource ${scope.userId}` }),
  ).toHaveAttribute(
    'href',
    `/workspaces/${scope.workspace.id}/resources/${scope.userId}`,
  );
  const user = userEvent.setup();
  await user.click(screen.getByLabelText('action'));
  await user.keyboard('connection.disabled{Enter}');
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).toContain(
    'action=connection.disabled',
  );
  await user.click(screen.getByRole('button', { name: 'Next activity page' }));
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).toContain(
    'cursor=history-next',
  );
});
it('shows empty and failure states separately and clears revoked history from cache', async () => {
  fetchMock
    .mockResolvedValueOnce(response(200, { data: [], next_cursor: null }))
    .mockResolvedValue(response(403, { code: 'permission_denied' }));
  const view = show(
    <MemoryRouter>
      <ActivityPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByText('No activity matches these filters.');
  await userEvent.click(
    screen.getByRole('button', { name: 'Reload activity' }),
  );
  await screen.findByRole('alert');
  expect(screen.queryByText(/No activity matches/)).not.toBeInTheDocument();
  expect(view.client.getQueryCache().getAll()).toHaveLength(0);
});
