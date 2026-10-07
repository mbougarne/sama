import { useState } from 'react';
import { apiRequest } from '../../api/client';
import { Field, ProblemSummary } from '../../components/Form';

export function InvitationEntry() {
  const [invitation, setInvitation] = useState('');
  const [pending, setPending] = useState(false),
    [error, setError] = useState<unknown>(null);
  async function start() {
    const proof = invitation;
    setInvitation('');
    setPending(true);
    setError(null);
    try {
      const result = await apiRequest<{ authorization_url: string }>(
        '/auth/login',
        { method: 'POST', body: { invitation: proof } },
      );
      const destination = new URL(result.authorization_url);
      if (
        destination.protocol !== 'https:' &&
        !(
          destination.protocol === 'http:' &&
          ['127.0.0.1', 'localhost', '[::1]'].includes(destination.hostname)
        )
      )
        throw new Error('Invalid login destination');
      window.location.assign(destination.href);
    } catch (failure) {
      setError(failure);
      setPending(false);
    }
  }
  return (
    <form
      className="settings-section"
      onSubmit={(event) => {
        event.preventDefault();
        void start();
      }}
    >
      <h2>Use an invitation</h2>
      <ProblemSummary error={error} />
      <Field
        label="Invitation code"
        hint="Sign in with the exact invited identity."
      >
        {(attributes) => (
          <input
            {...attributes}
            type="password"
            required
            minLength={43}
            maxLength={43}
            autoComplete="off"
            value={invitation}
            disabled={pending}
            onChange={(event) => setInvitation(event.target.value)}
          />
        )}
      </Field>
      <button disabled={pending}>Continue with invitation</button>
    </form>
  );
}
