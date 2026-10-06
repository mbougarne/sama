import { beforeEach, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SessionGate } from '../src/features/identity/SessionGate';

const fetchMock = jest.fn<typeof fetch>();
const user = {
  id: '01900000-0000-7000-8000-000000000001',
  display_name: '<script>Owner</script>',
};
function response(status: number, body: unknown = user): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: () => null },
    json: () => Promise.resolve(body),
  } as unknown as Response;
}
beforeEach(() => {
  Object.defineProperty(globalThis, 'fetch', {
    value: fetchMock,
    configurable: true,
  });
  fetchMock.mockReset();
});
function show() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <SessionGate>{() => <p>Private workspace</p>}</SessionGate>
    </QueryClientProvider>,
  );
  return client;
}
it('hides private views while loading and exposes a keyboard sign-in link on 401', async () => {
  let resolve!: (value: Response) => void;
  fetchMock.mockImplementation(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  show();
  expect(screen.getByRole('status')).toHaveTextContent('Checking your session');
  expect(screen.queryByText('Private workspace')).not.toBeInTheDocument();
  await act(async () => {
    await Promise.resolve();
    resolve(response(401));
  });
  const keyboard = userEvent.setup();
  await keyboard.tab();
  expect(screen.getByRole('link', { name: 'Sign in' })).toHaveFocus();
  expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute(
    'href',
    '/auth/login',
  );
  expect(fetchMock).toHaveBeenCalledTimes(1);
});
it('renders identity text safely and signs out through a keyboard button', async () => {
  fetchMock
    .mockResolvedValueOnce(response(200))
    .mockResolvedValueOnce(response(204));
  const client = show();
  await screen.findByText('Private workspace');
  expect(screen.getByText('<script>Owner</script>')).toBeVisible();
  client.setQueryData(['private'], 'secret');
  const keyboard = userEvent.setup();
  await keyboard.tab();
  await keyboard.keyboard('{Enter}');
  await screen.findByRole('link', { name: 'Sign in' });
  expect(screen.queryByText('Private workspace')).not.toBeInTheDocument();
  expect(client.getQueryData(['private'])).toBeUndefined();
  expect(fetchMock.mock.calls[1]![1]?.method).toBe('POST');
});
it('transitions any API 401 to session ended and removes cached private data', async () => {
  fetchMock.mockResolvedValue(response(200));
  const client = show();
  await screen.findByText('Private workspace');
  client.setQueryData(['private'], 'secret');
  await act(async () => {
    await Promise.resolve();
    window.dispatchEvent(new Event('sama:unauthenticated'));
  });
  expect(screen.getByRole('heading', { name: 'Session ended' })).toBeVisible();
  expect(screen.queryByText('Private workspace')).not.toBeInTheDocument();
  expect(client.getQueryData(['private'])).toBeUndefined();
});
it('shows access denial and does not expose workspace content', async () => {
  fetchMock.mockResolvedValue(response(403));
  show();
  await screen.findByRole('heading', { name: 'Access denied' });
  expect(screen.queryByText('Private workspace')).not.toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledTimes(1);
});
it('keeps failed logout visible without claiming the server session ended', async () => {
  fetchMock
    .mockResolvedValueOnce(response(200))
    .mockResolvedValueOnce(response(503));
  show();
  await screen.findByText('Private workspace');
  await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));
  await waitFor(() =>
    expect(screen.getByRole('alert')).toHaveTextContent(
      'could not be confirmed',
    ),
  );
  expect(screen.getByText('Private workspace')).toBeVisible();
});
