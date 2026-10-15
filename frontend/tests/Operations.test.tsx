import { beforeEach, afterEach, expect, it, jest } from '@jest/globals';
import { act, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import OperationsPage, {
  OperationDetail,
} from '../src/features/operations/OperationsPage';
import { scopedKey } from '../src/app/QueryScope';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';
import type { Schema } from '../src/features/connections/api';
let visible = true,
  online = true;
let state: Schema['OperationState'] = 'queued';
const tick = (ms: number) =>
  act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
const row = (): Schema['OperationView'] => ({
  id: scope.userId,
  resource_id: scope.userId,
  connection_id: scope.workspace.id,
  actor_id: scope.userId,
  action: 'compute.server.reboot',
  state,
  uncertain: state === 'unknown',
  reservation_released: false,
  target_name: '<Server>',
  native_id: '42',
  provider: 'hetzner',
  account_identity: 'project',
  region: 'fsn1',
  provider_reference: 'action-42',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:01:00Z',
  events: [
    { sequence: 1, state: 'queued', created_at: '2026-10-01T00:00:00Z' },
  ],
});
beforeEach(() => {
  setup();
  jest.useFakeTimers();
  visible = true;
  online = true;
  state = 'queued';
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => (visible ? 'visible' : 'hidden'),
  });
  Object.defineProperty(navigator, 'onLine', {
    configurable: true,
    get: () => online,
  });
  fetchMock.mockImplementation((url) =>
    Promise.resolve(
      response(
        200,
        requestURL(url).includes('?')
          ? { data: [row()], next_cursor: null }
          : row(),
      ),
    ),
  );
});
afterEach(() => {
  jest.useRealTimers();
});
it('pauses hidden/offline progress and stops automatically at unknown', async () => {
  const view = show(
    <MemoryRouter>
      <OperationDetail scope={scope} id={scope.userId} />
    </MemoryRouter>,
  );
  await tick(50);
  expect(screen.getByText('Accepted — queued before dispatch')).toBeVisible();
  expect(
    screen.getByRole('link', { name: 'Resource <Server>' }),
  ).toHaveAttribute(
    'href',
    `/workspaces/${scope.workspace.id}/resources/${scope.userId}`,
  );
  const calls = () =>
    fetchMock.mock.calls.filter(([url]) =>
      requestURL(url).includes('/operations/'),
    ).length;
  const initial = calls();
  await tick(3000);
  expect(calls()).toBe(initial + 1);
  visible = false;
  await act(() => document.dispatchEvent(new Event('visibilitychange')));
  await tick(6000);
  expect(calls()).toBe(initial + 1);
  visible = true;
  online = false;
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new Event('offline'));
  });
  await tick(6000);
  expect(calls()).toBe(initial + 1);
  online = true;
  state = 'unknown';
  await act(() => window.dispatchEvent(new Event('online')));
  await tick(3050);
  expect(calls()).toBe(initial + 2);
  expect(
    screen.getByText('Outcome unknown — evidence is insufficient'),
  ).toBeVisible();
  await tick(9000);
  expect(calls()).toBe(initial + 2);
  view.unmount();
  await tick(6000);
  expect(calls()).toBe(initial + 2);
});
it('invalidates related data only after a changed operation state', async () => {
  const view = show(
    <MemoryRouter>
      <OperationDetail scope={scope} id={scope.userId} />
    </MemoryRouter>,
  );
  const related = scopedKey(scope.userId, scope.workspace.id, 'resource', {
    id: scope.userId,
  });
  const unrelated = scopedKey(scope.userId, scope.workspace.id, 'resource', {
    id: 'other',
  });
  const otherScope = scopedKey('other-user', scope.workspace.id, 'activity');
  view.client.setQueryData(related, {});
  view.client.setQueryData(unrelated, {});
  view.client.setQueryData(otherScope, {});
  await tick(50);
  expect(view.client.getQueryState(related)?.isInvalidated).toBe(false);
  state = 'succeeded';
  await tick(3000);
  expect(
    screen.getByText('Succeeded — provider outcome verified'),
  ).toBeVisible();
  expect(view.client.getQueryState(related)?.isInvalidated).toBe(true);
  expect(view.client.getQueryState(unrelated)?.isInvalidated).toBe(false);
  expect(view.client.getQueryState(otherScope)?.isInvalidated).toBe(false);
  const count = fetchMock.mock.calls.length;
  await tick(9000);
  expect(fetchMock).toHaveBeenCalledTimes(count);
});
it('keeps bounded list cursors in the URL and distinguishes failures from success', async () => {
  state = 'failed';
  fetchMock
    .mockResolvedValueOnce(
      response(200, { data: [row()], next_cursor: 'next-page' }),
    )
    .mockResolvedValue(response(200, { data: [], next_cursor: null }));
  show(
    <MemoryRouter>
      <OperationsPage {...scope} />
    </MemoryRouter>,
  );
  await tick(50);
  expect(
    screen.getByText('Failed — definite failure established'),
  ).toBeVisible();
  fireEvent.click(screen.getByText('Next operations page'));
  await tick(50);
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).toContain(
    'cursor=next-page',
  );
  expect(screen.getByText('No operations on this page.')).toBeVisible();
  const count = fetchMock.mock.calls.length;
  await tick(6000);
  expect(fetchMock).toHaveBeenCalledTimes(count);
});
