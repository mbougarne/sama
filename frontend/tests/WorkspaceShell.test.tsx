import { beforeEach, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { WorkspaceShell } from '../src/features/workspaces/WorkspaceShell';

const user = {
  id: '01900000-0000-7000-8000-000000000001',
  display_name: 'Owner',
};
const first = {
  id: '01900000-0000-7000-8000-000000000002',
  name: 'First workspace',
  role: 'owner',
};
const second = {
  id: '01900000-0000-7000-8000-000000000003',
  name: 'Second workspace',
  role: 'viewer',
};
const fetchMock = jest.fn<typeof fetch>();
function page(
  data: (typeof first)[],
  next_cursor: string | null = null,
): Response {
  return {
    status: 200,
    ok: true,
    json: () => Promise.resolve({ data, next_cursor }),
  } as Response;
}
beforeEach(() => {
  Object.defineProperty(globalThis, 'fetch', {
    value: fetchMock,
    configurable: true,
  });
  fetchMock.mockReset();
});
function show(path = '/') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<WorkspaceShell user={user} />} />
          <Route
            path="/workspaces/:workspaceId/:section"
            element={<WorkspaceShell user={user} />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
it('selects a sole authorized workspace without a picker and supports keyboard navigation', async () => {
  fetchMock.mockResolvedValue(page([first]));
  show();
  await screen.findByRole('heading', { name: 'Overview' });
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  const keyboard = userEvent.setup();
  await keyboard.tab();
  expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveFocus();
  await keyboard.tab();
  await keyboard.tab();
  await keyboard.keyboard('{Enter}');
  expect(
    await screen.findByRole('heading', { name: 'Resources' }),
  ).toBeVisible();
  expect(screen.getByRole('link', { name: 'Resources' })).toHaveAttribute(
    'aria-current',
    'page',
  );
});
it('switches URL scope through an accessible selector and never shows unknown workspace data', async () => {
  fetchMock.mockResolvedValue(page([first, second]));
  show(`/workspaces/${first.id}/settings`);
  await screen.findByRole('heading', { name: 'Settings' });
  await userEvent.selectOptions(
    screen.getByRole('combobox', { name: 'Workspace' }),
    second.id,
  );
  expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute(
    'href',
    `/workspaces/${second.id}/settings`,
  );
  expect(screen.getByRole('combobox')).toHaveValue(second.id);
});
it('does not render a scope from an unauthorized URL', async () => {
  fetchMock.mockResolvedValue(page([first]));
  show(`/workspaces/${second.id}/resources`);
  await screen.findByRole('heading', { name: 'Workspace unavailable' });
  expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
  expect(screen.queryByText(second.name)).not.toBeInTheDocument();
});
it('removes a workspace view while access is revalidated and after removal', async () => {
  fetchMock.mockResolvedValue(page([first]));
  const client = show(`/workspaces/${first.id}/resources`);
  await screen.findByRole('heading', { name: 'Resources' });
  let resolve!: (value: Response) => void;
  fetchMock.mockImplementationOnce(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  await act(async () => {
    await Promise.resolve();
    void client.invalidateQueries({
      queryKey: ['user', user.id, 'workspaces'],
    });
  });
  await screen.findByRole('status');
  expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
  await act(async () => {
    await Promise.resolve();
    resolve(page([]));
  });
  await screen.findByRole('heading', { name: 'No workspace access' });
  expect(
    screen.queryByRole('heading', { name: 'Resources' }),
  ).not.toBeInTheDocument();
});
it('can find an authorized workspace on a later bounded page', async () => {
  fetchMock
    .mockResolvedValueOnce(page([first], first.id))
    .mockResolvedValueOnce(page([second]));
  show(`/workspaces/${second.id}/activity`);
  await userEvent.click(
    await screen.findByRole('button', { name: 'Load more workspaces' }),
  );
  await screen.findByRole('heading', { name: 'Activity' });
  expect((fetchMock.mock.calls[1]![0] as URL).href).toContain(
    `cursor=${first.id}`,
  );
});
