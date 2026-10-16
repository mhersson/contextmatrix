import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NewChatDialog } from './NewChatDialog';

const navigate = vi.fn();
vi.mock('react-router', () => ({ useNavigate: () => navigate }));

const createChat = vi.fn();
const openChat = vi.fn();
vi.mock('../../api/client', () => ({
  api: {
    createChat: (...args: unknown[]) => createChat(...args),
    openChat: (...args: unknown[]) => openChat(...args),
    listChatModels: () => Promise.resolve({ source: 'endpoint', models: [], default: '' }),
  },
  isAPIError: () => false,
}));

vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({ projects: [{ name: 'alpha', display_name: 'Alpha' }, { name: 'beta' }] }),
}));

vi.mock('../../hooks/useChatSessions', () => ({ notifyChatSessionsChanged: vi.fn() }));

let architectEnabled = true;
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ architectEnabled, favorites: null }),
}));

describe('NewChatDialog blueprint sessions', () => {
  beforeEach(() => {
    createChat.mockReset();
    openChat.mockReset();
    navigate.mockReset();
    architectEnabled = true;
    createChat.mockResolvedValue({ id: 'S1', title: '', status: 'cold' });
    openChat.mockResolvedValue({ id: 'S1', title: '', status: 'active' });
  });

  it('hides the blueprint option when no architect backend is configured', () => {
    architectEnabled = false;
    render(<NewChatDialog open onClose={() => {}} />);
    expect(screen.queryByLabelText('Blueprint')).not.toBeInTheDocument();
  });

  it('keeps Create disabled until a project is ticked', () => {
    render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByLabelText('Blueprint'));
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
    fireEvent.click(screen.getByLabelText('Alpha'));
    expect(screen.getByRole('button', { name: 'Create' })).toBeEnabled();
  });

  it('creates an architect session with projects, mode and cost cap', async () => {
    render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByLabelText('Blueprint'));
    fireEvent.click(screen.getByLabelText('Alpha'));
    fireEvent.click(screen.getByLabelText('beta'));
    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'auto' } });
    fireEvent.change(screen.getByLabelText(/Cost cap/), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat).toHaveBeenCalledWith({
      title: undefined,
      model: undefined,
      kind: 'architect',
      projects: ['alpha', 'beta'],
      mode: 'auto',
      cost_cap_usd: 3,
    });
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
  });

  it('creates a plain chat without blueprint fields', async () => {
    render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat).toHaveBeenCalledWith({ title: undefined, project: undefined, model: undefined });
  });
});
