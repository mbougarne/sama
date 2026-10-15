import { beforeEach, expect, it, jest } from '@jest/globals';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { ActionReview } from '../src/features/operations/ActionReview';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
import type { Schema } from '../src/features/connections/api';
export const resource = {
  id: scope.userId,
  connection_id: scope.userId,
  provider: 'hetzner',
  account_identity: 'project',
  region: 'fsn1',
  available_actions: ['power_on', 'power_off'].map((action) => ({
    action: `compute.server.${action}`,
    available: true,
    reason: 'available',
  })),
} as Schema['ResourceDetail'];
export const review = {
  id: '01900000-0000-7000-8000-000000000004',
  resource_id: resource.id,
  action: 'compute.server.power_on',
  target_name: '<Server>',
  high_impact: true,
  expires_at: new Date(Date.now() + 300_000).toISOString(),
};
beforeEach(() => {
  setup();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
  fetchMock.mockResolvedValue(response(200, review));
});
it('uses server-held review identity, visible risk, and typed confirmation', async () => {
  const confirm = jest.fn();
  show(<ActionReview scope={scope} resource={resource} onConfirm={confirm} />);
  expect(screen.getByText(/Price: unknown/)).toBeVisible();
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByRole('dialog');
  expect(screen.getByText(/hetzner · project · fsn1/)).toBeVisible();
  expect(JSON.parse(fetchMock.mock.calls[0]![1]!.body as string)).toEqual({
    action: review.action,
    parameters: {},
  });
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  fireEvent.change(screen.getByRole('textbox'), {
    target: { value: '<Server>' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  expect(confirm).toHaveBeenCalledWith(review, '<Server>');
  fireEvent.change(screen.getByRole('combobox'), {
    target: { value: 'compute.server.power_off' },
  });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
it('discards late review results when the action changes', async () => {
  let finish!: (value: Response) => void;
  fetchMock.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  show(<ActionReview scope={scope} resource={resource} />);
  fireEvent.click(screen.getByText('Review action'));
  fireEvent.change(screen.getByRole('combobox'), {
    target: { value: 'compute.server.power_off' },
  });
  finish(response(200, review));
  await waitFor(() => expect(screen.getByText('Review action')).toBeEnabled());
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
it('rejects an expired review and handles recent authentication without retry', async () => {
  fetchMock.mockResolvedValueOnce(
    response(200, { ...review, expires_at: '2000-01-01T00:00:00Z' }),
  );
  show(
    <ActionReview scope={scope} resource={resource} onConfirm={jest.fn()} />,
  );
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByText(/Review expired/);
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  fetchMock.mockResolvedValue(
    response(403, { code: 'recent_authentication_required' }),
  );
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByText(/Sign out and sign in again/);
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(scope.refreshAccess).not.toHaveBeenCalled();
});
