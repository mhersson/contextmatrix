import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, renderHook, act } from '@testing-library/react';
import { ChatPanel } from './ChatPanel';
import { useWorkerLogs } from '../../hooks/useWorkerLogs';
import { formatHHMM, formatTitle } from '../../utils/chatTimestamp';
import type { LogEntry } from '../../types';

// Local formatter matching the production instance - locale pinned to 'en-GB'
// so HH:MM output is deterministic regardless of the test environment's locale.
const hhmmFormatter = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

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

// ---------------------------------------------------------------------------
// Fake EventSource for the composed useWorkerLogs → ChatPanel integration test
// ---------------------------------------------------------------------------

type ESListener = (event: MessageEvent) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  url: string;
  readyState: number = 0;
  onopen: (() => void) | null = null;
  onmessage: ESListener | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  simulateMessage(data: unknown) {
    const evt = { data: JSON.stringify(data) } as MessageEvent;
    this.onmessage?.(evt);
  }

  simulateError() {
    this.readyState = 2;
    this.onerror?.();
  }

  close() {
    this.readyState = 2;
    this.closed = true;
  }
}

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

  describe('speaker chips (mob session discussions)', () => {
    /**
     * External producer boundary - real e2e proof against live
     * contextmatrix-agent mob-session traffic is out of scope for this
     * repository. The agent backend (contextmatrix-agent) must populate
     * protocol.LogEntry.Model for moderator and seat discussion frames;
     * if it does not, that is a separate external card in the
     * contextmatrix-agent repo. The tests here can only prove passthrough
     * correctness with synthetic fixtures, not against real agent traffic.
     * This limitation is explicitly scoped so the card is not mistakenly
     * treated as fully done.
     */
    it('renders a labeled chip on text entries that carry agent', () => {
      const discussionLogs: LogEntry[] = [
        {
          ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text',
          content: 'I propose splitting the parser.', agent: 'seat-1',
        },
      ];
      render(<ChatPanel logs={discussionLogs} onSend={() => {}} sendDisabled={false} />);
      const chip = screen.getByTestId('speaker-chip');
      expect(chip).toHaveTextContent('seat-1');
      expect(screen.getByText('I propose splitting the parser.')).toBeInTheDocument();
    });

    it('renders no chip when agent is absent', () => {
      const plainLogs: LogEntry[] = [
        { ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text', content: 'plain reply' },
      ];
      render(<ChatPanel logs={plainLogs} onSend={() => {}} sendDisabled={false} />);
      expect(screen.queryByTestId('speaker-chip')).not.toBeInTheDocument();
    });

    it('gives different authors chips (one per attributed entry)', () => {
      const discussionLogs: LogEntry[] = [
        { ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text', content: 'a', agent: 'seat-1' },
        { ts: '2026-05-13T10:00:01Z', card_id: 'C-1', type: 'text', content: 'b', agent: 'guest-laptop' },
      ];
      render(<ChatPanel logs={discussionLogs} onSend={() => {}} sendDisabled={false} />);
      const chips = screen.getAllByTestId('speaker-chip');
      expect(chips).toHaveLength(2);
      expect(chips[0]).toHaveTextContent('seat-1');
      expect(chips[1]).toHaveTextContent('guest-laptop');
    });

    it('renders speaker and model pills side-by-side when agent and model are both present', () => {
      // Two mob frames: moderator + z-ai/glm-5.2, seat-1 + anthropic/sonnet-5.
      const discussionLogs: LogEntry[] = [
        {
          ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text',
          content: 'framing the plan', agent: 'moderator', model: 'z-ai/glm-5.2',
        },
        {
          ts: '2026-05-13T10:00:01Z', card_id: 'C-1', type: 'text',
          content: 'drafting the parser', agent: 'seat-1', model: 'anthropic/sonnet-5',
        },
      ];
      render(<ChatPanel logs={discussionLogs} onSend={() => {}} sendDisabled={false} />);
      const speakerChips = screen.getAllByTestId('speaker-chip');
      const modelChips = screen.getAllByTestId('model-chip');
      expect(speakerChips).toHaveLength(2);
      expect(modelChips).toHaveLength(2);
      expect(speakerChips[0]).toHaveTextContent('moderator');
      expect(modelChips[0]).toHaveTextContent('z-ai/glm-5.2');
      expect(speakerChips[1]).toHaveTextContent('seat-1');
      expect(modelChips[1]).toHaveTextContent('anthropic/sonnet-5');
      // Both pills are rendered on the same line within one flex row container.
      expect(speakerChips[0].parentElement).toBe(modelChips[0].parentElement);
      expect(speakerChips[0].parentElement?.className).toContain('flex');
    });

    it('renders only the speaker pill when model is absent (regression guard for ordinary frames and human participants)', () => {
      const plainLogs: LogEntry[] = [
        { ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text', content: 'single-agent reply', agent: 'claude' },
        { ts: '2026-05-13T10:00:01Z', card_id: 'C-1', type: 'text', content: 'human note', agent: 'human:alice' },
      ];
      render(<ChatPanel logs={plainLogs} onSend={() => {}} sendDisabled={false} />);
      const speakerChips = screen.getAllByTestId('speaker-chip');
      expect(speakerChips).toHaveLength(2);
      expect(speakerChips[0]).toHaveTextContent('claude');
      expect(speakerChips[1]).toHaveTextContent('human:alice');
      // No model pill rendered when model is absent.
      expect(screen.queryByTestId('model-chip')).not.toBeInTheDocument();
    });

    it('renders only the speaker pill when model is an empty string (not empty parens or "undefined")', () => {
      const emptyModelLogs: LogEntry[] = [
        {
          ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text',
          content: 'reply', agent: 'seat-2', model: '',
        },
      ];
      render(<ChatPanel logs={emptyModelLogs} onSend={() => {}} sendDisabled={false} />);
      expect(screen.getAllByTestId('speaker-chip')).toHaveLength(1);
      expect(screen.queryByTestId('model-chip')).not.toBeInTheDocument();
      // No "(undefined)" or empty parens leak into the DOM.
      expect(document.body).not.toHaveTextContent('(undefined)');
      expect(document.body).not.toHaveTextContent('()');
    });

    it('the model pill uses the purple CSS custom properties, not a hardcoded hex', () => {
      const discussionLogs: LogEntry[] = [
        {
          ts: '2026-05-13T10:00:00Z', card_id: 'C-1', type: 'text',
          content: 'reply', agent: 'seat-1', model: 'z-ai/glm-5.2',
        },
      ];
      render(<ChatPanel logs={discussionLogs} onSend={() => {}} sendDisabled={false} />);
      const modelChip = screen.getByTestId('model-chip');
      // Assert the semantic tokens are referenced (no hardcoded hex).
      expect(modelChip.style.backgroundColor).toBe('var(--bg-purple)');
      expect(modelChip.style.color).toBe('var(--purple)');
      // The speaker chip, by contrast, must NOT use the purple token - it
      // uses the per-author idColor accent, keeping the two pills visually
      // distinct.
      const speakerChip = screen.getByTestId('speaker-chip');
      expect(speakerChip.style.color).not.toBe('var(--purple)');
    });
  });

  describe('composed integration: useWorkerLogs → LogEntry → ChatPanel', () => {
    beforeEach(() => {
      FakeEventSource.instances = [];
      vi.stubGlobal('EventSource', FakeEventSource);
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    /** Returns the most recently created FakeEventSource instance. */
    function latestES(): FakeEventSource {
      const es = FakeEventSource.instances[FakeEventSource.instances.length - 1];
      if (!es) throw new Error('No EventSource created');
      return es;
    }

    it('renders speaker and model pills from a wire-shaped {agent, model} frame through useWorkerLogs', () => {
      const { result } = renderHook(() =>
        useWorkerLogs({ project: 'proj', enabled: true }),
      );

      // Open the connection
      act(() => { latestES().simulateOpen(); });

      const ts = new Date().toISOString();

      // Send a mob-session frame with both agent and model
      act(() => {
        latestES().simulateMessage({
          type: 'text', content: 'framing the plan', card_id: 'C-1', ts, seq: 1,
          agent: 'moderator', model: 'z-ai/glm-5.2',
        });
      });

      // Render ChatPanel with the logs from useWorkerLogs
      render(<ChatPanel logs={result.current.logs} onSend={() => {}} sendDisabled={false} />);

      // Both pills must render
      const speakerChip = screen.getByTestId('speaker-chip');
      const modelChip = screen.getByTestId('model-chip');
      expect(speakerChip).toHaveTextContent('moderator');
      expect(modelChip).toHaveTextContent('z-ai/glm-5.2');
      // Content is visible
      expect(screen.getByText('framing the plan')).toBeInTheDocument();
    });
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

  describe('HH:MM timestamps', () => {
    // Deterministic ISO timestamps used in grouping tests.
    const TS_14_32_A = '2026-05-20T14:32:00Z'; // 14:32
    const TS_14_32_B = '2026-05-20T14:32:45Z'; // same minute
    const TS_14_33   = '2026-05-20T14:33:10Z'; // next minute

    it('user message renders a timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'user', content: 'hello' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      const stamps = container.querySelectorAll('time');
      expect(stamps).toHaveLength(1);
      expect(stamps[0].textContent).toMatch(/^\d{2}:\d{2}$/);
    });

    it('text message renders a timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'text', content: 'reply' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      const stamps = container.querySelectorAll('time');
      expect(stamps).toHaveLength(1);
      expect(stamps[0].textContent).toMatch(/^\d{2}:\d{2}$/);
    });

    it('tool_call entry renders no timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'tool_call', content: 'Read: foo.go' },
      ];
      // Tool calls are hidden by default; toggle them on.
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      fireEvent.click(screen.getByLabelText('Tool calls'));
      expect(container.querySelectorAll('time')).toHaveLength(0);
    });

    it('thinking entry renders no timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'thinking', content: 'Let me think…' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      fireEvent.click(screen.getByLabelText('Thinking'));
      expect(container.querySelectorAll('time')).toHaveLength(0);
    });

    it('malformed ts renders no timestamp', () => {
      const entry: LogEntry[] = [
        { ts: 'not-a-date', card_id: '', type: 'user', content: 'oops' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      expect(container.querySelectorAll('time')).toHaveLength(0);
    });

    it('user→user same minute renders only one timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'user', content: 'first' },
        { ts: TS_14_32_B, card_id: '', type: 'user', content: 'second' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      expect(container.querySelectorAll('time')).toHaveLength(1);
    });

    it('text→text same minute renders only one timestamp', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'text', content: 'first reply' },
        { ts: TS_14_32_B, card_id: '', type: 'text', content: 'second reply' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      expect(container.querySelectorAll('time')).toHaveLength(1);
    });

    it('user→text same minute renders two timestamps (different types)', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'user', content: 'hello' },
        { ts: TS_14_32_B, card_id: '', type: 'text', content: 'world' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      expect(container.querySelectorAll('time')).toHaveLength(2);
    });

    it('same type, different minutes renders two timestamps', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'text', content: 'first' },
        { ts: TS_14_33,   card_id: '', type: 'text', content: 'second' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      expect(container.querySelectorAll('time')).toHaveLength(2);
    });

    it('intervening tool_call does not reset grouping', () => {
      // [text@14:32, tool_call@14:32, text@14:32] → only the first text gets a stamp
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'text',      content: 'first text' },
        { ts: TS_14_32_B, card_id: '', type: 'tool_call', content: 'some tool' },
        { ts: TS_14_32_B, card_id: '', type: 'text',      content: 'second text' },
      ];
      // Enable tool_calls so all three entries render.
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      fireEvent.click(screen.getByLabelText('Tool calls'));
      // Only one timestamp: above the first text entry.
      expect(container.querySelectorAll('time')).toHaveLength(1);
    });

    it('title tooltip on timestamp is present, non-empty, contains HH:MM, and differs from raw ISO ts', () => {
      const entry: LogEntry[] = [
        { ts: TS_14_32_A, card_id: '', type: 'user', content: 'hi' },
      ];
      const { container } = render(<ChatPanel logs={entry} onSend={() => {}} sendDisabled={false} />);
      const stamp = container.querySelector('time');
      expect(stamp).not.toBeNull();
      const title = stamp!.getAttribute('title') ?? '';
      // Must be non-empty and differ from the raw ISO string.
      expect(title.length).toBeGreaterThan(0);
      expect(title).not.toBe(TS_14_32_A);
      // Must contain the HH:MM substring from formatHHMM.
      const hhmm = formatHHMM(TS_14_32_A);
      expect(hhmm).not.toBeNull();
      expect(title).toContain(hhmm!);
      // Verify formatTitle also matches the title.
      expect(title).toBe(formatTitle(TS_14_32_A));
    });

    it('formatHHMM returns null for missing ts', () => {
      expect(formatHHMM('')).toBeNull();
    });

    it('formatHHMM returns null for non-date string', () => {
      expect(formatHHMM('not-a-date')).toBeNull();
    });

    it('formatHHMM returns HH:MM for valid ISO timestamp', () => {
      const result = formatHHMM(TS_14_32_A);
      expect(result).not.toBeNull();
      // The result matches HH:MM pattern.
      expect(result).toMatch(/^\d{2}:\d{2}$/);
      // Also verify it matches our local formatter (locale-pinned, TZ-stable).
      expect(result).toBe(hhmmFormatter.format(new Date(TS_14_32_A)));
    });
  });
});
