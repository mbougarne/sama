import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { useAccessLoss } from '../../app/useAccessLoss';
import { apiRequest, ApiError } from '../../api/client';
import { ProblemSummary } from '../../components/Form';
import { RiskDialog } from '../../components/RiskDialog';
import { CredentialFields, credential } from './Onboarding';
import { path, unavailable, type Scope, type Schema } from './api';

export function ConnectionControls({
  scope,
  row,
}: {
  scope: Scope;
  row: Schema['ConnectionView'];
}) {
  const client = useQueryClient();
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [disable, setDisable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  useAccessLoss(error, scope.refreshAccess);
  if (!['owner', 'admin'].includes(scope.workspace.role))
    return <p>Rotation and disabling require an owner or admin.</p>;
  if (unavailable(error))
    return (
      <>
        <ProblemSummary error={error} />
        <button onClick={scope.refreshAccess}>Refresh workspace access</button>
      </>
    );
  async function submit(disabling: boolean) {
    if (busy) return;
    setBusy(true);
    setError(null);
    const body = disabling
      ? {}
      : { credential: credential(row.api_family, secrets) };
    setSecrets({});
    const controller = new AbortController();
    request.current = controller;
    try {
      await apiRequest<unknown>(
        `${path(scope)}/connections/${row.id}/${disabling ? 'disable' : 'credential-rotations'}`,
        { method: 'POST', body, signal: controller.signal },
      );
      setDisable(false);
      await client.invalidateQueries();
    } catch (failure) {
      if (!controller.signal.aborted) {
        setError(failure);
        if (unavailable(failure)) scope.refreshAccess();
      }
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <ProblemSummary error={error} />
      {error instanceof ApiError && error.code === 'unsupported_capability' && (
        <p>Credential validation is not available in this integration.</p>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit(false);
        }}
      >
        <fieldset disabled={busy}>
          <legend>Rotate write-only credential</legend>
          <p>
            Rotation validates the submitted credential against this account.
            Existing accepted operations remain visible.
          </p>
          <CredentialFields
            family={row.api_family}
            secrets={secrets}
            edit={(field, value) => setSecrets({ ...secrets, [field]: value })}
          />
          <button>Rotate credential</button>
        </fieldset>
      </form>
      <button
        disabled={busy || row.status === 'disabled'}
        onClick={() => setDisable(true)}
      >
        Disable connection
      </button>
      {disable && (
        <RiskDialog
          title="Disable connection"
          target={`${row.label} (${row.id})`}
          impact="Stops new work in Sama. It does not revoke the provider credential or undo submitted operations; accepted work remains visible."
          evidenceValid={row.status === 'active'}
          pending={busy}
          error={error}
          onCancel={() => setDisable(false)}
          onConfirm={() => void submit(true)}
        />
      )}
    </>
  );
}
