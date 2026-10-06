import { NavLink, Route, Routes, useLocation } from 'react-router';
import type { User } from '../api/client';
import { WorkspaceShell } from '../features/workspaces/WorkspaceShell';

export function App({ user }: { user: User }) {
  const { pathname } = useLocation();
  // Remount on URL changes so stale membership is revalidated before a new view.
  const workspace = <WorkspaceShell key={pathname} user={user} />;
  return (
    <Routes>
      <Route path="/" element={workspace} />
      <Route path="/workspaces/:workspaceId/:section?" element={workspace} />
      <Route
        path="*"
        element={
          <main>
            <h1>Page not found</h1>
            <NavLink to="/">Return to overview</NavLink>
          </main>
        }
      />
    </Routes>
  );
}
