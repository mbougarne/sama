import { useEffect, useReducer, useRef } from 'react';
import { apiRequest, ApiError } from '../../api/client';
import { Field, ProblemSummary } from '../../components/Form';
import { Capabilities, families, path, type Schema, type Scope } from './api';
import { Grants } from './Grants';

type State = {
  family: string;
  label: string;
  secrets: Record<string, string>;
  phase: 'draft' | 'testing' | 'saving' | 'preview' | 'saved' | 'error';
  preview?: Schema['ConnectionPreview'] | undefined;
  saved?: Schema['SavedConnection'] | undefined;
  error?: unknown;
};
type Event =
  | { type: 'edit'; field: string; value: string }
  | { type: 'begin'; save: boolean }
  | { type: 'preview'; value: Schema['ConnectionPreview'] }
  | { type: 'saved'; value: Schema['SavedConnection'] }
  | { type: 'error'; error: unknown };
const initial: State = {
  family: families[0].id,
  label: '',
  secrets: {},
  phase: 'draft',
};
export function onboarding(state: State, event: Event): State {
  switch (event.type) {
    case 'edit':
      return event.field === 'family'
        ? { ...initial, label: state.label, family: event.value }
        : event.field === 'label'
          ? { ...state, label: event.value }
          : {
              ...state,
              secrets: { ...state.secrets, [event.field]: event.value },
              preview: undefined,
              saved: undefined,
              error: undefined,
              phase: 'draft',
            };
    case 'begin':
      return {
        ...state,
        secrets: {},
        preview: undefined,
        saved: undefined,
        error: undefined,
        phase: event.save ? 'saving' : 'testing',
      };
    case 'preview':
      return { ...state, preview: event.value, phase: 'preview' };
    case 'saved':
      return { ...state, saved: event.value, phase: 'saved' };
    case 'error':
      return { ...state, error: event.error, phase: 'error' };
  }
}
export function CredentialFields({
  family,
  secrets,
  edit,
}: {
  family: string;
  secrets: Record<string, string>;
  edit: (field: string, value: string) => void;
}) {
  const spec = families.find((value) => value.id === family);
  if (!spec)
    return <p>Credential entry is not available for this API family.</p>;
  return (
    <>
      {spec.fields.map((field) => (
        <Field key={field} label={field.replaceAll('_', ' ')}>
          {(attrs) => (
            <input
              {...attrs}
              type="password"
              required
              maxLength={4096}
              autoComplete="new-password"
              value={secrets[field] ?? ''}
              onChange={(event) => edit(field, event.target.value)}
            />
          )}
        </Field>
      ))}
    </>
  );
}
export function credential(
  family: string,
  secrets: Record<string, string>,
): Schema['WriteCredential'] {
  return family === 'contabo.compute'
    ? {
        type: 'contabo_v1',
        client_id: secrets.client_id ?? '',
        client_secret: secrets.client_secret ?? '',
        username: secrets.username ?? '',
        password: secrets.password ?? '',
      }
    : { type: 'bearer_v1', token: secrets.token ?? '' };
}
export default function Onboarding(scope: Scope) {
  const [state, dispatch] = useReducer(onboarding, initial);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const busy = state.phase === 'testing' || state.phase === 'saving';
  const spec = families.find((value) => value.id === state.family)!;
  async function submit(save: boolean) {
    if (busy) return;
    const controller = new AbortController();
    request.current = controller;
    const body = {
      family: state.family,
      credential: credential(state.family, state.secrets),
      ...(save ? { label: state.label } : {}),
    };
    dispatch({ type: 'begin', save });
    try {
      if (save) {
        const value = await apiRequest<Schema['SavedConnection']>(
          `${path(scope)}/connections`,
          { method: 'POST', body, signal: controller.signal },
        );
        dispatch({ type: 'saved', value });
      } else {
        const value = await apiRequest<Schema['ConnectionPreview']>(
          `${path(scope)}/connection-validations`,
          { method: 'POST', body, signal: controller.signal },
        );
        dispatch({ type: 'preview', value });
      }
    } catch (error) {
      if (!controller.signal.aborted) dispatch({ type: 'error', error });
    }
  }
  return (
    <section className="settings-section">
      <h2>Connect an account</h2>
      <p>
        Test connection is a read-only preview. Save revalidates the current
        credential. Secrets clear on every submission; re-enter them to save
        after a test.
      </p>
      <p>
        Provider integrations require qualification before they can be enabled.
      </p>
      <ProblemSummary error={state.error} />
      {state.error instanceof ApiError &&
        state.error.code === 'unsupported_capability' && (
          <p>
            Not available in this integration until provider qualification is
            complete.
          </p>
        )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit(true);
        }}
      >
        <fieldset disabled={busy}>
          <legend>Connection credential</legend>
          <Field label="Provider and API family">
            {(attrs) => (
              <select
                {...attrs}
                value={state.family}
                onChange={(event) =>
                  dispatch({
                    type: 'edit',
                    field: 'family',
                    value: event.target.value,
                  })
                }
              >
                {families.map((family) => (
                  <option key={family.id} value={family.id}>
                    {family.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <p>Required scope: {spec.scope}</p>
          <Field label="Connection label">
            {(attrs) => (
              <input
                {...attrs}
                required
                maxLength={100}
                value={state.label}
                onChange={(event) =>
                  dispatch({
                    type: 'edit',
                    field: 'label',
                    value: event.target.value,
                  })
                }
              />
            )}
          </Field>
          <CredentialFields
            family={state.family}
            secrets={state.secrets}
            edit={(field, value) => dispatch({ type: 'edit', field, value })}
          />
          <button
            type="button"
            disabled={!spec.fields.every((field) => state.secrets[field])}
            onClick={() => void submit(false)}
          >
            Test connection
          </button>{' '}
          <button>Save connection</button>
        </fieldset>
      </form>
      {busy && (
        <p role="status">
          {state.phase === 'saving' ? 'Saving' : 'Testing'} connection…
        </p>
      )}
      {state.preview && (
        <div>
          <h3>Preview · verified account {state.preview.account_identity}</h3>
          <p>
            Verified at {state.preview.verified_at}. This preview does not
            authorize Save.
          </p>
          <Capabilities values={state.preview.capabilities} />
        </div>
      )}
      {state.saved && (
        <div role="status">
          <h3>Saved · verified account {state.saved.account_identity}</h3>
          <Capabilities values={state.saved.capabilities} />
          <p>Initial sync: {state.saved.sync_id}</p>
          <Grants scope={scope} id={state.saved.id} />
        </div>
      )}
    </section>
  );
}
