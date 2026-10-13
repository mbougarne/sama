import { requestURL } from './settings-fixture';
import { beforeEach, afterEach, expect, it, jest } from '@jest/globals';
import { act, fireEvent, screen } from '@testing-library/react';
import { Refresh } from '../src/features/resources/Refresh';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
let visible = true,
  online = true,
  status = 'running';
const tick = (ms: number) =>
  act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
const progressCalls = () =>
  fetchMock.mock.calls.filter(([url]) => requestURL(url).includes('/syncs/'))
    .length;
beforeEach(() => {
  setup();
  jest.useFakeTimers();
  visible = true;
  online = true;
  status = 'running';
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => (visible ? 'visible' : 'hidden'),
  });
  Object.defineProperty(navigator, 'onLine', {
    configurable: true,
    get: () => online,
  });
  fetchMock.mockImplementation((url, options) =>
    Promise.resolve(
      response(
        options?.method === 'POST' ? 202 : 200,
        requestURL(url).includes('/grants')
          ? { actions: ['read', 'refresh'] }
          : options?.method === 'POST'
            ? { sync_id: scope.userId }
            : {
                id: scope.userId,
                connection_id: scope.userId,
                status,
                page_count: 1,
                started_at: null,
                completed_at: null,
                failure_code:
                  status === 'failed' ? 'provider_unavailable' : null,
                inventory: {
                  last_success_at: '2026-10-01T00:00:00Z',
                  state: 'stale',
                  failure_code: 'provider_unavailable',
                },
              },
      ),
    ),
  );
});
afterEach(() => {
  jest.useRealTimers();
});
it('polls every three seconds only while visible and online and stops on failure', async () => {
  show(<Refresh scope={scope} connectionId={scope.userId} />);
  await tick(50);
  expect(progressCalls()).toBe(0);
  fireEvent.click(
    screen.getByRole('button', { name: 'Refresh connection inventory' }),
  );
  await tick(50);
  expect(progressCalls()).toBe(1);
  await tick(3000);
  expect(progressCalls()).toBe(2);
  visible = false;
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await tick(6000);
  expect(progressCalls()).toBe(2);
  online = false;
  visible = true;
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new Event('offline'));
  });
  await tick(6000);
  expect(progressCalls()).toBe(2);
  online = true;
  status = 'failed';
  act(() => {
    window.dispatchEvent(new Event('online'));
  });
  await tick(3050);
  expect(progressCalls()).toBe(3);
  expect(screen.getByRole('status')).toHaveTextContent('Sync: failed');
  expect(
    screen.getByText(/Last successful provider observation: 2026-10-01/),
  ).toBeVisible();
  await tick(6000);
  expect(progressCalls()).toBe(3);
});
it('ends polling when its view unmounts', async () => {
  const view = show(<Refresh scope={scope} connectionId={scope.userId} />);
  await tick(50);
  fireEvent.click(
    screen.getByRole('button', { name: 'Refresh connection inventory' }),
  );
  await tick(50);
  const count = progressCalls();
  view.unmount();
  await tick(6000);
  expect(progressCalls()).toBe(count);
});
