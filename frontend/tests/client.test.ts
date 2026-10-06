import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  jest,
} from '@jest/globals';
import { ApiError, apiRequest } from '../src/api/client';

const fetchMock = jest.fn<typeof fetch>();
function response(
  status: number,
  body: unknown = {},
  headers: Record<string, string> = {},
): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (key: string) => headers[key] ?? null },
    json: () => Promise.resolve(body),
  } as Response;
}
beforeEach(() => {
  Object.defineProperty(globalThis, 'fetch', {
    value: fetchMock,
    configurable: true,
  });
  fetchMock.mockReset();
  document.cookie = 'sama_csrf=synthetic-proof; path=/';
});
afterEach(() => {
  jest.useRealTimers();
});

describe('browser API transport', () => {
  it('uses same-origin credentials, CSRF and correlation without persisting secrets', async () => {
    fetchMock.mockResolvedValue(response(204));
    await apiRequest('/api/v1/workspaces', {
      method: 'POST',
      body: { name: 'Sky' },
    });
    const [url, options] = fetchMock.mock.calls[0]!;
    expect((url as URL).href).toBe(`${location.origin}/api/v1/workspaces`);
    expect(options).toMatchObject({
      credentials: 'same-origin',
      mode: 'same-origin',
      redirect: 'error',
      method: 'POST',
      headers: {
        'X-CSRF-Token': 'synthetic-proof',
        'Content-Type': 'application/json',
      },
      body: '{"name":"Sky"}',
    });
    expect(
      (options?.headers as Record<string, string>)['X-Request-ID'],
    ).toMatch(/^[a-z0-9-]+$/);
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });
  it('rejects external and non-API destinations before sending credentials', async () => {
    for (const path of [
      'https://evil.example/api/x',
      '//evil.example/api/x',
      '/auth/login',
      '/api/../../outside',
    ])
      await expect(apiRequest(path)).rejects.toThrow('Invalid API path.');
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it('retries a transient read once within its original deadline', async () => {
    fetchMock
      .mockRejectedValueOnce(new TypeError('private network detail'))
      .mockResolvedValueOnce(response(200, { name: 'Sky' }));
    await expect(apiRequest('/api/example')).resolves.toEqual({ name: 'Sky' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]![1]?.signal).toBe(
      fetchMock.mock.calls[1]![1]?.signal,
    );
  });
  it('never retries mutations or access, validation, conflict and rate-limit errors', async () => {
    for (const status of [400, 401, 403, 404, 409, 422, 429]) {
      fetchMock.mockClear().mockResolvedValue(response(status));
      await expect(apiRequest('/api/example')).rejects.toBeInstanceOf(ApiError);
      expect(fetchMock).toHaveBeenCalledTimes(1);
    }
    fetchMock.mockClear().mockRejectedValue(new TypeError('private'));
    await expect(
      apiRequest('/api/example', { method: 'PUT' }),
    ).rejects.toMatchObject({ code: 'network_error' });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    fetchMock
      .mockClear()
      .mockResolvedValue(response(503, {}, { 'Retry-After': '30' }));
    await expect(apiRequest('/api/example')).rejects.toMatchObject({
      retryAfterSeconds: 30,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it('bounds retries and strips raw messages and unknown field errors', async () => {
    fetchMock.mockResolvedValue(response(503));
    await expect(apiRequest('/api/example')).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    fetchMock.mockResolvedValue(
      response(
        422,
        {
          title: '<script>secret</script>',
          detail: 'secret',
          code: 'secret',
          errors: [
            { field: 'name', code: 'required', message: 'secret' },
            { field: 'token', code: 'invalid' },
            { field: 'name', code: 'secret' },
          ],
        },
        {
          'Content-Type': 'application/problem+json',
          'X-Request-ID': 'safe-id',
        },
      ),
    );
    try {
      await apiRequest('/api/example', { fields: ['name'] });
      throw new Error('expected rejection');
    } catch (error) {
      expect(error).toMatchObject({
        message: 'Check the form.',
        code: 'request_failed',
        requestId: 'safe-id',
        fieldErrors: [{ field: 'name', code: 'required' }],
      });
      expect(JSON.stringify(error)).not.toContain('secret');
    }
  });
  it('propagates caller cancellation and deadline expiry without retrying', async () => {
    fetchMock.mockImplementation(
      (_url, options) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener(
            'abort',
            () =>
              reject(
                options?.signal?.reason instanceof Error
                  ? options.signal.reason
                  : new DOMException('Cancelled', 'AbortError'),
              ),
            { once: true },
          );
        }),
    );
    const controller = new AbortController();
    const pending = apiRequest('/api/example', { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    await expect(
      apiRequest('/api/example', { timeoutMs: 10 }),
    ).rejects.toMatchObject({ name: 'TimeoutError' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await expect(
      apiRequest('/api/example', { signal: controller.signal }),
    ).rejects.toMatchObject({ name: 'AbortError' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
