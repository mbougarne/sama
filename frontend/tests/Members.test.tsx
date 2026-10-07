import { beforeEach, expect, it } from '@jest/globals';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Members } from '../src/features/settings/Members';
import {
  actor,
  target,
  scope,
  setup,
  show,
  fetchMock,
  response,
} from './settings-fixture';

beforeEach(setup);
it('offers only permitted roles and uses the current version while preserving drafts on conflict', async () => {
  fetchMock.mockResolvedValue(
    response(200, { data: [target], next_cursor: null }),
  );
  show(
    <Members {...scope} workspace={{ ...scope.workspace, role: 'admin' }} />,
  );
  const heading = await screen.findByRole('heading', { name: '<Member>' });
  const form = within(heading.closest('form')!);
  expect(form.queryByRole('option', { name: 'owner' })).not.toBeInTheDocument();
  expect(form.queryByRole('option', { name: 'admin' })).not.toBeInTheDocument();
  await userEvent.selectOptions(form.getByRole('combobox'), 'operator');
  fetchMock.mockResolvedValueOnce(
    response(409, { title: 'SECRET', code: 'membership_conflict' }),
  );
  await userEvent.click(form.getByRole('button', { name: 'Save role' }));
  await screen.findByRole('alert');
  expect(form.getByRole('combobox')).toHaveValue('operator');
  expect(fetchMock.mock.calls.at(-1)![1]?.body).toBe(
    JSON.stringify({ version: 3, role: 'operator' }),
  );
  expect(document.body).not.toHaveTextContent('SECRET');
  fetchMock.mockResolvedValueOnce(response(403));
  await userEvent.click(form.getByRole('button', { name: 'Remove member' }));
  await screen.findByRole('button', { name: 'Refresh workspace access' });
  expect(
    screen.queryByRole('button', { name: 'Save role' }),
  ).not.toBeInTheDocument();
});
it('loads a later page and safely reports final-owner removal without losing the role choice', async () => {
  fetchMock
    .mockResolvedValueOnce(
      response(200, { data: [actor], next_cursor: actor.user_id }),
    )
    .mockResolvedValueOnce(
      response(200, { data: [target], next_cursor: null }),
    );
  show(<Members {...scope} />);
  await userEvent.click(
    await screen.findByRole('button', { name: 'Load more members' }),
  );
  await screen.findByRole('heading', { name: '<Member>' });
  expect((fetchMock.mock.calls[1]![0] as URL).href).toContain(
    `cursor=${actor.user_id}`,
  );
  const form = within(
    screen.getByRole('heading', { name: 'Owner' }).closest('form')!,
  );
  fetchMock.mockResolvedValueOnce(response(409));
  await userEvent.click(form.getByRole('button', { name: 'Remove member' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('last owner');
  expect(form.getByRole('combobox')).toHaveValue('owner');
});
it('never loads membership data for viewers', () => {
  show(
    <Members {...scope} workspace={{ ...scope.workspace, role: 'viewer' }} />,
  );
  expect(fetchMock).not.toHaveBeenCalled();
  expect(
    screen.queryByRole('button', { name: 'Create invitation' }),
  ).not.toBeInTheDocument();
});
