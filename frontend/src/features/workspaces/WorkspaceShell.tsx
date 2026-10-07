import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { Navigate, NavLink, useNavigate, useParams } from 'react-router';
import { ApiError, getWorkspaces, type User } from '../../api/client';
import { QueryScope } from '../../app/QueryScope';

import { lazy, Suspense } from 'react';
const SettingsPage = lazy(() => import('../settings/SettingsPage'));

const sections = [
  'overview',
  'resources',
  'connections',
  'operations',
  'activity',
  'settings',
] as const;
const label = (value: string) => value.charAt(0).toUpperCase() + value.slice(1);

export function WorkspaceShell({ user }: { user: User }) {
  const { workspaceId, section = 'overview' } = useParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const access = useInfiniteQuery({
    queryKey: ['user', user.id, 'workspaces'],
    queryFn: ({ pageParam, signal }) => getWorkspaces(pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    staleTime: 0,
    retry: false,
  });
  const workspaces = access.data?.pages.flatMap((page) => page.data) ?? [];
  // Revalidation must finish before rendering a scope, including cached data.
  if (access.isPending || (access.isFetching && !access.isFetchingNextPage))
    return (
      <main>
        <p role="status">Checking workspace access…</p>
      </main>
    );
  if (access.error)
    return (
      <main>
        <h1>
          {access.error instanceof ApiError &&
          [403, 404].includes(access.error.status)
            ? 'Workspace access unavailable'
            : 'Unable to load workspaces'}
        </h1>
        <button onClick={() => void access.refetch()}>Try again</button>
      </main>
    );
  if (!workspaceId && workspaces[0])
    return <Navigate replace to={`/workspaces/${workspaces[0].id}/overview`} />;
  const selected = workspaces.find((workspace) => workspace.id === workspaceId);
  const more = access.hasNextPage && (
    <button
      disabled={access.isFetchingNextPage}
      onClick={() => void access.fetchNextPage()}
    >
      {access.isFetchingNextPage ? 'Loading…' : 'Load more workspaces'}
    </button>
  );
  if (!selected)
    return (
      <main>
        <h1>
          {workspaces.length ? 'Workspace unavailable' : 'No workspace access'}
        </h1>
        <p>Select a workspace you can access.</p>
        {workspaces.map((workspace) => (
          <p key={workspace.id}>
            <NavLink to={`/workspaces/${workspace.id}/overview`}>
              {workspace.name}
            </NavLink>
          </p>
        ))}
        {more}
      </main>
    );
  return (
    <QueryScope userId={user.id} workspaceId={selected.id}>
      <div className="backoffice">
        <a className="skip-link" href="#main-content">
          Skip to content
        </a>
        <aside className="sidebar" aria-label="Backoffice navigation">
          <div className="brand">
            <strong>Sama</strong>
            <span lang="ar" dir="rtl">
              سماء
            </span>
          </div>
          {workspaces.length > 1 || access.hasNextPage ? (
            <label className="workspace-picker">
              Workspace
              <select
                value={selected.id}
                onChange={(event) =>
                  void navigate(
                    `/workspaces/${event.target.value}/${sections.includes(section as (typeof sections)[number]) ? section : 'overview'}`,
                  )
                }
              >
                {workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <p className="workspace-name">{selected.name}</p>
          )}
          {more}
          <nav aria-label="Main">
            {sections.map((item) => (
              <NavLink key={item} to={`/workspaces/${selected.id}/${item}`}>
                {label(item)}
              </NavLink>
            ))}
          </nav>
        </aside>
        <main id="main-content" tabIndex={-1}>
          <p className="eyebrow">{selected.name}</p>
          <h1>
            {sections.includes(section as (typeof sections)[number])
              ? label(section)
              : 'Page not found'}
          </h1>
          {section === 'settings' ? (
            <Suspense fallback={<p role="status">Loading settings…</p>}>
              <SettingsPage
                userId={user.id}
                workspace={selected}
                refreshAccess={() =>
                  void client.invalidateQueries({
                    queryKey: ['user', user.id, 'workspaces'],
                  })
                }
              />
            </Suspense>
          ) : (
            <p>
              {section === 'overview'
                ? 'Your workspace is ready. Resource observations will appear when a supported connection is available.'
                : sections.includes(section as (typeof sections)[number])
                  ? 'This workspace section is not available yet.'
                  : 'Choose a section from the navigation.'}
            </p>
          )}
        </main>
      </div>
    </QueryScope>
  );
}
