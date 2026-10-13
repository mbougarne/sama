import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { apiRequest } from '../../api/client';
import { scopedKey } from '../../app/QueryScope';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ProblemSummary } from '../../components/Form';
import { useMembers } from '../settings/Members';
import { path, unavailable, type Schema, type Scope } from './api';

export function Grants(props: { scope: Scope; id: string }) {
  return ['owner', 'admin'].includes(props.scope.workspace.role) ? (
    <ManagedGrants {...props} />
  ) : null;
}
function ManagedGrants({ scope, id }: { scope: Scope; id: string }) {
  const members = useMembers(scope);
  const [target, setTarget] = useState('');
  useAccessLoss(members.error, scope.refreshAccess);
  if (members.error) return <ProblemSummary error={members.error} />;
  return (
    <section>
      <h3>Connection grants</h3>
      <p>
        Unchecked actions are denied. Roles and existing grants limit
        delegation; the server rechecks every save.
      </p>
      <label>
        Member{' '}
        <select
          value={target}
          onChange={(event) => setTarget(event.target.value)}
        >
          <option value="">Choose a member</option>
          {members.data?.pages
            .flatMap((page) => page.data)
            .map((member) => (
              <option key={member.user_id} value={member.user_id}>
                {member.display_name || member.user_id} · {member.role}
              </option>
            ))}
        </select>
      </label>
      {members.hasNextPage && (
        <button
          onClick={() => void members.fetchNextPage()}
          disabled={members.isFetching}
        >
          Load more members
        </button>
      )}
      {target && (
        <GrantEditor key={target} scope={scope} id={id} target={target} />
      )}
    </section>
  );
}
function GrantEditor({
  scope,
  id,
  target,
}: {
  scope: Scope;
  id: string;
  target: string;
}) {
  const client = useQueryClient();
  const [draft, setDraft] = useState<string[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const row = useQuery({
    queryKey: scopedKey(scope.userId, scope.workspace.id, 'grant', {
      id,
      target,
    }),
    queryFn: ({ signal }) =>
      apiRequest<Schema['GrantView']>(
        `${path(scope)}/connections/${id}/grants?user_id=${target}`,
        { signal },
      ),
    staleTime: 0,
  });
  useAccessLoss(row.error || error, scope.refreshAccess);
  if (row.error || unavailable(error))
    return (
      <>
        <ProblemSummary error={row.error || error} />
        <button onClick={scope.refreshAccess}>Refresh workspace access</button>
      </>
    );
  if (!row.data) return <p role="status">Loading grant constraints…</p>;
  const actions = draft ?? row.data.actions;
  async function save() {
    setBusy(true);
    setError(null);
    try {
      await apiRequest<void>(`${path(scope)}/connections/${id}/grants`, {
        method: 'PUT',
        body: { user_id: target, actions },
      });
    } catch (failure) {
      setError(failure);
      if (unavailable(failure)) scope.refreshAccess();
    } finally {
      setDraft(null);
      await client.invalidateQueries();
      setBusy(false);
    }
  }
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <ProblemSummary error={error} />
      <fieldset disabled={busy || !row.data.editable}>
        <legend>Explicit action grants</legend>
        {(['read', 'refresh', 'operate', 'high_impact'] as const).map(
          (action) => (
            <p key={action}>
              <label>
                <input
                  type="checkbox"
                  checked={actions.includes(action)}
                  disabled={!row.data?.allowed_actions.includes(action)}
                  onChange={(event) =>
                    setDraft(
                      event.target.checked
                        ? [...actions, action]
                        : action === 'read'
                          ? []
                          : actions.filter((value) => value !== action),
                    )
                  }
                />
                {action.replaceAll('_', ' ')}
              </label>
              {!row.data?.allowed_actions.includes(action) &&
                ' · Outside current delegation ceiling'}
            </p>
          ),
        )}
        <button
          disabled={
            actions.some((action) => action !== 'read') &&
            !actions.includes('read')
          }
        >
          Save grants
        </button>
      </fieldset>
    </form>
  );
}
