import { useState } from 'react';
import { apiRequest } from '../../api/client';
import { Field, ProblemSummary } from '../../components/Form';
import {
  RoleField,
  denied,
  workspacePath,
  type Scope,
  type Member,
} from './Members';

export function Invitation({
  scope,
  block,
}: {
  scope: Scope;
  block: (error: unknown) => void;
}) {
  const [subject, setSubject] = useState(''),
    [role, setRole] = useState<Member['role']>('viewer');
  const [token, setToken] = useState(''),
    [pending, setPending] = useState(false),
    [error, setError] = useState<unknown>(null);
  async function issue() {
    setToken('');
    setPending(true);
    setError(null);
    try {
      const result = await apiRequest<{ token: string }>(
        `${workspacePath(scope.workspace.id)}/invitations`,
        { method: 'POST', body: { subject, role } },
      );
      setToken(result.token);
      setSubject('');
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
        void issue();
      }}
    >
      <h2>Invite a member</h2>
      <p>
        Enter the exact subject from the configured identity provider. Share the
        one-use invitation manually; it expires after 24 hours.
      </p>
      <ProblemSummary error={error} />
      <fieldset disabled={pending}>
        <legend>New invitation</legend>
        <Field label="Identity provider subject">
          {(attributes) => (
            <input
              {...attributes}
              required
              maxLength={255}
              value={subject}
              onChange={(event) => setSubject(event.target.value)}
            />
          )}
        </Field>
        <RoleField
          value={role}
          owner={scope.workspace.role === 'owner'}
          change={setRole}
        />
        <button>Create invitation</button>
      </fieldset>
      {token && (
        <>
          <Field label="One-use invitation">
            {(attributes) => (
              <input
                {...attributes}
                readOnly
                value={token}
                autoComplete="off"
              />
            )}
          </Field>
          <button type="button" onClick={() => setToken('')}>
            Clear invitation
          </button>
        </>
      )}
    </form>
  );
}
