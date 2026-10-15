import { Link, useParams, useSearchParams } from 'react-router';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ProblemSummary } from '../../components/Form';
import { unavailable, type Scope, type Schema } from '../connections/api';
import { OperationControls } from './OperationControls';
import { useProgress, type Operation } from './progress';
const listRows = (page: Schema['OperationPage']) => page.data;
const detailRows = (row: Operation) => [row];
export const states: Record<Operation['state'], string> = {
  queued: 'Accepted — queued before dispatch',
  dispatching: 'Running — dispatching; cannot cancel through Sama',
  waiting_provider: 'Running — waiting for provider evidence',
  reconciling: 'Running — observing provider outcome',
  unknown: 'Outcome unknown — evidence is insufficient',
  succeeded: 'Succeeded — provider outcome verified',
  failed: 'Failed — definite failure established',
  cancelled: 'Cancelled — before provider submission',
};
export default function OperationsPage(scope: Scope) {
  const { itemId } = useParams();
  return itemId ? (
    <OperationDetail key={itemId} scope={scope} id={itemId} />
  ) : (
    <OperationList scope={scope} />
  );
}
function Identity({ row, scope }: { row: Operation; scope: Scope }) {
  return (
    <>
      <p>
        {row.provider} · {row.account_identity} ·{' '}
        {row.region || 'Region unknown'}
      </p>
      <p>
        Native target: {row.native_id} · Actor: {row.actor_id}
      </p>
      <p role="status">{states[row.state]}</p>
      <p>
        Created: {row.created_at} · Last evidence: {row.updated_at}
      </p>
      <Link
        to={`/workspaces/${scope.workspace.id}/resources/${row.resource_id}`}
      >
        Resource {row.target_name || row.native_id}
      </Link>
      {' · '}
      <Link
        to={`/workspaces/${scope.workspace.id}/connections/${row.connection_id}`}
      >
        Connection {row.connection_id}
      </Link>
    </>
  );
}
function OperationList({ scope }: { scope: Scope }) {
  const [params, setParams] = useSearchParams();
  const cursor = params.get('cursor') ?? '';
  const query = useProgress(
    scope,
    'operations',
    { cursor },
    `?limit=50&cursor=${encodeURIComponent(cursor)}`,
    listRows,
  );
  useAccessLoss(query.error, scope.refreshAccess);
  return (
    <>
      <ProblemSummary error={query.error} />
      {query.isPending && <p role="status">Loading operations…</p>}
      {!unavailable(query.error) &&
        query.data?.data.map((row) => (
          <article className="inventory-row" key={row.id}>
            <h2>
              <Link
                to={`/workspaces/${scope.workspace.id}/operations/${row.id}`}
              >
                {row.action} · {row.target_name}
              </Link>
            </h2>
            <Identity row={row} scope={scope} />
          </article>
        ))}
      {!query.error && query.data?.data.length === 0 && (
        <p>No operations on this page.</p>
      )}
      {!query.visible && (
        <p>Progress polling is paused while hidden or offline.</p>
      )}
      <div className="dialog-actions">
        <button
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          Reload operations
        </button>
        {cursor && (
          <button onClick={() => setParams({})}>First operations page</button>
        )}
        {!unavailable(query.error) && query.data?.next_cursor && (
          <button
            onClick={() => setParams({ cursor: query.data?.next_cursor ?? '' })}
          >
            Next operations page
          </button>
        )}
      </div>
    </>
  );
}
export function OperationDetail({ scope, id }: { scope: Scope; id: string }) {
  const query = useProgress(
    scope,
    'operation',
    { id },
    `/${encodeURIComponent(id)}`,
    detailRows,
  );
  useAccessLoss(query.error, scope.refreshAccess);
  const row = unavailable(query.error) ? undefined : query.data;
  return (
    <>
      <ProblemSummary error={query.error} />
      {query.isPending && <p role="status">Loading operation evidence…</p>}
      {row && (
        <article className="inventory-row">
          <h2>
            {row.action} · {row.target_name}
          </h2>
          <p>Operation {row.id}</p>
          <Identity row={row} scope={scope} />
          {row.provider_reference && (
            <p>Provider reference: {row.provider_reference}</p>
          )}
          {row.uncertain && (
            <p>
              The provider may have performed this action. Do not resubmit it.
            </p>
          )}
          {row.reservation_released && (
            <p>
              An owner released the reservation and acknowledged residual risk.
              This does not prove success.
            </p>
          )}
          <h3>Recorded progress</h3>
          <ol>
            {row.events.map((event) => (
              <li key={event.sequence}>
                Event {event.sequence}: {states[event.state]} ·{' '}
                <time dateTime={event.created_at}>{event.created_at}</time>
              </li>
            ))}
          </ol>
          <OperationControls
            key={row.id}
            scope={scope}
            row={row}
            reload={() => query.refetch()}
            stale={!!query.error || query.isFetching}
          />
        </article>
      )}
      {!query.visible && (
        <p>Progress polling is paused while hidden or offline.</p>
      )}
      <button disabled={query.isFetching} onClick={() => void query.refetch()}>
        Reload operation evidence
      </button>{' '}
      <Link to={`/workspaces/${scope.workspace.id}/operations`}>
        Back to operations
      </Link>
    </>
  );
}
