import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatPanel } from './ChatPanel';
import type { LogEntry } from '../../types';

// Node 25 provides a built-in localStorage that lacks .clear(). Override it
// with a real-backing-store mock that supports spy-able methods, matching the
// pattern used in useCollapsedCards.test.ts and useChatLayout.test.tsx.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

const logs: LogEntry[] = [
  { ts: '2026-05-13T10:00:00Z', card_id: '', type: 'user', content: 'hello' },
  { ts: '2026-05-13T10:00:01Z', card_id: '', type: 'text', content: 'world' },
  { ts: '2026-05-13T10:00:02Z', card_id: '', type: 'tool_call', content: 'Read: foo.go' },
];

describe('ChatPanel', () => {
  beforeEach(() => {
    localStorageMock.clear();
    vi.clearAllMocks();
  });

  it('renders user and assistant text by default; tool_call hidden', () => {
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
    expect(screen.getByText('hello')).toBeInTheDocument();
    expect(screen.getByText('world')).toBeInTheDocument();
    expect(screen.queryByText('Read: foo.go')).not.toBeInTheDocument();
  });

  it('shows tool_call when Tool calls filter is toggled on', () => {
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
    fireEvent.click(screen.getByLabelText('Tool calls'));
    expect(screen.getByText('Read: foo.go')).toBeInTheDocument();
  });

  it('sends on Enter, newline on Shift+Enter', () => {
    const onSend = vi.fn();
    render(<ChatPanel logs={[]} onSend={onSend} sendDisabled={false} />);
    const ta = screen.getByPlaceholderText(/Type a message/);
    fireEvent.change(ta, { target: { value: 'hi' } });
    fireEvent.keyDown(ta, { key: 'Enter', shiftKey: false });
    expect(onSend).toHaveBeenCalledWith('hi');
  });

  it('disables compose when sendDisabled is true', () => {
    render(<ChatPanel logs={[]} onSend={() => {}} sendDisabled={true} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeDisabled();
  });

  it('shows readOnlyMessage instead of compose when set', () => {
    render(<ChatPanel logs={[]} onSend={() => {}} sendDisabled={false} readOnlyMessage="Session ended" />);
    expect(screen.getByRole('status')).toHaveTextContent('Session ended');
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
  });

  it('renders a divider for kind="divider" entries', () => {
    const dividerLogs: LogEntry[] = [
      { ts: '2026-05-13T10:00:00Z', card_id: '', type: 'system', kind: 'divider', content: 'Context cleared' },
    ];
    render(<ChatPanel logs={dividerLogs} onSend={() => {}} sendDisabled={false} />);
    const divider = screen.getByTestId('chat-divider');
    expect(divider).toBeInTheDocument();
    expect(divider).toHaveTextContent('Context cleared');
  });

  it('does NOT render a divider for system entries that only match by content (no kind)', () => {
    // Regression: ChatPanel is shared with CardChat, so a HITL agent that
    // logs a "Context cleared" system line must NOT hijack into a divider.
    const noKindLogs: LogEntry[] = [
      { ts: '2026-05-13T10:00:00Z', card_id: '', type: 'system', content: 'Context cleared' },
    ];
    render(<ChatPanel logs={noKindLogs} onSend={() => {}} sendDisabled={false} />);
    expect(screen.queryByTestId('chat-divider')).not.toBeInTheDocument();
  });

  it('renders a regular system message normally when kind is absent', () => {
    const sysLogs: LogEntry[] = [
      { ts: '2026-05-13T10:00:00Z', card_id: '', type: 'system', content: 'just an ordinary system note' },
    ];
    render(<ChatPanel logs={sysLogs} onSend={() => {}} sendDisabled={false} />);
    expect(screen.getByText('just an ordinary system note')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-divider')).not.toBeInTheDocument();
  });

  describe('localStorage filter prefs', () => {
    it('restores showToolCalls=true from localStorage so tool_call entries are visible on first render', () => {
      localStorageMock.setItem('chat_filter_prefs', JSON.stringify({ showText: true, showToolCalls: true, showThinking: false }));
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      expect(screen.getByText('Read: foo.go')).toBeInTheDocument();
    });

    it('falls back to defaults when localStorage key is missing', () => {
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      expect(screen.getByText('hello')).toBeInTheDocument();
      expect(screen.getByText('world')).toBeInTheDocument();
      expect(screen.queryByText('Read: foo.go')).not.toBeInTheDocument();
    });

    it('falls back to defaults when localStorage contains malformed JSON', () => {
      localStorageMock.setItem('chat_filter_prefs', 'not-valid-json{{{');
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      expect(screen.queryByText('Read: foo.go')).not.toBeInTheDocument();
    });

    it('falls back to defaults for fields that are not booleans', () => {
      localStorageMock.setItem('chat_filter_prefs', JSON.stringify({ showText: 'yes', showToolCalls: 1, showThinking: null }));
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      // showText defaults to true (visible), showToolCalls defaults to false (hidden)
      expect(screen.getByText('world')).toBeInTheDocument();
      expect(screen.queryByText('Read: foo.go')).not.toBeInTheDocument();
    });

    it('writes updated prefs to localStorage when a checkbox is toggled', () => {
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      fireEvent.click(screen.getByLabelText('Tool calls'));
      const stored = JSON.parse(localStorageMock.getItem('chat_filter_prefs') ?? '{}');
      expect(stored.showToolCalls).toBe(true);
      expect(stored.showText).toBe(true);
      expect(stored.showThinking).toBe(false);
    });

    it('tolerates a throwing localStorage.getItem without crashing', () => {
      localStorageMock.getItem.mockImplementationOnce(() => {
        throw new Error('storage blocked');
      });
      expect(() => render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />)).not.toThrow();
    });

    it('tolerates a throwing localStorage.setItem without crashing', () => {
      render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
      localStorageMock.setItem.mockImplementationOnce(() => {
        throw new Error('QuotaExceededError');
      });
      expect(() => fireEvent.click(screen.getByLabelText('Tool calls'))).not.toThrow();
    });
  });
});
