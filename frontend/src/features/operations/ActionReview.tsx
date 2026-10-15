import { useEffect, useReducer, useRef, useState } from 'react';
import { ApiError, apiRequest } from '../../api/client';
import { useAccessLoss } from '../../app/useAccessLoss';
import { Field, ProblemSummary } from '../../components/Form';
import { RiskDialog } from '../../components/RiskDialog';
import { path, type Scope, type Schema } from '../connections/api';

type Review = Schema['ActionReview'];
type Action = Schema['PowerIntent']['action'];
const impacts: Record<Action, string> = {
  'compute.server.power_on': 'Start this server. Provider charges may apply.',
  'compute.server.shutdown': 'Request a graceful shutdown. Services will stop.',
  'compute.server.power_off': 'Force power off. Unsaved data may be lost.',
  'compute.server.reboot': 'Restart this server. Services will be interrupted.',
};
type State =
  | { stage: 'draft' | 'reviewing'; action: Action; error?: unknown }
  | { stage: 'reviewed'; action: Action; review: Review };
type Event =
  | { type: 'edit'; action: Action }
  | { type: 'prepare' }
  | { type: 'review'; review: Review }
  | { type: 'discard'; error?: unknown };
function reducer(state: State, event: Event): State {
  switch (event.type) {
    case 'edit':
      return { stage: 'draft', action: event.action };
    case 'prepare':
      return { stage: 'reviewing', action: state.action };
    case 'review':
      return { stage: 'reviewed', action: state.action, review: event.review };
    case 'discard':
      return { stage: 'draft', action: state.action, error: event.error };
  }
}
export const recentAuth = (error: unknown) =>
  error instanceof ApiError && error.code === 'recent_authentication_required';
export function AuthenticationNotice({ error }: { error: unknown }) {
  return recentAuth(error) ? (
    <p role="alert">
      Recent authentication is required. Sign out and sign in again, then review
      the action again. No automatic retry will occur.
    </p>
  ) : null;
}
export function ActionReview({
  scope,
  resource,
  onConfirm,
}: {
  scope: Scope;
  resource: Schema['ResourceDetail'];
  onConfirm?: (review: Review, typedTarget: string) => void;
}) {
  const [state, dispatch] = useReducer(reducer, {
    stage: 'draft',
    action: 'compute.server.power_on',
  });
  const [now, setNow] = useState(Date.now);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const review = state.stage === 'reviewed' ? state.review : null;
  useEffect(() => {
    if (!review) return;
    const timer = setTimeout(
      () => setNow(Date.now()),
      Math.max(0, Date.parse(review.expires_at) - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [review]);
  const error = 'error' in state ? state.error : null;
  useAccessLoss(recentAuth(error) ? null : error, scope.refreshAccess);
  const allowed =
    scope.workspace.role !== 'viewer' &&
    resource.available_actions.some(
      (value) => value.action === state.action && value.available,
    );
  async function prepare() {
    if (!allowed || request.current) return;
    const controller = new AbortController();
    request.current = controller;
    dispatch({ type: 'prepare' });
    try {
      const result = await apiRequest<Review>(
        `${path(scope)}/resources/${resource.id}/action-reviews`,
        {
          method: 'POST',
          body: {
            action: state.action,
            parameters: {},
          } satisfies Schema['PowerIntent'],
          signal: controller.signal,
        },
      );
      if (result.resource_id !== resource.id || result.action !== state.action)
        throw new Error('Review does not match the requested intent.');
      if (!controller.signal.aborted)
        dispatch({ type: 'review', review: result });
    } catch (failure) {
      if (!controller.signal.aborted)
        dispatch({ type: 'discard', error: failure });
    } finally {
      if (request.current === controller) request.current = null;
    }
  }
  return (
    <section aria-label="Review resource action">
      <h3>Review resource action</h3>
      <ProblemSummary error={error} />
      <AuthenticationNotice error={error} />
      <Field label="Power action">
        {(attrs) => (
          <select
            {...attrs}
            value={state.action}
            onChange={(event) => {
              request.current?.abort();
              request.current = null;
              dispatch({ type: 'edit', action: event.target.value as Action });
            }}
          >
            {Object.keys(impacts).map((action) => (
              <option key={action} value={action}>
                {action}
              </option>
            ))}
          </select>
        )}
      </Field>
      <p>Price: unknown. Check provider pricing; no spending guarantee.</p>
      {!allowed && (
        <p>
          This action requires an available capability and an operator role.
        </p>
      )}
      <button
        disabled={!allowed || state.stage === 'reviewing'}
        onClick={() => void prepare()}
      >
        {state.stage === 'reviewing' ? 'Preparing review…' : 'Review action'}
      </button>
      {review && (
        <RiskDialog
          key={review.id}
          title="Confirm resource action"
          target={review.target_name}
          impact={`${resource.provider} · ${resource.account_identity} · ${resource.region || 'Region unknown'}. ${impacts[state.action]} Price: unknown.`}
          typedTarget={review.high_impact}
          evidenceValid={
            allowed && !!onConfirm && Date.parse(review.expires_at) > now
          }
          pending={false}
          onCancel={() => dispatch({ type: 'discard' })}
          onConfirm={(typed) => {
            if (Date.parse(review.expires_at) > Date.now())
              onConfirm?.(review, typed);
            else dispatch({ type: 'discard' });
          }}
        />
      )}
      {review && Date.parse(review.expires_at) <= now && (
        <p>Review expired. Prepare a fresh review.</p>
      )}
    </section>
  );
}
