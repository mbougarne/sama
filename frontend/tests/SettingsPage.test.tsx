import { beforeEach, expect, it } from '@jest/globals';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SettingsPage from '../src/features/settings/SettingsPage';
import { QueryScope } from '../src/app/QueryScope';
import {
  actor,
  target,
  scope,
  setup,
  show,
  fetchMock,
  response,
} from './settings-fixture';

const settings = {
  name: 'First',
  policy_version: 7,
  queue_limit: 50,
  audit_retention_days: 180,
};
beforeEach(() => {
  setup();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
  fetchMock.mockImplementation((url, options) => {
    if (options?.method === 'GET')
      return Promise.resolve(
        response(
          200,
          (url as URL).pathname.endsWith('/settings')
            ? settings
            : { data: [actor, target], next_cursor: null },
        ),
      );
    return Promise.resolve(response(204));
  });
});
it('loads server settings, submits bounded values with policy version and preserves conflict edits', async () => {
  show(<SettingsPage {...scope} />);
  const name = await screen.findByLabelText('Workspace name');
  expect(name).toHaveValue('First');
  expect(screen.getByLabelText('Queue limit')).toHaveAttribute('max', '1000');
  await userEvent.clear(name);
  await userEvent.type(name, 'Edited');
  fetchMock.mockResolvedValueOnce(response(409));
  await userEvent.click(screen.getByRole('button', { name: 'Save settings' }));
  await screen.findByRole('alert');
  expect(name).toHaveValue('Edited');
  expect(fetchMock.mock.calls.at(-1)![1]?.body).toBe(
    JSON.stringify({ ...settings, name: 'Edited' }),
  );
  expect(scope.refreshAccess).not.toHaveBeenCalled();
});
it('reviews an exact target and impact, cancels safely and handles transfer conflicts', async () => {
  show(<SettingsPage {...scope} />);
  await screen.findByLabelText('New owner');
  await userEvent.selectOptions(
    screen.getByLabelText('New owner'),
    target.user_id,
  );
  await userEvent.click(
    screen.getByLabelText('Make me an admin after transfer'),
  );
  await userEvent.click(
    screen.getByRole('button', { name: 'Review transfer' }),
  );
  expect(screen.getByRole('dialog')).toHaveTextContent(
    'You will become an admin',
  );
  await userEvent.keyboard('{Escape}');
  expect(
    fetchMock.mock.calls.filter((call) => call[1]?.method === 'POST'),
  ).toHaveLength(0);
  expect(screen.getByRole('button', { name: 'Review transfer' })).toHaveFocus();
  await userEvent.click(
    screen.getByRole('button', { name: 'Review transfer' }),
  );
  await userEvent.type(
    screen.getByLabelText(`Type ${target.user_id} to confirm`),
    target.user_id,
  );
  fetchMock.mockResolvedValueOnce(response(409));
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await screen.findByRole('alert');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.getByLabelText('New owner')).toHaveValue(target.user_id);
  const mutation = fetchMock.mock.calls.find(
    (call) => call[1]?.method === 'POST',
  );
  expect(mutation![1]?.body).toBe(
    JSON.stringify({
      user_id: target.user_id,
      actor_version: 2,
      target_version: 3,
      demote: true,
    }),
  );
});
it('drops drafts and owner controls when workspace scope changes', async () => {
  const view = show(
    <QueryScope userId={scope.userId} workspaceId={scope.workspace.id}>
      <SettingsPage {...scope} />
    </QueryScope>,
  );
  await userEvent.type(
    await screen.findByLabelText('Workspace name'),
    ' unsaved',
  );
  view.rerender(
    <QueryScope userId={scope.userId} workspaceId="another">
      <SettingsPage
        {...scope}
        workspace={{ ...scope.workspace, id: 'another', role: 'viewer' }}
      />
    </QueryScope>,
  );
  await waitFor(() =>
    expect(screen.queryByLabelText('Workspace name')).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole('button', { name: 'Review transfer' }),
  ).not.toBeInTheDocument();
  expect(document.body).not.toHaveTextContent('unsaved');
});

it('refreshes authority after a confirmed settings save', async () => {
  show(<SettingsPage {...scope} />);
  await screen.findByLabelText('Workspace name');
  await userEvent.click(screen.getByRole('button', { name: 'Save settings' }));
  await waitFor(() => expect(scope.refreshAccess).toHaveBeenCalledTimes(1));
});
