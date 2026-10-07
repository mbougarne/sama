import { useInfiniteQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { apiRequest, ApiError } from '../../api/client';
import type { components } from '../../api/generated/openapi';
import { scopedKey } from '../../app/QueryScope';
import { Field, ProblemSummary } from '../../components/Form';
import { Invitation } from './Invitation';

export type Member = components['schemas']['Member'];
export type Scope = {
  userId: string;
  workspace: components['schemas']['Workspace'];
  refreshAccess: () => void;
};
export const workspacePath = (id: string) =>
  `/api/v1/workspaces/${encodeURIComponent(id)}`;
export const denied = (error: unknown) =>
  error instanceof ApiError && [403, 404].includes(error.status);
export function useMembers({ userId, workspace }: Scope) {
  return useInfiniteQuery({
    queryKey: scopedKey(userId, workspace.id, 'members'),
    queryFn: ({ pageParam, signal }) =>
      apiRequest<components['schemas']['MemberPage']>(
        `${workspacePath(workspace.id)}/members?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ''}`,
        { signal },
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    staleTime: 0,
  });
}
export function Members(props: Scope) {
  return props.workspace.role === 'owner' ||
    props.workspace.role === 'admin' ? (
    <MemberList {...props} />
  ) : (
    <p>Membership management requires an owner or admin.</p>
  );
}
function MemberList(props: Scope) {
  const members = useMembers(props);
  const [blocked, setBlocked] = useState<unknown>(null);
  if (blocked || denied(members.error))
    return (
      <>
        <ProblemSummary error={blocked || members.error} />
        <button onClick={props.refreshAccess}>Refresh workspace access</button>
      </>
    );
  return (
    <section className="settings-section" aria-labelledby="members-heading">
      <h2 id="members-heading">Members</h2>
      <ProblemSummary error={members.error} />
      <button
        disabled={members.isFetching}
        onClick={() => void members.refetch()}
      >
        Reload current members
      </button>
      {members.isPending && <p role="status">Loading members…</p>}
      {members.data?.pages
        .flatMap((page) => page.data)
        .map((member) => (
          <MemberRow
            key={member.user_id}
            member={member}
            scope={props}
            block={setBlocked}
            refresh={() => void members.refetch()}
          />
        ))}
      {members.hasNextPage && (
        <button
          disabled={members.isFetching}
          onClick={() => void members.fetchNextPage()}
        >
          Load more members
        </button>
      )}
      <Invitation scope={props} block={setBlocked} />
    </section>
  );
}
const roles: Member['role'][] = ['viewer', 'operator', 'admin', 'owner'];
export function RoleField({
  value,
  owner,
  change,
}: {
  value: string;
  owner: boolean;
  change: (value: Member['role']) => void;
}) {
  return (
    <Field label="Role">
      {(attributes) => (
        <select
          {...attributes}
          value={value}
          onChange={(event) => change(event.target.value as Member['role'])}
        >
          {roles
            .filter((role) => owner || role === 'viewer' || role === 'operator')
            .map((role) => (
              <option key={role}>{role}</option>
            ))}
        </select>
      )}
    </Field>
  );
}
function MemberRow({
  member,
  scope,
  block,
  refresh,
}: {
  member: Member;
  scope: Scope;
  block: (error: unknown) => void;
  refresh: () => void;
}) {
  const [role, setRole] = useState(member.role);
  const [error, setError] = useState<unknown>(null);
  const [pending, setPending] = useState(false);
  const owner = scope.workspace.role === 'owner';
  if (!owner && !['viewer', 'operator'].includes(member.role)) return null;
  async function change(remove: boolean) {
    setPending(true);
    setError(null);
    try {
      await apiRequest<void>(
        `${workspacePath(scope.workspace.id)}/members/${member.user_id}`,
        {
          method: remove ? 'DELETE' : 'PUT',
          body: remove
            ? { version: member.version }
            : { version: member.version, role },
        },
      );
      refresh();
      if (member.user_id === scope.userId) scope.refreshAccess();
    } catch (failure) {
      if (denied(failure)) {
        block(failure);
        scope.refreshAccess();
      } else setError(failure);
    } finally {
      setPending(false);
    }
  }
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void change(false);
      }}
    >
      <h3>{member.display_name || member.user_id}</h3>
      <p>
        {member.user_id} · Current role: {member.role}
      </p>
      <ProblemSummary error={error} />
      <fieldset disabled={pending}>
        <legend>Membership</legend>
        <RoleField value={role} owner={owner} change={setRole} />
        <button>Save role</button>{' '}
        <button type="button" onClick={() => void change(true)}>
          Remove member
        </button>
      </fieldset>
    </form>
  );
}
