import { beforeEach, expect, it } from '@jest/globals';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Invitation } from '../src/features/settings/Invitation';
import { InvitationEntry } from '../src/features/identity/InvitationEntry';
import { scope, setup, show, fetchMock, response } from './settings-fixture';

beforeEach(setup);
it('keeps one-use issued proof outside query caches and clears it after use', async () => {
  const proof = 'fixture-proof-only';
  fetchMock.mockResolvedValue(response(201, { token: proof }));
  const view = show(<Invitation scope={scope} block={() => undefined} />);
  await userEvent.type(
    screen.getByLabelText('Identity provider subject'),
    'exact-subject',
  );
  await userEvent.click(
    screen.getByRole('button', { name: 'Create invitation' }),
  );
  expect(await screen.findByLabelText('One-use invitation')).toHaveValue(proof);
  expect(screen.getByLabelText('Identity provider subject')).toHaveValue('');
  expect(view.client.getQueryCache().getAll()).toEqual([]);
  expect(view.client.getMutationCache().getAll()).toEqual([]);
  expect(location.href).not.toContain(proof);
  await userEvent.click(
    screen.getByRole('button', { name: 'Clear invitation' }),
  );
  expect(screen.queryByLabelText('One-use invitation')).not.toBeInTheDocument();
});
it('clears login proof before submission finishes and never echoes denial details', async () => {
  fetchMock.mockResolvedValue(response(400, { title: 'SECRET' }));
  show(<InvitationEntry />);
  const proof = 'A'.repeat(43);
  await userEvent.type(screen.getByLabelText('Invitation code'), proof);
  await userEvent.click(
    screen.getByRole('button', { name: 'Continue with invitation' }),
  );
  await screen.findByRole('alert');
  expect(screen.getByLabelText('Invitation code')).toHaveValue('');
  expect(fetchMock.mock.calls[0]![1]?.body).toBe(
    JSON.stringify({ invitation: proof }),
  );
  expect((fetchMock.mock.calls[0]![0] as URL).href).not.toContain(proof);
  expect(document.body).not.toHaveTextContent('SECRET');
});
