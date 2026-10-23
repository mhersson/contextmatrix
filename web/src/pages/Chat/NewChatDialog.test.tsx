import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NewChatDialog } from './NewChatDialog';
import { PLAIN_LANGUAGE, needReason } from './blueprintForm';
import { HANDOFF_REFUSALS, NEED_NOT_POSTED } from './useCreateSession';
import { BLUEPRINT_MESSAGE_MAX_BYTES, formatBytes, utf8Length } from '../../components/ChatPanel/chatEntryUtils';
import type { HandoffPrefill } from '../../components/ChatPanel/handoff';

const navigate = vi.fn();
vi.mock('react-router', () => ({ useNavigate: () => navigate }));

const createChat = vi.fn();
const openChat = vi.fn();
const sendChatMessage = vi.fn();
vi.mock('../../api/client', () => {
  const isAPIError = (e: unknown): e is { error: string; code?: string } =>
    typeof e === 'object' && e !== null && typeof (e as { error?: unknown }).error === 'string';
  return {
    api: {
      createChat: (...args: unknown[]) => createChat(...args),
      openChat: (...args: unknown[]) => openChat(...args),
      sendChatMessage: (...args: unknown[]) => sendChatMessage(...args),
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

let projectsLoading = false;
vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({
    projects: projectsLoading
      ? []
      : [{ name: 'alpha', display_name: 'Alpha', repo: 'git@github.com:example/alpha.git' }, { name: 'beta' }],
    loading: projectsLoading,
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

let identity: string | null = 'human:web-abc';
vi.mock('../../hooks/useIdentity', () => ({ useIdentity: () => ({ identity }) }));

function renderDialog() {
  render(<NewChatDialog open onClose={() => {}} />);
}

function startBlueprint() {
  renderDialog();
  fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
}

describe('NewChatDialog', () => {
  beforeEach(() => {
    identity = 'human:web-abc';
    createChat.mockReset();
    openChat.mockReset();
    sendChatMessage.mockReset();
    navigate.mockReset();
    architectEnabled = true;
    projectsLoading = false;
    modelSource = 'endpoint';
    modelList = baseModels;
    createChat.mockResolvedValue({ id: 'S1', title: '', status: 'cold' });
    openChat.mockResolvedValue({ id: 'S1', title: '', status: 'active' });
    sendChatMessage.mockResolvedValue({ ok: true, message_id: 'M1' });
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
    const reason = /No price is known for this model/;

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

  const plainSwitch = () => screen.getByRole('switch', { name: PLAIN_LANGUAGE.label });
  const createAlpha = async () => {
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create blueprint' }));
    await waitFor(() => expect(createChat).toHaveBeenCalled());
  };

  it('offers plain language only for a blueprint', () => {
    renderDialog();
    expect(screen.queryByRole('switch', { name: PLAIN_LANGUAGE.label })).not.toBeInTheDocument();
  });

  it('sends plain language when switched on, and the next blueprint starts with it on', async () => {
    const first = render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    expect(plainSwitch()).not.toBeChecked();
    fireEvent.click(plainSwitch());
    expect(plainSwitch()).toBeChecked();
    await createAlpha();
    expect(createChat.mock.calls[0][0].plain_language).toBe(true);
    first.unmount();
    startBlueprint();
    expect(plainSwitch()).toBeChecked();
  });

  it('keeps each person their own choice', async () => {
    identity = 'human:alice';
    const first = render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    fireEvent.click(plainSwitch());
    await createAlpha();
    first.unmount();
    identity = 'human:bob';
    startBlueprint();
    expect(plainSwitch()).not.toBeChecked();
  });

  it('remembers nothing until a blueprint is created, and sends no flag while off', async () => {
    const first = render(<NewChatDialog open onClose={() => {}} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    fireEvent.click(plainSwitch());
    first.unmount();
    startBlueprint();
    expect(plainSwitch()).not.toBeChecked();
    await createAlpha();
    expect(createChat.mock.calls[0][0]).not.toHaveProperty('plain_language');
  });
  describe('from a hand-off', () => {
    const prefill: HandoffPrefill = {
      chatId: 'C',
      chatTitle: 'Card budgets',
      seq: 4,
      title: 'Card time budgets',
      need: '## Need\nx',
      projects: ['alpha', 'gone'],
    };
    const openHandoff = () => render(<NewChatDialog open onClose={() => {}} handoff={prefill} />);
    const createButton = () => screen.getByRole('button', { name: 'Create blueprint' });

    it('opens pre-filled from a hand-off', () => {
      openHandoff();
      expect(screen.getByRole('heading', { name: 'New blueprint' })).toBeInTheDocument();
      expect(screen.getByRole('note')).toHaveTextContent(prefill.chatTitle);
      expect(screen.getByLabelText('Title')).toHaveValue(prefill.title);
      expect(screen.getByLabelText('Need')).toHaveValue(prefill.need);
      expect(screen.getByRole('checkbox', { name: 'Alpha' })).toBeChecked();
      expect(screen.getByRole('checkbox', { name: 'beta' })).not.toBeChecked();
      expect(screen.getByText(/\bgone\b/)).toBeInTheDocument();
      expect(screen.queryByRole('radio', { name: 'Chat' })).not.toBeInTheDocument();
      expect(screen.queryByRole('radio', { name: 'Blueprint' })).not.toBeInTheDocument();
    });

    it('leaves no project out until the project list has loaded', () => {
      projectsLoading = true;
      const view = openHandoff();
      expect(screen.queryByText(/\bgone\b/)).not.toBeInTheDocument();
      projectsLoading = false;
      view.rerender(<NewChatDialog open onClose={() => {}} handoff={prefill} />);
      expect(screen.getByRole('checkbox', { name: 'Alpha' })).toBeChecked();
      expect(screen.getByText(/\bgone\b/)).toBeInTheDocument();
    });

    it('creates, opens and posts the need once', async () => {
      openHandoff();
      await screen.findByLabelText('Model');
      fireEvent.change(screen.getByLabelText('Need'), { target: { value: '## Need\nedited' } });
      fireEvent.click(createButton());
      await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
      expect(createChat).toHaveBeenCalledTimes(1);
      expect(createChat).toHaveBeenCalledWith({
        title: prefill.title,
        kind: 'architect',
        projects: ['alpha'],
        model: 'arch/default',
        mode: 'default',
        origin_chat: 'C',
        origin_seq: 4,
      });
      expect(openChat).toHaveBeenCalledWith('S1');
      expect(sendChatMessage).toHaveBeenCalledTimes(1);
      expect(sendChatMessage).toHaveBeenCalledWith('S1', '## Need\nedited', { handoff_need: true });
      expect(openChat.mock.invocationCallOrder[0]).toBeLessThan(sendChatMessage.mock.invocationCallOrder[0]);
    });

    it('refuses a need over 64 KiB', () => {
      openHandoff();
      const need = screen.getByLabelText('Need');
      const over = '€'.repeat(21846);
      fireEvent.change(need, { target: { value: over } });
      expect(createButton()).toBeDisabled();
      expect(screen.getByText(needReason(over)!)).toBeInTheDocument();
      expect(need).toHaveAccessibleDescription(expect.stringContaining(formatBytes(utf8Length(over))));
      fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter', ctrlKey: true });
      expect(createChat).not.toHaveBeenCalled();

      const under = '€'.repeat(21845);
      expect(utf8Length(under)).toBeLessThanOrEqual(BLUEPRINT_MESSAGE_MAX_BYTES);
      fireEvent.change(need, { target: { value: under } });
      expect(createButton()).toBeEnabled();
    });

    it('refuses a blank need', () => {
      openHandoff();
      fireEvent.change(screen.getByLabelText('Need'), { target: { value: '  ' } });
      expect(createButton()).toBeDisabled();
    });

    it('retries a failed need send on the same session', async () => {
      sendChatMessage.mockRejectedValueOnce({ error: 'boom', code: 'INTERNAL_ERROR' });
      openHandoff();
      await screen.findByLabelText('Model');
      fireEvent.click(createButton());
      expect(await screen.findByRole('alert')).toHaveTextContent(NEED_NOT_POSTED);
      expect(navigate).not.toHaveBeenCalled();

      fireEvent.click(createButton());
      await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
      expect(createChat).toHaveBeenCalledTimes(1);
      expect(openChat).toHaveBeenCalledTimes(2);
      expect(sendChatMessage).toHaveBeenCalledTimes(2);
      expect(sendChatMessage.mock.calls[1]).toEqual(sendChatMessage.mock.calls[0]);
      expect(sendChatMessage.mock.calls[1][0]).toBe('S1');
    });

    it.each(['HANDOFF_TAKEN', 'HANDOFF_REPLACED'] as const)('explains a %s refusal', async (code) => {
      createChat.mockRejectedValueOnce({ error: 'conflict', code });
      openHandoff();
      fireEvent.click(createButton());
      expect(await screen.findByRole('alert')).toHaveTextContent(HANDOFF_REFUSALS[code]);
      expect(openChat).not.toHaveBeenCalled();
      expect(sendChatMessage).not.toHaveBeenCalled();
    });
  });

  it('a plain New chat dialog has no Need field or banner', () => {
    renderDialog();
    expect(screen.queryByLabelText('Need')).not.toBeInTheDocument();
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    expect(screen.queryByLabelText('Need')).not.toBeInTheDocument();
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('posts no need for a blueprint made without a hand-off', async () => {
    startBlueprint();
    await createAlpha();
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/chat/S1'));
    expect(createChat.mock.calls[0][0]).not.toHaveProperty('origin_chat');
    expect(sendChatMessage).not.toHaveBeenCalled();
  });
});
