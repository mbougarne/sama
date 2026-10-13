import { requestURL } from './settings-fixture';
import { beforeEach, expect, it } from '@jest/globals';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ResourcesPage from '../src/features/resources/ResourcesPage';
import { setup, show, scope, fetchMock, response } from './settings-fixture';
beforeEach(setup);
export const resource = {
  id: scope.userId,
  connection_id: scope.userId,
  provider: 'hetzner',
  api_family: 'hetzner.cloud',
  account_identity: 'project',
  type: 'compute.server' as const,
  scope: 'global',
  native_id: '42',
  name: '<Server>',
  region: 'fsn1',
  status: 'unknown' as const,
  native_status: 'future-status',
  observed_at: '2026-10-01T00:00:00Z',
  inventory: {
    state: 'stale' as const,
    last_success_at: '2026-10-01T00:00:00Z',
    failure_code: null,
  },
};
it('owns filters and opaque paging in the URL and displays provider observation time', async () => {
  fetchMock.mockImplementation((url) =>
    Promise.resolve(
      response(200, {
        data: [resource],
        next_cursor: requestURL(url).includes('cursor=') ? null : 'opaque-next',
      }),
    ),
  );
  show(
    <MemoryRouter initialEntries={['/resources?provider=hetzner&region=fsn1']}>
      <ResourcesPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByRole('link', { name: '<Server>' });
  expect(screen.getByLabelText('provider')).toHaveValue('hetzner');
  expect(
    screen.getByText('Provider observation: 2026-10-01T00:00:00Z'),
  ).toBeVisible();
  await userEvent.click(
    screen.getByRole('button', { name: 'Next resource page' }),
  );
  await screen.findByRole('button', { name: 'First page' });
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).toContain(
    'provider=hetzner&region=fsn1&cursor=opaque-next',
  );
  await userEvent.clear(screen.getByLabelText('region'));
  await userEvent.type(screen.getByLabelText('region'), 'nbg1');
  await userEvent.click(screen.getByRole('button', { name: 'Apply filters' }));
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).toContain(
    'provider=hetzner&region=nbg1',
  );
  expect(requestURL(fetchMock.mock.calls.at(-1)![0])).not.toContain('cursor=');
});
it('distinguishes failure from empty observations', async () => {
  fetchMock.mockResolvedValue(response(400, { code: 'invalid_request' }));
  show(
    <MemoryRouter>
      <ResourcesPage {...scope} />
    </MemoryRouter>,
  );
  await screen.findByRole('alert');
  expect(
    screen.queryByText(/No resource observations/),
  ).not.toBeInTheDocument();
});
