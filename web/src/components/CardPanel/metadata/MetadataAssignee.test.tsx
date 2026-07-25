import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MetadataAssignee } from './MetadataAssignee';

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

const usersState = vi.hoisted(() => ({ current: [] as unknown[] }));
vi.mock('../../../hooks/useUsers', () => ({
  useUsers: () => usersState.current,
}));

beforeEach(() => {
  authState.current = null;
  usersState.current = [];
});

describe('MetadataAssignee', () => {
  it('renders nothing without an AuthProvider (useOptionalAuth returns null)', () => {
    render(<MetadataAssignee assignee={undefined} onChange={vi.fn()} />);
    expect(screen.queryByText('Assignee')).not.toBeInTheDocument();
  });

  it('renders nothing in none mode', () => {
    authState.current = { mode: 'none' };
    render(<MetadataAssignee assignee={undefined} onChange={vi.fn()} />);
    expect(screen.queryByText('Assignee')).not.toBeInTheDocument();
  });

  it('renders Unassigned + roster options with display-name labels in multi mode', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [
      { username: 'alice', display_name: 'Alice Smith' },
      { username: 'bob', display_name: '' },
    ];
    render(<MetadataAssignee assignee={undefined} onChange={vi.fn()} />);

    expect(screen.getByText('Assignee')).toBeInTheDocument();
    const select = screen.getByRole('combobox', { name: 'Assignee' });
    expect(select).toHaveValue('');
    expect(screen.getByRole('option', { name: 'Unassigned' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Alice Smith' })).toBeInTheDocument();
    // Empty display_name falls back to the username.
    expect(screen.getByRole('option', { name: 'bob' })).toBeInTheDocument();
  });

  it('shows a stale current value as "<username> (unknown)" and keeps it selectable', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'alice', display_name: 'Alice Smith' }];
    render(<MetadataAssignee assignee="ghost" onChange={vi.fn()} />);

    const select = screen.getByRole('combobox', { name: 'Assignee' }) as HTMLSelectElement;
    expect(select.value).toBe('ghost');
    expect(screen.getByRole('option', { name: 'ghost (unknown)' })).toBeInTheDocument();
  });

  it('fires onChange with the selected username', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'alice', display_name: 'Alice Smith' }];
    const onChange = vi.fn();
    render(<MetadataAssignee assignee={undefined} onChange={onChange} />);

    fireEvent.change(screen.getByRole('combobox', { name: 'Assignee' }), {
      target: { value: 'alice' },
    });
    expect(onChange).toHaveBeenCalledWith('alice');
  });

  it('respects disabled', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'alice', display_name: 'Alice Smith' }];
    render(<MetadataAssignee assignee={undefined} onChange={vi.fn()} disabled />);
    expect(screen.getByRole('combobox', { name: 'Assignee' })).toBeDisabled();
  });
});
