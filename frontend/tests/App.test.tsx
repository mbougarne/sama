import { expect, it } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { App } from '../src/app/App';

it('keeps unknown routes outside workspace views', () => {
  render(
    <MemoryRouter initialEntries={['/missing-page']}>
      <App
        user={{
          id: '01900000-0000-7000-8000-000000000001',
          display_name: 'Owner',
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  expect(
    screen.getByRole('link', { name: 'Return to overview' }),
  ).toHaveAttribute('href', '/');
  expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
});
