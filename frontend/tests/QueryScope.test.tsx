import { expect, it } from '@jest/globals';
import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { StrictMode, useState } from 'react';
import { QueryScope, scopedKey } from '../src/app/QueryScope';

it('normalizes set filters and includes user, workspace and resource scope', () => {
  const first = scopedKey('user-a', 'workspace-a', 'resources', {
    region: ['b', 'a', 'b'],
    cursor: undefined,
    connection: 'connection-a',
  });
  expect(first).toEqual(
    scopedKey('user-a', 'workspace-a', 'resources', {
      connection: 'connection-a',
      region: ['a', 'b'],
    }),
  );
  expect(first).not.toEqual(scopedKey('user-b', 'workspace-a', 'resources'));
  expect(first).not.toEqual(scopedKey('user-a', 'workspace-b', 'resources'));
});

it.each(['workspace', 'identity'])(
  'discards %s drafts and late queries before showing a fresh scope',
  async (change) => {
    const clients: QueryClient[] = [];
    let finishOld!: (value: string) => void;
    let oldSignal: AbortSignal | undefined;
    function Feature({ user, workspace }: { user: string; workspace: string }) {
      const client = useQueryClient();
      if (!clients.includes(client)) clients.push(client);
      const [draft, setDraft] = useState('');
      const query = useQuery({
        queryKey: scopedKey(user, workspace, 'resources'),
        queryFn: ({ signal }) => {
          if (user === 'user-a' && workspace === 'workspace-a') {
            oldSignal = signal;
            return new Promise<string>((resolve) => {
              finishOld = resolve;
            });
          }
          return Promise.resolve('New scope data');
        },
      });
      return (
        <>
          <label>
            Draft
            <input
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
            />
          </label>
          <p>{query.data ?? 'Loading'}</p>
        </>
      );
    }
    const view = (user: string, workspace: string) => (
      <QueryScope userId={user} workspaceId={workspace}>
        <Feature user={user} workspace={workspace} />
      </QueryScope>
    );
    const { rerender, unmount } = render(view('user-a', 'workspace-a'));
    await userEvent.type(screen.getByLabelText('Draft'), 'private draft');
    rerender(view(change === 'identity' ? 'user-b' : 'user-a', 'workspace-b'));
    expect(screen.getByLabelText('Draft')).toHaveValue('');
    await screen.findByText('New scope data');
    await act(async () => {
      await Promise.resolve();
      finishOld('Old secret');
    });
    expect(screen.queryByText('Old secret')).not.toBeInTheDocument();
    expect(oldSignal?.aborted).toBe(true);
    expect(clients).toHaveLength(2);
    expect(clients[0]!.getQueryCache().getAll()).toHaveLength(0);
    unmount();
    await waitFor(() =>
      expect(clients[1]!.getQueryCache().getAll()).toHaveLength(0),
    );
    expect(localStorage.length).toBe(0);
  },
);

it('loads data under development StrictMode cleanup and remount', async () => {
  function Feature() {
    const query = useQuery({
      queryKey: scopedKey('user', 'workspace', 'example'),
      queryFn: () => Promise.resolve('Loaded safely'),
    });
    return <p>{query.data ?? 'Loading'}</p>;
  }
  render(
    <StrictMode>
      <QueryScope userId="user" workspaceId="workspace">
        <Feature />
      </QueryScope>
    </StrictMode>,
  );
  await screen.findByText('Loaded safely');
});
