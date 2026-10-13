import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { Field, ProblemSummary } from '../../components/Form';
import { path, unavailable, type Scope, type Schema } from '../connections/api';

const filters = ['actor_id', 'action', 'target_id'] as const;
const entity = (id?: string) =>
  !!id &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    id,
  );
export default function ActivityPage(scope: Scope) {
  const [params, setParams] = useSearchParams();
  const rows = useQuery({
    queryKey: scopedKey(
      scope.userId,
      scope.workspace.id,
      'activity',
      Object.fromEntries(params),
    ),
    queryFn: ({ signal }) =>
      apiRequest<Schema['AuditPage']>(
        `${path(scope)}/audit-events?limit=50&${params.toString()}`,
        { signal },
      ),
    staleTime: 30_000,
  });
  useAccessLoss(rows.error, scope.refreshAccess);
  const page = (cursor?: string) => {
    const next = new URLSearchParams(params);
    next.delete('cursor');
    if (cursor) next.set('cursor', cursor);
    setParams(next);
  };
  return (
    <>
      <form
        key={filters.map((field) => params.get(field)).join(':')}
        onSubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          const next = new URLSearchParams();
          for (const field of filters) {
            const value = data.get(field);
            if (typeof value === 'string' && value.trim())
              next.set(field, value.trim());
          }
          setParams(next);
        }}
      >
        <fieldset>
          <legend>Activity filters</legend>
          {filters.map((field) => (
            <Field label={field.replaceAll('_', ' ')} key={field}>
              {(attrs) => (
                <input
                  {...attrs}
                  name={field}
                  defaultValue={params.get(field) ?? ''}
                  maxLength={64}
                />
              )}
            </Field>
          ))}
          <button>Apply activity filters</button>{' '}
          <button type="button" onClick={() => setParams({})}>
            Clear activity filters
          </button>
        </fieldset>
      </form>
      <ProblemSummary error={rows.error} />
      {rows.isPending && <p role="status">Loading activity history…</p>}
      {!unavailable(rows.error) &&
        rows.data?.data.map((event) => (
          <article className="inventory-row" key={event.id}>
            <h2>{event.action}</h2>
            <p>
              <time dateTime={event.created_at}>{event.created_at}</time> ·
              Actor: {event.actor_id ?? event.actor_kind}
            </p>
            <p>Historical actor IDs remain after membership removal.</p>
            {entity(event.metadata.resource_id) && (
              <p>
                <Link
                  to={`/workspaces/${scope.workspace.id}/resources/${event.metadata.resource_id}`}
                >
                  Resource {event.metadata.resource_id}
                </Link>
              </p>
            )}
            {entity(event.metadata.operation_id) && (
              <p>
                <Link
                  to={`/workspaces/${scope.workspace.id}/operations/${event.metadata.operation_id}`}
                >
                  Operation {event.metadata.operation_id}
                </Link>
              </p>
            )}
            {entity(event.metadata.connection_id) && (
              <p>
                <Link
                  to={`/workspaces/${scope.workspace.id}/connections/${event.metadata.connection_id}`}
                >
                  Connection {event.metadata.connection_id}
                </Link>
              </p>
            )}
            {event.metadata.reason_code && (
              <p>Reason: {event.metadata.reason_code}</p>
            )}
          </article>
        ))}
      {!rows.error && rows.data?.data.length === 0 && (
        <p>No activity matches these filters.</p>
      )}
      <div className="dialog-actions">
        <button disabled={rows.isFetching} onClick={() => void rows.refetch()}>
          Reload activity
        </button>
        {params.has('cursor') && (
          <button onClick={() => page()}>First activity page</button>
        )}
        {!unavailable(rows.error) && rows.data?.next_cursor && (
          <button
            disabled={rows.isFetching}
            onClick={() => page(rows.data.next_cursor!)}
          >
            Next activity page
          </button>
        )}
      </div>
    </>
  );
}
