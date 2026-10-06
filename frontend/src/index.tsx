import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App';
import { Providers } from './app/providers';
import './styles.css';
import { SessionGate } from './features/identity/SessionGate';

const root = document.getElementById('root');
if (!root) throw new Error('Missing application root');

createRoot(root).render(
  <StrictMode>
    <Providers>
      <SessionGate>{() => <App />}</SessionGate>
    </Providers>
  </StrictMode>,
);
