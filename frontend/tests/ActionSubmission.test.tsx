import { beforeEach, expect, it } from '@jest/globals';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ActionSubmission } from '../src/features/operations/ActionSubmission';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';
import type { Schema } from '../src/features/connections/api';
const resource = {
  id: scope.userId,
  provider: 'hetzner',
  account_identity: 'project',
  available_actions: [{ action: 'compute.server.power_on', available: true }],
} as Schema['ResourceDetail'];
const review = {
  id: scope.workspace.id,
  resource_id: resource.id,
  action: 'compute.server.power_on',
  target_name: 'server',
  high_impact: false,
  expires_at: new Date(Date.now() + 300_000).toISOString(),
};
const calls = () =>
  fetchMock.mock.calls.filter(([url]) =>
    requestURL(url).endsWith('/operations'),
  );
const showForm = () =>
  show(
    <MemoryRouter>
      <ActionSubmission scope={scope} resource={resource} />
    </MemoryRouter>,
  );
async function confirm() {
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByRole('dialog');
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
}
beforeEach(() => {
  setup();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
it('retains one key after a lost response and recovers the original operation', async () => {
  let accepted = false;
  fetchMock.mockImplementation((url) => {
    if (requestURL(url).endsWith('action-reviews'))
      return Promise.resolve(response(200, review));
    if (!accepted) {
      accepted = true;
      return Promise.reject(new TypeError('lost response'));
    }
    return Promise.resolve(response(202, { id: resource.id, state: 'queued' }));
  });
  showForm();
  await confirm();
  await screen.findByText(/Acceptance is unresolved/);
  expect(calls()).toHaveLength(1);
  expect(screen.queryByText('Review action')).not.toBeInTheDocument();
  fireEvent.click(screen.getByText('Recover original confirmation'));
  await screen.findByRole('link', { name: `Follow operation ${resource.id}` });
  expect(calls()).toHaveLength(2);
  const [first, second] = calls();
  expect(first![1]!.headers).toHaveProperty('Idempotency-Key');
  expect(
    (first![1]!.headers as Record<string, string>)['Idempotency-Key'],
  ).toBe((second![1]!.headers as Record<string, string>)['Idempotency-Key']);
  expect(first![1]!.body).toBe(second![1]!.body);
  expect(JSON.parse(first![1]!.body as string)).toEqual({
    review_id: review.id,
    confirmed: true,
    typed_target: '',
  });
});
it('blocks concurrent confirmation and aborts it when the scope unmounts', async () => {
  fetchMock.mockImplementation((url) =>
    requestURL(url).endsWith('action-reviews')
      ? Promise.resolve(response(200, review))
      : new Promise(() => {}),
  );
  const view = showForm();
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByRole('dialog');
  const button = screen.getByRole('button', { name: 'Confirm' });
  fireEvent.click(button);
  fireEvent.click(button);
  expect(calls()).toHaveLength(1);
  expect(screen.getByText('Recover original confirmation')).toBeDisabled();
  const signal = calls()[0]![1]!.signal!;
  view.unmount();
  expect(signal.aborted).toBe(true);
  showForm();
  await waitFor(() => expect(screen.getByText('Review action')).toBeVisible());
  expect(
    screen.queryByText('Recover original confirmation'),
  ).not.toBeInTheDocument();
});
