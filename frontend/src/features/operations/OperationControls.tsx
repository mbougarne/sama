import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { ProblemSummary } from '../../components/Form';
import { RiskDialog } from '../../components/RiskDialog';
import { path, type Scope, type Schema } from '../connections/api';
import { AuthenticationNotice } from './ActionReview';
import { invalidateRelated, type Operation } from './progress';

type Control = 'cancel' | 'reconciliations' | 'reservation-release';
export function OperationControls({
  scope,
  row,
  reload,
  stale,
}: {
  scope: Scope;
  row: Operation;
  reload: () => Promise<unknown>;
  stale: boolean;
}) {
  const client = useQueryClient();
  const [error, setError] = useState<unknown>(null);
  const [blocked, setBlocked] = useState(false);
  const [busy, setBusy] = useState(false);
  const [resolving, setResolving] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const grant = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'grant-own', {
      connectionId: row.connection_id,
    }),
    queryFn: ({ signal }) =>
      apiRequest<{ actions: Schema['GrantActions'] }>(
        `${path(scope)}/connections/${row.connection_id}/grants`,
        { signal },
      ),
    staleTime: 0,
  });
  const actions = grant.data?.actions ?? [];
  const allowed =
    !stale &&
    !blocked &&
    !busy &&
    !grant.error &&
    !grant.isFetching &&
    scope.workspace.role !== 'viewer' &&
    actions.includes('read') &&
    (actions.includes('operate') || actions.includes('high_impact'));
  async function control(kind: Control, typed = '') {
    if (!allowed || request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(null);
    try {
      await apiRequest<Schema['AcceptedOperation']>(
        `${path(scope)}/operations/${row.id}/${kind}`,
        {
          method: 'POST',
          signal: controller.signal,
          body:
            kind === 'reservation-release'
              ? ({
                  residual_risk_acknowledged: true,
                  typed_target: typed,
                } satisfies Schema['ReservationRelease'])
              : {},
        },
      );
      if (!controller.signal.aborted) {
        setResolving(false);
        invalidateRelated(client, scope, row);
        await client.invalidateQueries({
          queryKey: scopedKey(
            scope.userId,
            scope.workspace.id,
            'operations',
          ).slice(0, 5),
        });
        await reload();
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        setError(failure);
        setBlocked(true);
        setResolving(false);
        // Refresh readable evidence without claiming that a failed control changed it.
        await reload();
        void grant.refetch();
      }
    } finally {
      if (!controller.signal.aborted) setBusy(false);
      request.current = null;
    }
  }
  return (
    <section aria-label="Supported operation controls">
      <h3>Supported operation controls</h3>
      <ProblemSummary error={error || grant.error} />
      <AuthenticationNotice error={error} />
      {row.state === 'queued' && (
        <button disabled={!allowed} onClick={() => void control('cancel')}>
          Cancel queued operation
        </button>
      )}
      {['dispatching', 'waiting_provider', 'reconciling'].includes(
        row.state,
      ) && (
        <p>
          Cannot cancel through Sama after dispatch. Observation never resubmits
          the action.
        </p>
      )}
      {row.state === 'unknown' && !row.reservation_released && (
        <>
          <p>
            Recheck requests bounded observation only. The original action will
            not be retried.
          </p>
          <button
            disabled={!allowed}
            onClick={() => void control('reconciliations')}
          >
            Recheck unknown outcome
          </button>
          {scope.workspace.role === 'owner' &&
            actions.includes('high_impact') && (
              <button disabled={!allowed} onClick={() => setResolving(true)}>
                Review reservation release
              </button>
            )}
        </>
      )}
      {blocked && (
        <p>
          Controls are blocked until current evidence and permission are
          reloaded.
        </p>
      )}
      {(blocked || grant.error) && (
        <button
          disabled={busy || grant.isFetching}
          onClick={() => {
            void (async () => {
              const result = await grant.refetch();
              await reload();
              if (!result.error) {
                setBlocked(false);
                setError(null);
              }
            })();
          }}
        >
          Reload supported controls
        </button>
      )}
      {resolving && (
        <RiskDialog
          title="Release reservation with residual risk"
          target={row.target_name}
          typedTarget
          impact="The provider outcome remains unknown. Releasing this reservation allows later actions to conflict with an action that may still finish. This records your risk acknowledgment; it does not prove success or retry the action. Recent authentication is required."
          evidenceValid={
            allowed &&
            row.state === 'unknown' &&
            !row.reservation_released &&
            scope.workspace.role === 'owner' &&
            actions.includes('high_impact')
          }
          pending={busy}
          onCancel={() => setResolving(false)}
          onConfirm={(typed) => void control('reservation-release', typed)}
        />
      )}
    </section>
  );
}
