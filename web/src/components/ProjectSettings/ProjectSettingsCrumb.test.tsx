import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { ProjectSettingsCrumb } from './ProjectSettingsCrumb';

describe('ProjectSettingsCrumb', () => {
  it('links the project segment back to the board and names the current page', () => {
    render(
      <MemoryRouter>
        <ProjectSettingsCrumb project="contextmatrix" />
      </MemoryRouter>
    );
    const link = screen.getByRole('link', { name: 'contextmatrix' });
    expect(link).toHaveAttribute('href', '/projects/contextmatrix');
    expect(screen.getByText('Settings')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /open menu/i })).toBeNull();
  });

  it('renders the sidebar menu button when onOpenSidebar is provided', () => {
    const onOpenSidebar = vi.fn();
    render(
      <MemoryRouter>
        <ProjectSettingsCrumb project="contextmatrix" onOpenSidebar={onOpenSidebar} />
      </MemoryRouter>
    );
    fireEvent.click(screen.getByRole('button', { name: /open menu/i }));
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);
  });
});
