import { NavLink, Route, Routes } from 'react-router';
import { OverviewPage } from '../features/overview/OverviewPage';

export function App() {
  return (
    <div className="backoffice">
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <aside className="sidebar" aria-label="Backoffice navigation">
        <div className="brand">
          <strong>Sama</strong>
          <span lang="ar" dir="rtl">
            سماء
          </span>
        </div>
        <p className="muted">Cloud backoffice</p>
        <nav aria-label="Main">
          <NavLink to="/" end>
            Overview
          </NavLink>
        </nav>
      </aside>
      <main id="main-content" tabIndex={-1}>
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route
            path="*"
            element={
              <section>
                <h1>Page not found</h1>
                <NavLink to="/">Return to overview</NavLink>
              </section>
            }
          />
        </Routes>
      </main>
    </div>
  );
}
