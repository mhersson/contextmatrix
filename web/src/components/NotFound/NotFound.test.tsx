import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { NotFound } from './NotFound';

function renderNotFound() {
  return render(
    <MemoryRouter>
      <NotFound />
    </MemoryRouter>
  );
}

describe('NotFound', () => {
  it('renders a "Page not found" heading', () => {
    renderNotFound();
    expect(screen.getByRole('heading', { name: /page not found/i })).toBeInTheDocument();
  });

  it('renders the 404 indicator', () => {
    renderNotFound();
    expect(screen.getByText('404')).toBeInTheDocument();
  });

  it('renders a "Go home" link that points to "/"', () => {
    renderNotFound();
    const link = screen.getByRole('link', { name: /go home/i });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/');
  });

  it('renders a descriptive message', () => {
    renderNotFound();
    expect(screen.getByText(/doesn't exist or has been moved/i)).toBeInTheDocument();
  });
});
