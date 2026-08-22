import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ProjectCrumb } from './ProjectCrumb';

describe('ProjectCrumb', () => {
  it('with a current segment, links the project back to the board and names the page', () => {
    render(
      <MemoryRouter>
        <ProjectCrumb project="contextmatrix" current="Settings" />
      </MemoryRouter>
    );
    const link = screen.getByRole('link', { name: 'contextmatrix' });
    expect(link).toHaveAttribute('href', '/projects/contextmatrix');
    expect(screen.getByText('Settings')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /open menu/i })).toBeNull();
  });

  it('without a current segment, the project is the current page (no link)', () => {
    render(
      <MemoryRouter>
        <ProjectCrumb project="contextmatrix" />
      </MemoryRouter>
    );
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText('contextmatrix')).toBeInTheDocument();
  });

  it('renders the sidebar menu button when onOpenSidebar is provided', () => {
    const onOpenSidebar = vi.fn();
    render(
      <MemoryRouter>
        <ProjectCrumb project="contextmatrix" current="Settings" onOpenSidebar={onOpenSidebar} />
      </MemoryRouter>
    );
    fireEvent.click(screen.getByRole('button', { name: /open menu/i }));
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);
  });
});
