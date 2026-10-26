import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ActionSubmission } from '../operations/ActionSubmission';
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
        Status: {resource.status} · Provider status:{' '}
        {resource.native_status || 'Not reported'}
      </p>
      <p>Provider observation: {resource.observed_at}</p>
      <Freshness value={resource.inventory} />
      {resource.type === 'storage.snapshot' ? (
        <SnapshotDetails resource={resource} />
      ) : (
        <>
          <h3>Server details</h3>
          <dl>
            <dt>Machine type</dt>
            <dd>{resource.details.machine_type ?? 'Unknown'}</dd>
            <dt>Region</dt>
            <dd>{resource.details.region ?? 'Unknown'}</dd>
          </dl>
          <Link
            to={`/workspaces/${scope.workspace.id}/resources?${new URLSearchParams({ type: 'storage.snapshot', connection_id: resource.connection_id, source_native_id: resource.native_id, source_type: 'compute.server' })}`}
          >
            Snapshots for this server
          </Link>
          <Refresh
            scope={scope}
            connectionId={resource.connection_id}
            resourceType="storage.snapshot"
          />
        </>
      )}
      <h3>Action availability</h3>
      <Refresh
        scope={scope}
        connectionId={resource.connection_id}
        resourceType={resource.type}
      />
      <Capabilities values={resource.available_actions} />
      <ActionSubmission key={resource.id} scope={scope} resource={resource} />
      <Link
        to={`/workspaces/${scope.workspace.id}/resources?${params.toString()}`}
      >
        Back to resource observations
      </Link>
    </article>
  );
}

function SnapshotDetails({ resource }: { resource: Schema['ResourceDetail'] }) {
  const value = resource.details.snapshot;
  return (
    <>
      <h3>Snapshot details</h3>
      {resource.status === 'unknown' && (
        <p role="status">
          Snapshot readiness is unknown. This observation does not prove a
          usable snapshot.
        </p>
      )}
      {resource.status === 'pending' && (
        <p role="status">
          Snapshot creation is pending. A usable snapshot is not yet confirmed.
        </p>
      )}
      {!value ? (
        <p>Snapshot metadata is unavailable.</p>
      ) : (
        <dl>
          <dt>Source resource</dt>
          <dd>
            {value.source_native_id || 'Not reported'} (
            {value.source_type || 'type not reported'})
          </dd>
          <dt>Created</dt>
          <dd>{value.created_at ?? 'Not reported'}</dd>
          <dt>Provider creation timestamp</dt>
          <dd>
            {value.native_created_at || 'Not reported'}{' '}
            {value.creation_time_reason && `(${value.creation_time_reason})`}
          </dd>
          <dt>Size in bytes</dt>
          <dd>{value.size_bytes ?? 'Not reported'}</dd>
          <dt>Size in provider GB</dt>
          <dd>{value.size_gb ?? 'Not reported'}</dd>
          <dt>Minimum disk in provider GB</dt>
          <dd>{value.minimum_disk_gb ?? 'Not reported'}</dd>
          <dt>Regions</dt>
          <dd>{value.regions.join(', ') || 'Not reported'}</dd>
          <dt>Status evidence</dt>
          <dd>
            {value.status_reason || resource.native_status || 'Not reported'}
          </dd>
        </dl>
      )}
    </>
  );
}
