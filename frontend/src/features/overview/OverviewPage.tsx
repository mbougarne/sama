import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ProblemSummary } from '../../components/Form';
import { Freshness, path, type Scope, type Schema } from '../connections/api';

export default function OverviewPage(scope: Scope) {
  const overview = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'overview'),
    queryFn: ({ signal }) =>
      apiRequest<Schema['WorkspaceOverview']>(`${path(scope)}/overview`, {
        signal,
      }),
    staleTime: 30_000,
  });
  useAccessLoss(overview.error, scope.refreshAccess);
  if (overview.error)
    return (
      <>
        <ProblemSummary error={overview.error} />
        <button onClick={() => void overview.refetch()}>Reload overview</button>
      </>
    );
  if (!overview.data)
    return <p role="status">Loading workspace observations…</p>;
  const data = overview.data;
  return (
    <section>
      <h2>Workspace observations</h2>
      <dl>
        <dt>Authorized connections</dt>
        <dd>{data.connection_count}</dd>
        <dt>Observed resources</dt>
        <dd>{data.resource_count}</dd>
        <dt>Queued or running operations</dt>
        <dd>{data.pending_operations}</dd>
        <dt>Unknown operation outcomes</dt>
        <dd>{data.unknown_operations}</dd>
        <dt>Stale or never-observed connections</dt>
        <dd>{data.stale_connections}</dd>
      </dl>
      <p>
        Resource counts describe the last successful observations. Opening
        Overview does not refresh providers.
      </p>
      {data.connections.map((connection) => (
        <article className="inventory-row" key={connection.id}>
          <h3>
            <Link
              to={`/workspaces/${scope.workspace.id}/connections/${connection.id}`}
            >
              {connection.label}
            </Link>
          </h3>
          <p>
            {connection.provider} · {connection.account_identity} ·{' '}
            {connection.status}
          </p>
          <p>
            Permissions verified:{' '}
            {connection.permissions_verified_at ?? 'Not verified'}
          </p>
          <Freshness value={connection.inventory} />
        </article>
      ))}
      {data.connections_truncated && (
        <Link to={`/workspaces/${scope.workspace.id}/connections`}>
          View the remaining connections
        </Link>
      )}
      <p>
        <Link to={`/workspaces/${scope.workspace.id}/resources`}>
          Browse resource observations
        </Link>
      </p>
    </section>
  );
}
