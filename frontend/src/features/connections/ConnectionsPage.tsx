import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { apiRequest } from '../../api/client';
import { useAccessLoss } from '../../app/useAccessLoss';
import { scopedKey } from '../../app/QueryScope';
import { ProblemSummary } from '../../components/Form';
import {
  Capabilities,
  Freshness,
  path,
  unavailable,
  type Scope,
  type Schema,
} from './api';
import Onboarding from './Onboarding';
import { ConnectionControls } from './Controls';
import { Grants } from './Grants';

export default function ConnectionsPage(scope: Scope) {
  const { itemId } = useParams();
  return itemId ? (
    <ConnectionDetail key={itemId} scope={scope} id={itemId} />
  ) : (
    <ConnectionList scope={scope} />
  );
}
function ConnectionList({ scope }: { scope: Scope }) {
  const rows = useInfiniteQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'connections'),
    queryFn: ({ pageParam, signal }) =>
      apiRequest<Schema['ConnectionPage']>(
        `${path(scope)}/connections?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ''}`,
        { signal },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    staleTime: 0,
  });
  useAccessLoss(rows.error, scope.refreshAccess);
  if (unavailable(rows.error)) return <ProblemSummary error={rows.error} />;
  return (
    <>
      {['owner', 'admin'].includes(scope.workspace.role) && (
        <Onboarding {...scope} />
      )}
      <h2>Connection health</h2>
      <ProblemSummary error={rows.error} />
      {rows.isPending && <p role="status">Loading connections…</p>}
      {rows.data?.pages
        .flatMap((page) => page.data)
        .map((row) => (
          <article className="settings-section" key={row.id}>
            <h3>
              <Link
                to={`/workspaces/${scope.workspace.id}/connections/${row.id}`}
              >
                {row.label}
              </Link>
            </h3>
            <p>
              {row.provider} · {row.account_identity} · {row.status}
            </p>
            <p>
              Permissions verified:{' '}
              {row.permissions_verified_at ?? 'Not verified'}
            </p>
            <Freshness value={row.inventory} />
            <Capabilities values={row.capabilities} />
          </article>
        ))}
      {rows.data?.pages[0]?.data.length === 0 && (
        <p>No connections granted to you.</p>
      )}
      <button disabled={rows.isFetching} onClick={() => void rows.refetch()}>
        Reload connections
      </button>{' '}
      {rows.hasNextPage && (
        <button
          disabled={rows.isFetching}
          onClick={() => void rows.fetchNextPage()}
        >
          Load more connections
        </button>
      )}
    </>
  );
}
function ConnectionDetail({ scope, id }: { scope: Scope; id: string }) {
  const row = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'connection', { id }),
    queryFn: ({ signal }) =>
      apiRequest<Schema['ConnectionView']>(
        `${path(scope)}/connections/${encodeURIComponent(id)}`,
        { signal },
      ),
    staleTime: 0,
  });
  useAccessLoss(row.error, scope.refreshAccess);
  if (row.error)
    return (
      <>
        <ProblemSummary error={row.error} />
        <button onClick={scope.refreshAccess}>Refresh workspace access</button>
      </>
    );
  if (!row.data) return <p role="status">Loading connection…</p>;
  return (
    <section className="settings-section">
      <h2>{row.data.label}</h2>
      <p>
        {row.data.provider} · {row.data.api_family} ·{' '}
        {row.data.account_identity} · {row.data.status}
      </p>
      <p>
        Permissions verified:{' '}
        {row.data.permissions_verified_at ?? 'Not verified'}
      </p>
      <Freshness value={row.data.inventory} />
      <Capabilities values={row.data.capabilities} />
      <ConnectionControls scope={scope} row={row.data} />
      <Grants scope={scope} id={id} />
    </section>
  );
}
