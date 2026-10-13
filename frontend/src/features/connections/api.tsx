import type { components } from '../../api/generated/openapi';
import { ApiError } from '../../api/client';
import type { Scope } from '../settings/Members';
export type { Scope };
export type Schema = components['schemas'];
export const path = (scope: Scope) =>
  `/api/v1/workspaces/${encodeURIComponent(scope.workspace.id)}`;
export const unavailable = (error: unknown) =>
  error instanceof ApiError && [403, 404].includes(error.status);
const reasons: Record<Schema['Capability']['reason'], string> = {
  available: 'Available',
  unsupported: 'Not available in this integration',
  unverified: 'Provider permission has not been verified',
  denied: 'Your connection grant does not allow this action',
  state_restricted: 'Unavailable in the current resource or connection state',
};
export function Capabilities({ values }: { values: Schema['Capabilities'] }) {
  return (
    <ul>
      {values.map((value) => (
        <li key={value.action}>
          {value.action}: {reasons[value.reason]}
        </li>
      ))}
    </ul>
  );
}
export function Freshness({ value }: { value: Schema['InventoryFreshness'] }) {
  return (
    <p>
      Inventory: {value.state.replaceAll('_', ' ')} · Last successful provider
      observation: {value.last_success_at ?? 'Never'}
      {value.failure_code && ` · Last failure: ${value.failure_code}`}
    </p>
  );
}
export const families = [
  {
    id: 'hetzner.cloud',
    name: 'Hetzner · Cloud API v1',
    scope: 'Project token with Read permission',
    fields: ['token'],
  },
  {
    id: 'digitalocean.platform',
    name: 'DigitalOcean · Platform API v2',
    scope: 'Read access to account and droplets',
    fields: ['token'],
  },
  {
    id: 'vultr.compute',
    name: 'Vultr · Compute API v2',
    scope: 'Read access to account and instances',
    fields: ['token'],
  },
  {
    id: 'contabo.compute',
    name: 'Contabo · Compute Management API v1',
    scope: 'OAuth client and account credentials with compute read access',
    fields: ['client_id', 'client_secret', 'username', 'password'],
  },
] as const;
