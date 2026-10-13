import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { Refresh } from './Refresh';
import { ProblemSummary } from '../../components/Form';
import {
  Capabilities,
  Freshness,
  path,
  type Scope,
  type Schema,
} from '../connections/api';

export function ResourceDetail({ scope, id }: { scope: Scope; id: string }) {
  const [params] = useSearchParams();
  const row = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'resource', { id }),
    queryFn: ({ signal }) =>
      apiRequest<Schema['ResourceDetail']>(
        `${path(scope)}/resources/${encodeURIComponent(id)}`,
        { signal },
      ),
    staleTime: 30_000,
  });
  useAccessLoss(row.error, scope.refreshAccess);
  if (row.error)
    return (
      <>
        <ProblemSummary error={row.error} />
        <button onClick={() => void row.refetch()}>Reload resource</button>
      </>
    );
  if (!row.data) return <p role="status">Loading resource detail…</p>;
  const resource = row.data;
  return (
    <article className="inventory-row">
      <h2>{resource.name || resource.native_id}</h2>
      <p>
        {resource.provider} · {resource.account_identity} ·{' '}
        {resource.region || 'Region unknown'}
      </p>
      <p>
        API family: {resource.api_family} · Native identity:{' '}
        {resource.native_id}
      </p>
      <p>
        Status: {resource.status} · Provider status: {resource.native_status}
      </p>
      <p>Provider observation: {resource.observed_at}</p>
      <Freshness value={resource.inventory} />
      <h3>Server details</h3>
      <dl>
        <dt>Machine type</dt>
        <dd>{resource.details.machine_type ?? 'Unknown'}</dd>
        <dt>Region</dt>
        <dd>{resource.details.region ?? 'Unknown'}</dd>
      </dl>
      <h3>Action availability</h3>
      <Refresh scope={scope} connectionId={resource.connection_id} />
      <Capabilities values={resource.available_actions} />
      <p>
        Action availability is advisory; reviewed operation controls require
        their own implementation and current server authorization.
      </p>
      <Link
        to={`/workspaces/${scope.workspace.id}/resources?${params.toString()}`}
      >
        Back to resource observations
      </Link>
    </article>
  );
}
