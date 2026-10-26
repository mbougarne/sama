import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ProblemSummary } from '../../components/Form';
import { Freshness, path, type Scope, type Schema } from '../connections/api';

function subscribe(notify: () => void) {
  document.addEventListener('visibilitychange', notify);
  window.addEventListener('online', notify);
  window.addEventListener('offline', notify);
  return () => {
    document.removeEventListener('visibilitychange', notify);
    window.removeEventListener('online', notify);
    window.removeEventListener('offline', notify);
  };
}
const snapshot = () =>
  document.visibilityState === 'visible' && navigator.onLine;
const active = (status?: string) =>
  status === 'pending' || status === 'running';
export function Refresh({
  scope,
  connectionId,
  resourceType = 'compute.server',
}: {
  scope: Scope;
  connectionId: string;
  resourceType?: Schema['Resource']['type'];
}) {
  const client = useQueryClient();
  const visible = useSyncExternalStore(subscribe, snapshot);
  const [syncId, setSyncId] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const grant = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'grant-own', {
      connectionId,
    }),
    queryFn: ({ signal }) =>
      apiRequest<{ actions: Schema['GrantActions'] }>(
        `${path(scope)}/connections/${connectionId}/grants`,
        { signal },
      ),
    staleTime: 0,
  });
  const key = scopedKey(scope.userId, scope.workspace.id, 'sync', { syncId });
  const sync = useQuery<Schema['SyncStatus']>({
    queryKey: key,
    enabled: (query) =>
      !!syncId &&
      visible &&
      !query.state.error &&
      (!query.state.data || active(query.state.data.status)),
    queryFn: ({ signal }) =>
      apiRequest<Schema['SyncStatus']>(`${path(scope)}/syncs/${syncId}`, {
        signal,
      }),
    refetchInterval: (query) =>
      visible && !query.state.error && active(query.state.data?.status)
        ? 3000
        : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  useAccessLoss(error || grant.error || sync.error, scope.refreshAccess);
  useEffect(() => {
    if (!visible && syncId)
      void client.cancelQueries({
        queryKey: scopedKey(scope.userId, scope.workspace.id, 'sync', {
          syncId,
        }),
      });
  }, [visible, syncId, client, scope.userId, scope.workspace.id]);
  const status = sync.data?.status;
  useEffect(() => {
    if (status && !active(status))
      void client.invalidateQueries({
        predicate: (query) =>
          [
            'resources',
            'resource',
            'connections',
            'connection',
            'overview',
            'activity',
          ].includes(String(query.queryKey[4])),
      });
  }, [status, client]);
  async function refresh() {
    if (busy || active(status)) return;
    setBusy(true);
    setError(null);
    const controller = new AbortController();
    request.current = controller;
    try {
      const value = await apiRequest<{ sync_id: string }>(
        `${path(scope)}/connections/${connectionId}/syncs`,
        {
          method: 'POST',
          body: { resource_type: resourceType },
          signal: controller.signal,
        },
      );
      setSyncId(value.sync_id);
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure);
    } finally {
      setBusy(false);
    }
  }
  const allowed =
    scope.workspace.role !== 'viewer' &&
    grant.data?.actions.includes('refresh') &&
    !grant.error;
  return (
    <section>
      <h3>Refresh provider observations</h3>
      <p>
        Explicit refresh schedules one bounded connection scan. Progress reads
        stay inside Sama.
      </p>
      <ProblemSummary error={error || grant.error || sync.error} />
      {!allowed && (
        <p>An operator role and an explicit refresh grant are required.</p>
      )}
      <button
        disabled={!allowed || busy || (!!syncId && (!status || active(status)))}
        onClick={() => void refresh()}
      >
        {resourceType === 'storage.snapshot'
          ? 'Refresh snapshot inventory'
          : 'Refresh connection inventory'}
      </button>
      {syncId && <p>Sync reference: {syncId}</p>}
      {sync.data && (
        <>
          <p role="status">
            Sync: {sync.data.status} · Pages: {sync.data.page_count}
            {sync.data.failure_code && ` · ${sync.data.failure_code}`}
          </p>
          <Freshness value={sync.data.inventory} />
        </>
      )}
      {!visible && syncId && (
        <p>Progress polling is paused while hidden or offline.</p>
      )}
    </section>
  );
}
