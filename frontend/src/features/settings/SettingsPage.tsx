import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { apiRequest } from '../../api/client';
import type { components } from '../../api/generated/openapi';
import { scopedKey } from '../../app/QueryScope';
import { Field, ProblemSummary } from '../../components/Form';
import { RiskDialog } from '../../components/RiskDialog';
import {
  Members,
  useMembers,
  workspacePath,
  denied,
  type Scope,
  type Member,
} from './Members';

type Settings = components['schemas']['WorkspaceSettings'];
export default function SettingsPage(props: Scope) {
  return (
    <>
      {props.workspace.role === 'owner' && <OwnerSettings {...props} />}
      <Members {...props} />
    </>
  );
}
function OwnerSettings(props: Scope) {
  const settings = useQuery({
    queryKey: scopedKey(props.userId, props.workspace.id, 'settings'),
    queryFn: ({ signal }) =>
      apiRequest<Settings>(`${workspacePath(props.workspace.id)}/settings`, {
        signal,
      }),
    staleTime: 0,
  });
  if (settings.error)
    return (
      <>
        <ProblemSummary error={settings.error} />
        <button onClick={props.refreshAccess}>Refresh workspace access</button>
      </>
    );
  if (!settings.data) return <p role="status">Loading settings…</p>;
  return (
    <>
      <SettingsForm
        key={settings.data.policy_version}
        initial={settings.data}
        scope={props}
        reload={() => void settings.refetch()}
      />
      <Ownership {...props} />
    </>
  );
}
function SettingsForm({
  initial,
  scope,
  reload,
}: {
  initial: Settings;
  scope: Scope;
  reload: () => void;
}) {
  const [draft, setDraft] = useState(initial);
  const [pending, setPending] = useState(false),
    [error, setError] = useState<unknown>(null);
  async function save() {
    setPending(true);
    setError(null);
    try {
      await apiRequest<Settings>(
        `${workspacePath(scope.workspace.id)}/settings`,
        { method: 'PUT', body: draft },
      );
      scope.refreshAccess();
    } catch (failure) {
      setError(failure);
      if (denied(failure)) scope.refreshAccess();
    } finally {
      setPending(false);
    }
  }
  if (denied(error))
    return (
      <>
        <ProblemSummary error={error} />
        <button onClick={scope.refreshAccess}>Refresh workspace access</button>
      </>
    );
  return (
    <form
      className="settings-section"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <h2>Workspace settings</h2>
      <ProblemSummary error={error} />
      <fieldset disabled={pending}>
        <legend>Name and operational limits</legend>
        <Field label="Workspace name">
          {(attributes) => (
            <input
              {...attributes}
              required
              maxLength={100}
              value={draft.name}
              onChange={(event) =>
                setDraft({ ...draft, name: event.target.value })
              }
            />
          )}
        </Field>
        <Field label="Queue limit" hint="1–1000 queued operations.">
          {(attributes) => (
            <input
              {...attributes}
              type="number"
              required
              min={1}
              max={1000}
              value={draft.queue_limit}
              onChange={(event) =>
                setDraft({ ...draft, queue_limit: Number(event.target.value) })
              }
            />
          )}
        </Field>
        <Field label="Audit retention days" hint="180–3650 days.">
          {(attributes) => (
            <input
              {...attributes}
              type="number"
              required
              min={180}
              max={3650}
              value={draft.audit_retention_days}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  audit_retention_days: Number(event.target.value),
                })
              }
            />
          )}
        </Field>
        <button>Save settings</button>{' '}
        <button
          type="button"
          onClick={() => {
            setDraft(initial);
            setError(null);
            reload();
          }}
        >
          Discard entries and reload settings
        </button>
      </fieldset>
    </form>
  );
}
function Ownership(props: Scope) {
  const members = useMembers(props);
  const [targetId, setTargetId] = useState(''),
    [demote, setDemote] = useState(false);
  const [review, setReview] = useState<{
    actor: Member;
    target: Member;
    demote: boolean;
  } | null>(null);
  const [pending, setPending] = useState(false),
    [error, setError] = useState<unknown>(null);
  const loaded = members.data?.pages.flatMap((page) => page.data) ?? [];
  const actor = loaded.find((member) => member.user_id === props.userId);
  const target = loaded.find((member) => member.user_id === targetId);
  async function transfer() {
    if (!review) return;
    setPending(true);
    setError(null);
    try {
      await apiRequest<void>(
        `${workspacePath(props.workspace.id)}/ownership-transfers`,
        {
          method: 'POST',
          body: {
            user_id: review.target.user_id,
            actor_version: review.actor.version,
            target_version: review.target.version,
            demote: review.demote,
          },
        },
      );
      setReview(null);
      props.refreshAccess();
    } catch (failure) {
      setReview(null);
      setError(failure);
      if (denied(failure)) props.refreshAccess();
      void members.refetch();
    } finally {
      setPending(false);
    }
  }
  if (denied(error) || denied(members.error))
    return (
      <>
        <ProblemSummary error={error || members.error} />
        <button onClick={props.refreshAccess}>Refresh workspace access</button>
      </>
    );
  return (
    <section className="settings-section" aria-labelledby="ownership-heading">
      <h2 id="ownership-heading">Transfer ownership</h2>
      <ProblemSummary error={error} />
      <Field label="New owner">
        {(attributes) => (
          <select
            {...attributes}
            value={targetId}
            disabled={pending}
            onChange={(event) => setTargetId(event.target.value)}
          >
            <option value="">Select a current member</option>
            {loaded
              .filter(
                (member) =>
                  member.user_id !== props.userId && member.role !== 'owner',
              )
              .map((member) => (
                <option key={member.user_id} value={member.user_id}>
                  {member.display_name || member.user_id} ({member.user_id})
                </option>
              ))}
          </select>
        )}
      </Field>
      <label>
        <input
          type="checkbox"
          checked={demote}
          disabled={pending}
          onChange={(event) => setDemote(event.target.checked)}
        />{' '}
        Make me an admin after transfer
      </label>
      {!actor && (
        <p>
          Load current members below until your own membership is available.
        </p>
      )}
      <p>
        <button
          disabled={
            !actor ||
            actor.role !== 'owner' ||
            !target ||
            members.isFetching ||
            pending
          }
          onClick={() => {
            if (actor && target) {
              setError(null);
              setReview({ actor, target, demote });
            }
          }}
        >
          Review transfer
        </button>
      </p>
      {review && (
        <RiskDialog
          title="Confirm ownership transfer"
          target={review.target.user_id}
          impact={`Grant full workspace control to ${review.target.display_name || review.target.user_id}.${review.demote ? ' You will become an admin and lose owner-only controls.' : ' You will remain an owner.'}`}
          typedTarget
          evidenceValid={
            !members.isFetching &&
            !members.error &&
            actor?.version === review.actor.version &&
            target?.version === review.target.version
          }
          pending={pending}
          onCancel={() => setReview(null)}
          onConfirm={() => void transfer()}
        />
      )}
    </section>
  );
}
