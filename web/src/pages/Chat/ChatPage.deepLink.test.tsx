/**
 * Regression test for fresh-mount deep-link behaviour on ChatPage.
 *
 * Kept in a separate file from ChatPage.test.tsx so the broad `vi.mock`s
 * below don't bleed into the standalone helper-component suite there.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ChatPage } from './ChatPage';
import type { PaneSlots, Slot } from '../../components/ChatLayout/types';

vi.mock('../../api/client', () => ({
  api: { listChats: vi.fn(() => Promise.resolve([])) },
  isAPIError: vi.fn(() => false),
}));

vi.mock('./ChatThread', () => ({ ChatThread: () => null }));
const dialogSpy = vi.fn<(props: { open: boolean; initialKind?: string }) => void>();
vi.mock('./NewChatDialog', () => ({
  NewChatDialog: (props: { open: boolean; initialKind?: string }) => {
    dialogSpy({ open: props.open, initialKind: props.initialKind });
    return null;
  },
}));
vi.mock('./MobileChatHeader', () => ({ MobileChatHeader: () => null }));
vi.mock('../../components/ConfirmModal/ConfirmModal', () => ({ ConfirmModal: () => null }));

const layoutSpy = vi.fn<(props: { panes: PaneSlots; focused: Slot | null }) => void>();

vi.mock('../../components/ChatLayout', async () => {
  const real = await vi.importActual<typeof import('../../components/ChatLayout')>(
    '../../components/ChatLayout',
  );
  return {
    ...real,
    ChatLayout: (props: { panes: PaneSlots; focused: Slot | null }) => {
      layoutSpy({ panes: props.panes, focused: props.focused });
      return null;
    },
  };
});

describe('ChatPage deep link', () => {
  it('opens the deep-linked chat as a pane on first mount at /chat/:id', async () => {
    render(
      <MemoryRouter initialEntries={['/chat/chat-a']}>
        <Routes>
          <Route path="chat" element={<ChatPage />} />
          <Route path="chat/:id" element={<ChatPage />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => {
      const lastCall = layoutSpy.mock.calls.at(-1)?.[0];
      expect(lastCall?.panes.TL?.chatId).toBe('chat-a');
      expect(lastCall?.focused).toBe('TL');
    });
  });

  it.each([
    ['/chat?new=blueprint', 'architect'],
    ['/chat?new=1', 'chat'],
  ])('%s opens the dialog on %s', async (url, kind) => {
    dialogSpy.mockClear();
    render(
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="chat" element={<ChatPage />} />
        </Routes>
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(dialogSpy.mock.calls.at(-1)?.[0]).toEqual({ open: true, initialKind: kind });
    });
  });
});
