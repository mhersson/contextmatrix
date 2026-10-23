/**
 * ChatPage hands the hand-off context to the panes and opens the dialog
 * pre-filled from it. Kept apart from ChatPage.test.tsx so these broad
 * `vi.mock`s don't bleed into the helper-component suite there.
 */
import { describe, it, expect, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { useContext } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ChatPage } from './ChatPage';
import { HandoffContext } from '../../components/ChatPanel/handoffContext';
import type { HandoffPrefill } from '../../components/ChatPanel/handoff';

const sessions = [{ id: 'B1', title: 'Budgets', status: 'active', kind: 'architect', origin_chat: 'C', origin_seq: 4 }];
vi.mock('../../api/client', () => ({
  api: { listChats: vi.fn(() => Promise.resolve(sessions)) },
  isAPIError: vi.fn(() => false),
}));

vi.mock('./ChatThread', () => ({ ChatThread: () => null }));
vi.mock('./MobileChatHeader', () => ({ MobileChatHeader: () => null }));
vi.mock('../../components/ConfirmModal/ConfirmModal', () => ({ ConfirmModal: () => null }));

interface DialogProps {
  open: boolean;
  onClose: () => void;
  handoff?: HandoffPrefill;
}
const dialogSpy = vi.fn<(props: DialogProps) => void>();
vi.mock('./NewChatDialog', () => ({
  NewChatDialog: (props: DialogProps) => {
    dialogSpy(props);
    return null;
  },
}));

const prefill: HandoffPrefill = {
  chatId: 'C',
  chatTitle: 'Card budgets',
  seq: 5,
  title: 'Card time budgets',
  need: 'x',
  projects: ['alpha'],
};

function HandoffProbe() {
  const ctx = useContext(HandoffContext);
  return (
    <>
      <span data-testid="sessions">{ctx?.sessions.map((s) => s.id).join(',')}</span>
      <button type="button" onClick={() => ctx?.openHandoff(prefill)}>
        Review
      </button>
    </>
  );
}

vi.mock('../../components/ChatLayout', async () => {
  const real = await vi.importActual<typeof import('../../components/ChatLayout')>('../../components/ChatLayout');
  return { ...real, ChatLayout: () => <HandoffProbe /> };
});

describe('ChatPage hand-off', () => {
  it('gives the panes the sessions and opens the dialog pre-filled until it closes', async () => {
    render(
      <MemoryRouter initialEntries={['/chat']}>
        <Routes>
          <Route path="chat" element={<ChatPage />} />
        </Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByTestId('sessions')).toHaveTextContent('B1'));
    expect(dialogSpy.mock.lastCall?.[0].open).toBe(false);

    act(() => screen.getByRole('button', { name: 'Review' }).click());
    expect(dialogSpy.mock.lastCall?.[0]).toMatchObject({ open: true, handoff: prefill });

    act(() => dialogSpy.mock.lastCall?.[0].onClose());
    expect(dialogSpy.mock.lastCall?.[0].open).toBe(false);
    expect(dialogSpy.mock.lastCall?.[0].handoff).toBeUndefined();
  });
});
