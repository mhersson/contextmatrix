import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NewChatDialog } from './NewChatDialog';

const navigate = vi.fn();
vi.mock('react-router', () => ({ useNavigate: () => navigate }));

const createChat = vi.fn();
const openChat = vi.fn();
vi.mock('../../api/client', () => {
  const isAPIError = (e: unknown): e is { error: string; code?: string } =>
    typeof e === 'object' && e !== null && typeof (e as { error?: unknown }).error === 'string';
  return {
    api: {
      createChat: (...args: unknown[]) => createChat(...args),
      openChat: (...args: unknown[]) => openChat(...args),
      listChatModels: () =>
        Promise.resolve({
          source: modelSource,
          models: modelList,
          default: 'chat/default',
          architect_default: 'arch/default',
        }),
    },
    isAPIError,
    describeApiError: (e: unknown, fallback: string) => (isAPIError(e) ? e.error : fallback),
  };
});

vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({
    projects: [{ name: 'alpha', display_name: 'Alpha', repo: 'git@github.com:example/alpha.git' }, { name: 'beta' }],
  }),
}));
vi.mock('../../hooks/useChatSessions', () => ({ notifyChatSessionsChanged: vi.fn() }));

let architectEnabled = true;
let modelSource: 'endpoint' | 'openrouter' = 'endpoint';
const baseModels = [
  { id: 'chat/default', label: 'Chat default', max_tokens: 1000 },
  { id: 'arch/default', label: 'Arch default', max_tokens: 1000 },
  { id: 'other/priced', label: 'Other priced', max_tokens: 1000 },
  { id: 'free/unpriced', label: 'Free unpriced', max_tokens: 1000, unpriced: true },
];
let modelList = baseModels;
vi.mock('../../hooks/useTheme', () => ({ useTheme: () => ({ architectEnabled, favorites: null }) }));

function renderDialog() {
  render(<NewChatDialog open onClose={() => {}} />);
}

function startBlueprint() {
  renderDialog();
  fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
}

describe('NewChatDialog', () => {
  beforeEach(() => {
    createChat.mockReset();
    openChat.mockReset();
    navigate.mockReset();
    architectEnabled = true;
    modelSource = 'endpoint';
    modelList = baseModels;
    createChat.mockResolvedValue({ id: 'S1', title: '', status: 'cold' });
    openChat.mockResolvedValue({ id: 'S1', title: '', status: 'active' });
  });

  it('offers no kind switch when no architect backend is configured', () => {
    architectEnabled = false;
    renderDialog();
    expect(screen.queryByRole('radio', { name: 'Blueprint' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create chat' })).toBeEnabled();
  });

  it('says why Create is disabled until a project is picked', () => {
    startBlueprint();
    const create = screen.getByRole('button', { name: 'Create blueprint' });
    expect(create).toBeDisabled();
    expect(screen.getByText(/Pick at least one project/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    expect(create).toBeEnabled();
    expect(screen.queryByText(/Pick at least one project/)).not.toBeInTheDocument();
  });

  it('creates an architect session with projects, mode and a cap preset', async () => {
    startBlueprint();
    await screen.findByLabelText('Model');
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'beta' }));
    fireEvent.click(screen.getByRole('radio', { name: 'Auto' }));
    fireEvent.click(screen.getByRole('radio', { name: '$5' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
    expect(createChat).toHaveBeenCalledWith({
      title: undefined,
      kind: 'architect',
      projects: ['alpha', 'beta'],
      model: 'arch/default',
      mode: 'auto',
      cost_cap_usd: 5,
    });
    expect(openChat).toHaveBeenCalledWith('S1');
  });

  it('sends no cap when No cap stays picked', async () => {
    startBlueprint();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat.mock.calls[0][0].cost_cap_usd).toBeUndefined();
  });

  it('sends the model picked for the blueprint', async () => {
    startBlueprint();
    fireEvent.change(await screen.findByLabelText('Model'), { target: { value: 'other/priced' } });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat.mock.calls[0][0].model).toBe('other/priced');
  });

  it('keeps the blueprint and chat picks independent', async () => {
    startBlueprint();
    fireEvent.change(await screen.findByLabelText('Model'), { target: { value: 'other/priced' } });
    fireEvent.click(screen.getByRole('radio', { name: 'Chat' }));
    expect(screen.getByLabelText('Model')).toHaveValue('chat/default');
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'free/unpriced' } });
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    expect(screen.getByLabelText('Model')).toHaveValue('other/priced');
    fireEvent.click(screen.getByRole('radio', { name: 'Chat' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create chat' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat.mock.calls[0][0].model).toBe('free/unpriced');
  });

  describe('cost cap on an unpriced model', () => {
    async function pickUnpricedWithCap() {
      startBlueprint();
      fireEvent.change(await screen.findByLabelText('Model'), { target: { value: 'free/unpriced' } });
      fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
      fireEvent.click(screen.getByRole('radio', { name: '$5' }));
    }
    const reason = /cannot price this model/;

    it('disables Create and says why, on the button and on the keyboard paths', async () => {
      await pickUnpricedWithCap();
      expect(screen.getByRole('button', { name: 'Create blueprint' })).toBeDisabled();
      expect(screen.getByText(reason)).toBeInTheDocument();
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter', ctrlKey: true });
      fireEvent.keyDown(screen.getByLabelText('Title'), { key: 'Enter' });
      expect(createChat).not.toHaveBeenCalled();
    });

    it('enables Create again on No cap', async () => {
      await pickUnpricedWithCap();
      fireEvent.click(screen.getByRole('radio', { name: 'No cap' }));
      expect(screen.getByRole('button', { name: 'Create blueprint' })).toBeEnabled();
      expect(screen.queryByText(reason)).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
      await waitFor(() => expect(createChat).toHaveBeenCalled());
      expect(createChat.mock.calls[0][0].model).toBe('free/unpriced');
      expect(createChat.mock.calls[0][0].cost_cap_usd).toBeUndefined();
    });

    it('enables Create again on a priced model', async () => {
      await pickUnpricedWithCap();
      fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'other/priced' } });
      expect(screen.getByRole('button', { name: 'Create blueprint' })).toBeEnabled();
    });

    it('does not block a chat on an unpriced model', async () => {
      renderDialog();
      fireEvent.change(await screen.findByLabelText('Model'), { target: { value: 'free/unpriced' } });
      expect(screen.getByRole('button', { name: 'Create chat' })).toBeEnabled();
    });
  });

  it('names the yolo outcome on the button', () => {
    startBlueprint();
    fireEvent.click(screen.getByRole('radio', { name: 'Yolo' }));
    expect(screen.getByRole('button', { name: 'Create and run' })).toBeInTheDocument();
  });

  it('shows the architect starting while the session opens', async () => {
    let finish: (v: unknown) => void = () => {};
    openChat.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    startBlueprint();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    expect(await screen.findByRole('button', { name: /Starting the architect/ })).toBeDisabled();
    finish({ id: 'S1', status: 'active' });
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
  });

  it('keeps the dialog open when the architect does not start, and retries the same session', async () => {
    openChat.mockRejectedValueOnce({ error: 'internal error', code: 'INTERNAL_ERROR' });
    startBlueprint();
    await screen.findByLabelText('Model');
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/architect is not reachable/);
    expect(navigate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
    expect(createChat).toHaveBeenCalledTimes(1);
    expect(openChat).toHaveBeenCalledTimes(2);
  });

  it('creates a plain chat with the default model and the picked project', async () => {
    renderDialog();
    await screen.findByLabelText('Model');
    fireEvent.click(screen.getByRole('radio', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create chat' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
    expect(createChat).toHaveBeenCalledWith({ title: undefined, project: 'alpha', model: 'chat/default' });
  });

  it('opens a plain chat even when its worker does not start', async () => {
    openChat.mockRejectedValueOnce({ error: 'boom', code: 'INTERNAL_ERROR' });
    renderDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Create chat' }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
  });

  it('creates on Ctrl+Enter anywhere in the dialog', async () => {
    renderDialog();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(createChat).toHaveBeenCalledTimes(1));
  });

  it('creates on Enter in the title field', async () => {
    renderDialog();
    const title = screen.getByLabelText('Title');
    fireEvent.change(title, { target: { value: 'Why is it slow' } });
    fireEvent.keyDown(title, { key: 'Enter' });
    await waitFor(() => expect(createChat).toHaveBeenCalledTimes(1));
    expect(createChat.mock.calls[0][0].title).toBe('Why is it slow');
  });

  it('leaves Ctrl+Enter to the OpenRouter model combobox so its commit lands first', async () => {
    modelSource = 'openrouter';
    renderDialog();
    const combo = await screen.findByRole('combobox');
    fireEvent.keyDown(combo, { key: 'Enter', ctrlKey: true });
    expect(createChat).not.toHaveBeenCalled();
  });

  it('drops a blueprint error when switching to the Chat tile', async () => {
    openChat.mockRejectedValueOnce({ error: 'internal error', code: 'INTERNAL_ERROR' });
    startBlueprint();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    expect(await screen.findByRole('alert')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('radio', { name: 'Chat' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
