import { beforeEach, expect, it, jest } from '@jest/globals';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { OperationControls } from '../src/features/operations/OperationControls';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';
import type { Schema } from '../src/features/connections/api';
const operation = {
  id: scope.userId,
  connection_id: scope.workspace.id,
  resource_id: scope.userId,
  state: 'queued',
  target_name: '<Server>',
  reservation_released: false,
} as Schema['OperationView'];
const reload = jest.fn<() => Promise<unknown>>();
const mutations = () =>
  fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST');
beforeEach(() => {
  setup();
  reload.mockReset();
  reload.mockResolvedValue(undefined);
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
  fetchMock.mockImplementation((_url, options) =>
    Promise.resolve(
      options?.method === 'POST'
        ? response(200, { id: operation.id, state: 'cancelled' })
        : response(200, { actions: ['read', 'operate', 'high_impact'] }),
    ),
  );
});
it('allows queued cancellation only and blocks controls when dispatch wins the race', async () => {
  fetchMock.mockImplementation((_url, options) =>
    Promise.resolve(
      options?.method === 'POST'
        ? response(409, { code: 'operation_state_conflict' })
        : response(200, { actions: ['read', 'operate'] }),
    ),
  );
  const view = show(
    <OperationControls
      scope={scope}
      row={operation}
      reload={reload}
      stale={false}
    />,
  );
  const button = screen.getByText('Cancel queued operation');
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await screen.findByText(/Controls are blocked/);
  expect(mutations()).toHaveLength(1);
  expect(reload).toHaveBeenCalledTimes(1);
  expect(button).toBeDisabled();
  view.rerender(
    <OperationControls
      scope={scope}
      row={{ ...operation, state: 'dispatching' }}
      reload={reload}
      stale={false}
    />,
  );
  expect(screen.queryByText('Cancel queued operation')).not.toBeInTheDocument();
  expect(screen.getByText(/Cannot cancel through Sama/)).toBeVisible();
});
it('rechecks unknown outcomes only through the observation endpoint', async () => {
  show(
    <OperationControls
      scope={scope}
      row={{ ...operation, state: 'unknown' }}
      reload={reload}
      stale={false}
    />,
  );
  const button = screen.getByText('Recheck unknown outcome');
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  fireEvent.click(button);
  await waitFor(() => expect(reload).toHaveBeenCalled());
  expect(mutations()).toHaveLength(1);
  expect(requestURL(mutations()[0]![0])).toContain('/reconciliations');
  expect(mutations()[0]![1]!.body).toBe('{}');
});
it('requires owner, high-impact grant, exact target and risk confirmation for release', async () => {
  const row = { ...operation, state: 'unknown' as const };
  const view = show(
    <OperationControls scope={scope} row={row} reload={reload} stale={false} />,
  );
  const button = await screen.findByText('Review reservation release');
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
    /outcome remains unknown/,
  );
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  fireEvent.change(screen.getByRole('textbox'), {
    target: { value: '<Server>' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(reload).toHaveBeenCalled());
  expect(requestURL(mutations()[0]![0])).toContain('/reservation-release');
  expect(JSON.parse(mutations()[0]![1]!.body as string)).toEqual({
    residual_risk_acknowledged: true,
    typed_target: '<Server>',
  });
  view.rerender(
    <OperationControls
      scope={scope}
      row={{ ...row, reservation_released: true }}
      reload={reload}
      stale={false}
    />,
  );
  expect(
    screen.queryByText('Review reservation release'),
  ).not.toBeInTheDocument();
  view.rerender(
    <OperationControls
      scope={{ ...scope, workspace: { ...scope.workspace, role: 'admin' } }}
      row={row}
      reload={reload}
      stale={false}
    />,
  );
  expect(
    screen.queryByText('Review reservation release'),
  ).not.toBeInTheDocument();
});
it('clears invalid controls after permission loss while leaving readable evidence to its owner', async () => {
  fetchMock.mockImplementation((_url, options) =>
    Promise.resolve(
      options?.method === 'POST'
        ? response(403, { code: 'permission_denied' })
        : response(200, { actions: ['read', 'operate'] }),
    ),
  );
  show(
    <OperationControls
      scope={scope}
      row={operation}
      reload={reload}
      stale={false}
    />,
  );
  const button = screen.getByText('Cancel queued operation');
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await screen.findByText(/Controls are blocked/);
  expect(button).toBeDisabled();
  expect(reload).toHaveBeenCalled();
  expect(mutations()).toHaveLength(1);
});
