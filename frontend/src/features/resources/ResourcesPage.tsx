import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ResourceDetail } from './ResourceDetail';
import { Field, ProblemSummary } from '../../components/Form';
import {
  Freshness,
  path,
  unavailable,
  type Scope,
  type Schema,
} from '../connections/api';

const filters = [
  'provider',
  'account',
  'connection_id',
  'type',
  'region',
  'source_native_id',
  'source_type',
] as const;
export default function ResourcesPage(scope: Scope) {
  const { itemId } = useParams();
  return itemId ? (
    <ResourceDetail key={itemId} scope={scope} id={itemId} />
  ) : (
    <ResourceList scope={scope} />
  );
}
function ResourceList({ scope }: { scope: Scope }) {
  const [params, setParams] = useSearchParams();
  const values = Object.fromEntries(params);
  const rows = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'resources', values),
    queryFn: ({ signal }) =>
      apiRequest<Schema['ResourcePage']>(
        `${path(scope)}/resources?limit=50&${params.toString()}`,
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
      <p>
        <Link to="?type=storage.snapshot">Snapshot observations</Link> ·{' '}
        <Link to="?type=compute.server">Server observations</Link>
      </p>
      <form
        key={filters.map((field) => params.get(field)).join(':')}
        onSubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          const next = new URLSearchParams();
          for (const field of filters) {
            const entry = data.get(field);
            const value = typeof entry === 'string' ? entry.trim() : '';
            if (value) next.set(field, value);
          }
          setParams(next);
        }}
      >
        <fieldset>
          <legend>Resource filters</legend>
          {filters.map((field) => (
            <Field key={field} label={field.replaceAll('_', ' ')}>
              {(attrs) => (
                <input
                  {...attrs}
                  name={field}
                  defaultValue={params.get(field) ?? ''}
                  maxLength={
                    field === 'account' || field === 'source_native_id'
                      ? 512
                      : 128
                  }
                />
              )}
            </Field>
          ))}
          <button>Apply filters</button>{' '}
          <button type="button" onClick={() => setParams({})}>
            Clear filters
          </button>
        </fieldset>
      </form>
      <ProblemSummary error={rows.error} />
      {rows.isPending && <p role="status">Loading resource observations…</p>}
      {!unavailable(rows.error) &&
        rows.data?.data.map((row) => (
          <article className="inventory-row" key={row.id}>
            <h2>
              <Link
                to={`/workspaces/${scope.workspace.id}/resources/${row.id}?${params.toString()}`}
              >
                {row.name || row.native_id}
              </Link>
            </h2>
            <p>
              {row.provider} · {row.account_identity} ·{' '}
              {row.region || 'Region unknown'} · {row.type}
            </p>
            <p>
              Status: {row.status} · Provider status: {row.native_status}
            </p>
            <p>Provider observation: {row.observed_at}</p>
            <Freshness value={row.inventory} />
          </article>
        ))}
      {!rows.error && rows.data?.data.length === 0 && (
        <p>No resource observations match these filters.</p>
      )}
      <div className="dialog-actions">
        <button disabled={rows.isFetching} onClick={() => void rows.refetch()}>
          Reload observations
        </button>
        {params.has('cursor') && (
          <button onClick={() => page()}>First page</button>
        )}
        {!unavailable(rows.error) && rows.data?.next_cursor && (
          <button
            disabled={rows.isFetching}
            onClick={() => page(rows.data.next_cursor!)}
          >
            Next resource page
          </button>
        )}
      </div>
    </>
  );
}
