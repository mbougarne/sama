import type { components } from './generated/openapi';

type Problem = components['schemas']['Problem'];
export type User = components['schemas']['User'];
export type WorkspacePage = components['schemas']['WorkspacePage'];
export type FieldError = {
  field: string;
  code: 'required' | 'invalid' | 'out_of_range';
};
type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
export type RequestOptions = {
  method?: Method;
  body?: unknown;
  signal?: AbortSignal | undefined;
  timeoutMs?: number;
  fields?: readonly string[];
};

const titles: Record<number, string> = {
  400: 'Check the request.',
  401: 'Your session has expired.',
  403: 'Access denied.',
  404: 'Not found.',
  409: 'The information has changed.',
  422: 'Check the form.',
  429: 'Too many requests. Try again later.',
};
const knownCodes: readonly Problem['code'][] = [
  'permission_denied',
  'not_found',
  'invalid_request',
  'membership_conflict',
  'policy_conflict',
  'recent_authentication_required',
  'reauthentication_unsupported',
  'rate_limited',
  'identity_unavailable',
  'unauthenticated',
];
const correlation = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly requestId: string,
    readonly fieldErrors: FieldError[] = [],
    readonly retryAfterSeconds?: number,
  ) {
    super(titles[status] ?? 'The request could not be completed.');
  }
}

function csrfToken(): string {
  const name =
    window.location.protocol === 'https:' ? '__Host-sama_csrf' : 'sama_csrf';
  return (
    document.cookie
      .split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith(`${name}=`))
      ?.slice(name.length + 1) ?? ''
  );
}
function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}
async function problem(
  response: Response,
  requestId: string,
  fields: readonly string[],
): Promise<ApiError> {
  let data: Record<string, unknown> = {};
  if (
    response.headers.get('Content-Type')?.split(';')[0] ===
    'application/problem+json'
  ) {
    try {
      data = record(await response.json());
    } catch {
      /* Keep the safe local message. */
    }
  }
  const code =
    knownCodes.find((value) => value === data.code) ?? 'request_failed';
  const headerId = response.headers.get('X-Request-ID') ?? '';
  const errors: FieldError[] = [];
  if (Array.isArray(data.errors)) {
    for (const item of data.errors.slice(0, 20)) {
      const value = record(item);
      if (
        typeof value.field === 'string' &&
        fields.includes(value.field) &&
        (value.code === 'required' ||
          value.code === 'invalid' ||
          value.code === 'out_of_range')
      )
        errors.push({ field: value.field, code: value.code });
    }
  }
  const retry = response.headers.get('Retry-After') ?? '';
  const seconds = /^\d{1,4}$/.test(retry)
    ? Math.min(Number(retry), 3600)
    : undefined;
  return new ApiError(
    response.status,
    code,
    correlation.test(headerId) ? headerId : requestId,
    errors,
    seconds,
  );
}
function pause(signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer);
      reject(
        signal.reason instanceof Error
          ? signal.reason
          : new DOMException('Request cancelled.', 'AbortError'),
      );
    };
    const timer = setTimeout(
      () => {
        signal.removeEventListener('abort', abort);
        resolve();
      },
      100 + Math.random() * 100,
    );
    signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) abort();
  });
}

// Each call has one shared deadline, including its single optional read retry.
export async function apiRequest<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const url = new URL(path, window.location.origin);
  if (
    !path.startsWith('/') ||
    path.startsWith('//') ||
    url.origin !== window.location.origin ||
    url.hash ||
    !(
      url.pathname.startsWith('/api/') ||
      url.pathname === '/auth/logout' ||
      (url.pathname === '/auth/login' && options.method === 'POST')
    )
  )
    throw new Error('Invalid API path.');
  const method = options.method ?? 'GET';
  const timeout = options.timeoutMs ?? 10_000;
  if (!Number.isFinite(timeout) || timeout < 1 || timeout > 30_000)
    throw new Error('Invalid request deadline.');
  const controller = new AbortController();
  const abort = () => controller.abort(options.signal?.reason);
  options.signal?.addEventListener('abort', abort, { once: true });
  if (options.signal?.aborted) abort();
  const timer = setTimeout(
    () =>
      controller.abort(new DOMException('Request timed out.', 'TimeoutError')),
    timeout,
  );
  const requestId = crypto.randomUUID();
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'X-Request-ID': requestId,
  };
  if (method !== 'GET') {
    headers['Content-Type'] = 'application/json';
    headers['X-CSRF-Token'] = csrfToken();
  }
  try {
    for (let attempt = 0; ; attempt++) {
      controller.signal.throwIfAborted();
      try {
        const response = await fetch(url, {
          method,
          headers,
          credentials: 'same-origin',
          mode: 'same-origin',
          redirect: 'error',
          cache: 'no-store',
          signal: controller.signal,
          body: method === 'GET' ? null : JSON.stringify(options.body ?? {}),
        });
        if (!response.ok) {
          const error = await problem(
            response,
            requestId,
            options.fields ?? [],
          );
          if (response.status === 401)
            window.dispatchEvent(new Event('sama:unauthenticated'));
          throw error;
        }
        const result: unknown =
          response.status === 204 ? undefined : await response.json();
        controller.signal.throwIfAborted();
        return result as T;
      } catch (error) {
        controller.signal.throwIfAborted();
        const transient =
          error instanceof TypeError ||
          (error instanceof ApiError &&
            error.status >= 500 &&
            error.retryAfterSeconds === undefined);
        if (method !== 'GET' || attempt > 0 || !transient)
          throw error instanceof ApiError || error instanceof DOMException
            ? error
            : new ApiError(0, 'network_error', requestId);
        await pause(controller.signal);
      }
    }
  } finally {
    clearTimeout(timer);
    options.signal?.removeEventListener('abort', abort);
  }
}
export const getMe = (signal?: AbortSignal) =>
  apiRequest<User>('/api/v1/me', { signal });
export const getWorkspaces = (cursor?: string, signal?: AbortSignal) =>
  apiRequest<WorkspacePage>(
    `/api/v1/workspaces?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
    { signal },
  );
export const logout = () =>
  apiRequest<void>('/auth/logout', { method: 'POST' });
