import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { ApiError, apiRequest } from '../../api/client';
import { useAccessLoss } from '../../app/useAccessLoss';
import { ProblemSummary } from '../../components/Form';
import { path, unavailable, type Scope, type Schema } from '../connections/api';
import { ActionReview, AuthenticationNotice, recentAuth } from './ActionReview';

type Submission = {
  key: string;
  review: Schema['ActionReview'];
  typed: string;
};
export function ActionSubmission({
  scope,
  resource,
}: {
  scope: Scope;
  resource: Schema['ResourceDetail'];
}) {
  const [submission, setSubmission] = useState<Submission | null>(null);
  const [accepted, setAccepted] = useState<Schema['AcceptedOperation'] | null>(
    null,
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const request = useRef<AbortController | null>(null);
  const intent = useRef<Submission | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  useAccessLoss(recentAuth(error) ? null : error, scope.refreshAccess);
  async function submit(value: Submission) {
    if (request.current || accepted) return;
    const controller = new AbortController();
    request.current = controller;
    setPending(true);
    setError(null);
    try {
      const result = await apiRequest<Schema['AcceptedOperation']>(
        `${path(scope)}/operations`,
        {
          method: 'POST',
          idempotencyKey: value.key,
          signal: controller.signal,
          body: {
            review_id: value.review.id,
            confirmed: true,
            typed_target: value.typed,
          },
        },
      );
      if (!controller.signal.aborted) setAccepted(result);
    } catch (failure) {
      if (!controller.signal.aborted) setError(failure);
    } finally {
      if (!controller.signal.aborted) setPending(false);
      request.current = null;
    }
  }
  if (!submission)
    return (
      <ActionReview
        scope={scope}
        resource={resource}
        onConfirm={(review, typed) => {
          // Lock intent synchronously, before React can render a disabled button.
          if (intent.current) return;
          const value = { key: crypto.randomUUID(), review, typed };
          intent.current = value;
          setSubmission(value);
          void submit(value);
        }}
      />
    );
  return (
    <section aria-label="Operation acceptance">
      <h3>Operation acceptance</h3>
      <p>Reviewed target: {submission.review.target_name}</p>
      <ProblemSummary error={error} />
      <AuthenticationNotice error={error} />
      {accepted ? (
        <>
          <p role="status">
            Operation accepted: {accepted.state}. Acceptance does not prove
            provider completion.
          </p>
          <Link
            to={`/workspaces/${scope.workspace.id}/operations/${accepted.id}`}
          >
            Follow operation {accepted.id}
          </Link>
        </>
      ) : (
        <>
          <p role="status">
            {pending
              ? 'Confirming acceptance…'
              : 'Acceptance is unresolved. The original confirmation is retained in memory.'}
          </p>
          <p>
            Do not submit another intent. Recovery sends the same review and
            idempotency key; it cannot create a second operation.
          </p>
          <button
            disabled={
              pending ||
              unavailable(error) ||
              recentAuth(error) ||
              (error instanceof ApiError && error.status === 401)
            }
            onClick={() => void submit(submission)}
          >
            Recover original confirmation
          </button>
          <Link to={`/workspaces/${scope.workspace.id}/operations`}>
            Check operation history
          </Link>
        </>
      )}
    </section>
  );
}
