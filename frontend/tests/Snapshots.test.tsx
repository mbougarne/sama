import { beforeEach, expect, it } from '@jest/globals';
import { fireEvent, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import ResourcesPage from '../src/features/resources/ResourcesPage';
import { ResourceDetail } from '../src/features/resources/ResourceDetail';
import { ActionReview } from '../src/features/operations/ActionReview';
import { ActionSubmission } from '../src/features/operations/ActionSubmission';
import type { Schema } from '../src/features/connections/api';
import {
  setup,
  show,
  scope,
  fetchMock,
  response,
  requestURL,
} from './settings-fixture';

const snapshot: Schema['ResourceDetail'] = {
  id: scope.userId,
  connection_id: scope.userId,
  provider: 'contabo',
  api_family: 'contabo.compute',
  account_identity: 'account',
  type: 'storage.snapshot',
  scope: 'global',
  native_id: '42/snap42',
  name: 'Snapshot',
  region: '',
  status: 'unknown',
  native_status: '',
  observed_at: '2026-10-01T00:00:00Z',
  inventory: { state: 'stale', last_success_at: null, failure_code: null },
  details_version: 1,
  details: {
    snapshot: {
      source_native_id: '42',
      source_type: 'compute.server',
      status_reason: 'not_reported',
      created_at: null,
      size_bytes: null,
      size_gb: null,
      minimum_disk_gb: null,
      regions: [],
    },
  },
  available_actions: [
    {
      action: 'storage.snapshot.delete',
      available: false,
      reason: 'unverified',
    },
  ],
};
beforeEach(() => {
  setup();
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
  };
});
it('reads snapshot list filters from the URL without treating pending observations as usable', async () => {
  fetchMock.mockResolvedValue(
    response(200, {
      data: [{ ...snapshot, status: 'pending' }],
      next_cursor: null,
    }),
  );
  show(
    <MemoryRouter
      initialEntries={[
        '/resources?type=storage.snapshot&source_native_id=42&source_type=compute.server',
      ]}
    >
      <ResourcesPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByRole('link', { name: 'Snapshot' });
  expect(requestURL(fetchMock.mock.calls[0]![0])).toContain(
    'type=storage.snapshot&source_native_id=42&source_type=compute.server',
  );
  expect(screen.getByText(/Status: pending/)).toBeVisible();
  expect(
    screen.queryByText(/Status: available|Status: deleted/),
  ).not.toBeInTheDocument();
});
it('shows unknown metadata, unavailable reasons and an explicit snapshot refresh', async () => {
  fetchMock.mockImplementation((url, options) =>
    Promise.resolve(
      response(
        options?.method === 'POST' ? 202 : 200,
        requestURL(url).includes('/grants')
          ? { actions: ['read', 'refresh'] }
          : options?.method === 'POST'
            ? { sync_id: scope.userId }
            : requestURL(url).includes('/syncs/')
              ? {
                  status: 'failed',
                  page_count: 0,
                  failure_code: 'provider_unavailable',
                  inventory: snapshot.inventory,
                }
              : snapshot,
      ),
    ),
  );
  show(
    <MemoryRouter>
      <ResourceDetail scope={scope} id={snapshot.id} />
    </MemoryRouter>,
  );
  await screen.findByRole('heading', { name: 'Snapshot details' });
  expect(screen.getByText(/Snapshot readiness is unknown/)).toBeVisible();
  expect(
    screen.getByText('Size in bytes').nextElementSibling,
  ).toHaveTextContent('Not reported');
  expect(screen.getByText('Created').nextElementSibling).toHaveTextContent(
    'Not reported',
  );
  expect(screen.getByText(/42 \(compute.server\)/)).toBeVisible();
  expect(
    screen.getByText(/Provider permission has not been verified/),
  ).toBeVisible();
  expect(screen.getByRole('button', { name: 'Review action' })).toBeDisabled();
  const refresh = await screen.findByRole('button', {
    name: 'Refresh snapshot inventory',
  });
  expect(refresh).toBeEnabled();
  fireEvent.click(refresh);
  await screen.findByText(/Sync: failed/);
  const posts = fetchMock.mock.calls.filter(
    ([, options]) => options?.method === 'POST',
  );
  expect(posts).toHaveLength(1);
  expect(JSON.parse(posts[0]![1]!.body as string)).toEqual({
    resource_type: 'storage.snapshot',
  });
  expect(
    screen.queryByRole('heading', { name: 'Server details' }),
  ).not.toBeInTheDocument();
});
it.each(['compute.server.snapshot_create', 'storage.snapshot.delete'] as const)(
  'uses shared typed confirmation and durable operation tracking for %s',
  async (action) => {
    const resource: Schema['ResourceDetail'] = {
      ...snapshot,
      type:
        action === 'storage.snapshot.delete'
          ? 'storage.snapshot'
          : 'compute.server',
      available_actions: [{ action, available: true, reason: 'available' }],
    };
    const review = {
      id: scope.workspace.id,
      resource_id: resource.id,
      action,
      target_name: resource.name,
      high_impact: true,
      expires_at: new Date(Date.now() + 300_000).toISOString(),
    };
    fetchMock.mockImplementation((url) =>
      Promise.resolve(
        requestURL(url).endsWith('action-reviews')
          ? response(200, review)
          : response(202, { id: scope.userId, state: 'queued' }),
      ),
    );
    show(
      <MemoryRouter>
        <ActionSubmission scope={scope} resource={resource} />
      </MemoryRouter>,
    );
    expect(screen.getByRole('combobox')).toHaveValue(action);
    expect(screen.getAllByRole('option')).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'Review action' }));
    await screen.findByRole('dialog');
    expect(
      screen.getAllByText(
        action === 'storage.snapshot.delete'
          ? /Irreversibly delete this snapshot/
          : /Snapshot storage may incur provider charges/,
      ).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: resource.name },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await screen.findByRole('link', {
      name: `Follow operation ${scope.userId}`,
    });
    expect(JSON.parse(fetchMock.mock.calls[0]![1]!.body as string)).toEqual({
      action,
      parameters: {},
    });
    const accepted = fetchMock.mock.calls.find(([url]) =>
      requestURL(url).endsWith('/operations'),
    )!;
    expect(JSON.parse(accepted[1]!.body as string)).toEqual({
      review_id: review.id,
      confirmed: true,
      typed_target: resource.name,
    });
  },
);
it('stops on recent-authentication denial and rejects expired or low-impact snapshot reviews', async () => {
  const resource: Schema['ResourceDetail'] = {
    ...snapshot,
    available_actions: [
      {
        action: 'storage.snapshot.delete',
        available: true,
        reason: 'available',
      },
    ],
  };
  fetchMock.mockResolvedValue(
    response(403, { code: 'recent_authentication_required' }),
  );
  show(
    <ActionReview
      scope={scope}
      resource={resource}
      onConfirm={() => {
        throw new Error('invalid snapshot review confirmed');
      }}
    />,
  );
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByText(/Sign out and sign in again/);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const review = {
    id: scope.workspace.id,
    resource_id: resource.id,
    action: 'storage.snapshot.delete',
    target_name: 'Snapshot',
    high_impact: true,
    expires_at: '2000-01-01T00:00:00Z',
  };
  fetchMock.mockResolvedValue(response(200, review));
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByText(/Review expired/);
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  fetchMock.mockResolvedValue(response(200, { ...review, high_impact: false }));
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByRole('alert');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledTimes(3);
});

it('dismisses a snapshot review without submission and discards typed confirmation', async () => {
  const resource: Schema['ResourceDetail'] = {
    ...snapshot,
    available_actions: [
      {
        action: 'storage.snapshot.delete',
        available: true,
        reason: 'available',
      },
    ],
  };
  const review = {
    id: scope.workspace.id,
    resource_id: resource.id,
    action: 'storage.snapshot.delete',
    target_name: 'Snapshot',
    high_impact: true,
    expires_at: new Date(Date.now() + 300_000).toISOString(),
  };
  fetchMock.mockResolvedValue(response(200, review));
  show(
    <MemoryRouter>
      <ActionSubmission scope={scope} resource={resource} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByText('Review action'));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(screen.getByRole('textbox'), {
    target: { value: 'Snapshot' },
  });
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled();
  fireEvent.keyDown(dialog, { key: 'Escape' });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  fireEvent.click(screen.getByText('Review action'));
  await screen.findByRole('dialog');
  expect(screen.getByRole('textbox')).toHaveValue('');
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(fetchMock.mock.calls).toHaveLength(2);
  expect(
    fetchMock.mock.calls.every(([url]) =>
      requestURL(url).endsWith('action-reviews'),
    ),
  ).toBe(true);
});
