import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type ReactNode } from 'react';
import { ApiError, getMe, logout, type User } from '../../api/client';
import { QueryScope } from '../../app/QueryScope';

export function SessionGate({
  children,
}: {
  children: (user: User) => ReactNode;
}) {
  const client = useQueryClient();
  const [ended, setEnded] = useState<'initial' | 'expired' | 'logout' | null>(
    null,
  );
  const [pending, setPending] = useState(false);
  const [logoutError, setLogoutError] = useState(false);
  const session = useQuery({
    queryKey: ['session'],
    queryFn: ({ signal }) => getMe(signal),
    enabled: !ended,
    retry: false,
    staleTime: 0,
  });
  useEffect(() => {
    const expire = () => {
      setEnded(session.data ? 'expired' : 'initial');
      void client.cancelQueries();
      client.clear();
    };
    window.addEventListener('sama:unauthenticated', expire);
    return () => window.removeEventListener('sama:unauthenticated', expire);
  }, [client, session.data]);
  async function signOut() {
    setPending(true);
    setLogoutError(false);
    try {
      await logout();
      setEnded('logout');
      await client.cancelQueries();
      client.clear();
    } catch {
      setLogoutError(true);
    } finally {
      setPending(false);
    }
  }
  if (
    ended ||
    (session.error instanceof ApiError && session.error.status === 401)
  )
    return (
      <main id="main-content">
        <h1>
          {ended === 'expired' || ended === 'logout'
            ? 'Session ended'
            : 'Sign in to Sama'}
        </h1>
        <p>Sign in to access your workspaces.</p>
        <a href="/auth/login">Sign in</a>
      </main>
    );
  if (session.isPending || session.isFetching)
    return (
      <main>
        <p role="status">Checking your session…</p>
      </main>
    );
  if (session.error)
    return (
      <main>
        <h1>
          {session.error instanceof ApiError && session.error.status === 403
            ? 'Access denied'
            : 'Unable to check your session'}
        </h1>
        <p>No workspace data is shown.</p>
        <button onClick={() => void session.refetch()}>Try again</button>
      </main>
    );
  return (
    <>
      <header className="session-bar">
        <span>{session.data.display_name || 'Signed in'}</span>
        <button disabled={pending} onClick={() => void signOut()}>
          {pending ? 'Signing out…' : 'Sign out'}
        </button>
        {logoutError && (
          <p role="alert">Sign out could not be confirmed. Try again.</p>
        )}
      </header>
      <QueryScope userId={session.data.id}>{children(session.data)}</QueryScope>
    </>
  );
}
