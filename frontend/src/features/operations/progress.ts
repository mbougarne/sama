import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { path, type Scope, type Schema } from '../connections/api';
export type Operation = Schema['OperationView'];
const active = (state: Operation['state']) =>
  ['queued', 'dispatching', 'waiting_provider', 'reconciling'].includes(state);
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
export function invalidateRelated(
  client: QueryClient,
  scope: Scope,
  row: Operation,
) {
  void client.invalidateQueries({
    predicate: ({ queryKey: key }) => {
      if (key[1] !== scope.userId || key[3] !== scope.workspace.id)
        return false;
      const filters = (key[5] ?? {}) as Record<string, unknown>;
      switch (key[4]) {
        case 'resource':
          return filters.id === row.resource_id;
        case 'connection':
          return filters.id === row.connection_id;
        case 'resources':
          return (
            !filters.connection_id ||
            filters.connection_id === row.connection_id
          );
        case 'connections':
        case 'overview':
          return true;
        case 'activity':
          return (
            (!filters.target_id ||
              [row.id, row.resource_id, row.connection_id].includes(
                filters.target_id as string,
              )) &&
            (!filters.actor_id || filters.actor_id === row.actor_id)
          );
        default:
          return false;
      }
    },
  });
}
export function useProgress<T>(
  scope: Scope,
  feature: string,
  filters: { id?: string; cursor?: string },
  url: string,
  rows: (data: T) => Operation[],
) {
  const client = useQueryClient();
  const visible = useSyncExternalStore(subscribe, snapshot);
  const previous = useRef(new Map<string, string>());
  const { id, cursor } = filters;
  const key = useMemo(
    () => scopedKey(scope.userId, scope.workspace.id, feature, { id, cursor }),
    [scope.userId, scope.workspace.id, feature, id, cursor],
  );
  const query = useQuery<T>({
    queryKey: key,
    queryFn: ({ signal }) =>
      apiRequest<T>(`${path(scope)}/operations${url}`, { signal }),
    enabled: visible,
    staleTime: 30_000,
    refetchInterval: (query) =>
      visible &&
      !query.state.error &&
      query.state.data &&
      rows(query.state.data).some((row) => active(row.state))
        ? 3000
        : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  useEffect(() => {
    if (!visible) void client.cancelQueries({ queryKey: key });
  }, [visible, client, key]);
  useEffect(() => {
    if (!query.data) return;
    for (const row of rows(query.data)) {
      const signature = `${row.state}:${row.reservation_released}`;
      const before = previous.current.get(row.id);
      if (before && before !== signature) invalidateRelated(client, scope, row);
      previous.current.set(row.id, signature);
    }
  }, [query.data, client, scope, rows]);
  return { ...query, visible };
}
