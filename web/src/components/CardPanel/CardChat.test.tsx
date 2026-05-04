import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CardChat } from './CardChat';
import type { Card, LogEntry } from '../../types';

// Mock api client
const mockSendCardMessage = vi.fn();
const mockPromoteCardToAutonomous = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    sendCardMessage: (...args: unknown[]) => mockSendCardMessage(...args),
    promoteCardToAutonomous: (...args: unknown[]) => mockPromoteCardToAutonomous(...args),
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

const noLogs: LogEntry[] = [];

const runningCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'in_progress',
  priority: 'medium',
  runner_status: 'running',
  autonomous: false,
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

const stoppedCard: Card = {
  ...runningCard,
  runner_status: 'failed',
};

const autonomousCard: Card = {
  ...runningCard,
  autonomous: true,
};

beforeEach(() => {
  vi.clearAllMocks();
  mockSendCardMessage.mockResolvedValue({ ok: true, message_id: 'msg-1' });
  mockPromoteCardToAutonomous.mockResolvedValue({ ...runningCard, autonomous: true });
});

describe('CardChat — visibility gate', () => {
  it('renders a read-only footer when runner_status is not "running"', () => {
    render(<CardChat card={stoppedCard} cardLogs={noLogs} />);
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Send/ })).not.toBeInTheDocument();
    expect(screen.getByText(/Session ended — read-only/)).toBeInTheDocument();
  });

  it('renders when runner_status is "running"', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
  });

  it('renders a "Promoted to autonomous" read-only footer when autonomous is true', () => {
    render(<CardChat card={autonomousCard} cardLogs={noLogs} />);
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Send/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Switch to Autonomous/ })).not.toBeInTheDocument();
    expect(screen.getByText(/Promoted to autonomous — read-only/)).toBeInTheDocument();
  });

  it('renders normally when runner_status is "running" AND autonomous is false', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Send/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Switch to Autonomous/ })).toBeInTheDocument();
  });

  it('compose hides and read-only footer appears when autonomous flips from false to true (simulates promote)', () => {
    const { rerender } = render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
    rerender(<CardChat card={autonomousCard} cardLogs={noLogs} />);
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
    expect(screen.getByText(/Promoted to autonomous — read-only/)).toBeInTheDocument();
  });
});

describe('CardChat — layout classes', () => {
  it('log container uses flex-1 (not max-h-[200px]) when session is active', () => {
    const { container } = render(<CardChat card={runningCard} cardLogs={noLogs} />);
    // Find the log container by its bg-dim class (unique to the log div)
    const logContainer = container.querySelector('.bg-\\[var\\(--bg-dim\\)\\]');
    expect(logContainer).not.toBeNull();
    expect(logContainer!.className).toContain('flex-1');
    expect(logContainer!.className).not.toContain('max-h-[200px]');
  });

  it('root container uses h-full so it fills flex parent', () => {
    const { container } = render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const root = container.firstChild as HTMLElement;
    expect(root.className).toContain('h-full');
  });

  it('chat input textarea and Send button are rendered and accessible', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Send/ })).toBeInTheDocument();
  });
});

describe('CardChat — Enter-to-send', () => {
  it('Enter sends message and clears textarea', async () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/) as HTMLTextAreaElement;

    fireEvent.change(textarea, { target: { value: 'Hello agent' } });
    expect(textarea.value).toBe('Hello agent');

    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
    });

    expect(mockSendCardMessage).toHaveBeenCalledOnce();
    expect(mockSendCardMessage).toHaveBeenCalledWith('test', 'TEST-001', 'Hello agent');
    expect(textarea.value).toBe('');
  });

  it('Shift+Enter does NOT submit; inserts newline behavior (not prevented)', async () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/) as HTMLTextAreaElement;

    fireEvent.change(textarea, { target: { value: 'line1' } });
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true });

    // No API call
    expect(mockSendCardMessage).not.toHaveBeenCalled();
    // Textarea still has the content (default not prevented = browser would insert \n, but we just confirm no submit)
    expect(textarea.value).toBe('line1');
  });
});

describe('CardChat — over-limit guard', () => {
  it('Send button is disabled when content exceeds 8000 chars', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/);
    const overLimit = 'a'.repeat(8001);

    fireEvent.change(textarea, { target: { value: overLimit } });

    const sendBtn = screen.getByRole('button', { name: /Send/ });
    expect(sendBtn).toBeDisabled();
  });

  it('Send button is enabled when content is within limit', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/);

    fireEvent.change(textarea, { target: { value: 'valid message' } });

    const sendBtn = screen.getByRole('button', { name: /Send/ });
    expect(sendBtn).not.toBeDisabled();
  });
});

describe('CardChat — Switch to Autonomous button', () => {
  it('is visible when card.autonomous === false and runner_status === "running"', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByRole('button', { name: /Switch to Autonomous/ })).toBeInTheDocument();
  });

  it('is hidden when card.autonomous === true', () => {
    render(<CardChat card={autonomousCard} cardLogs={noLogs} />);
    expect(screen.queryByRole('button', { name: /Switch to Autonomous/ })).not.toBeInTheDocument();
  });

  it('opens ConfirmModal when Switch to Autonomous is clicked', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const btn = screen.getByRole('button', { name: /Switch to Autonomous/ });
    fireEvent.click(btn);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Promote to autonomous?')).toBeInTheDocument();
  });

  it('calls api.promoteCardToAutonomous after confirming in modal', async () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    fireEvent.click(screen.getByRole('button', { name: /Switch to Autonomous/ }));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Promote' }));
    });

    expect(mockPromoteCardToAutonomous).toHaveBeenCalledOnce();
    expect(mockPromoteCardToAutonomous).toHaveBeenCalledWith('test', 'TEST-001');
  });

  it('does NOT call api.promoteCardToAutonomous when user cancels in modal', async () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    fireEvent.click(screen.getByRole('button', { name: /Switch to Autonomous/ }));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    });

    expect(mockPromoteCardToAutonomous).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('does NOT call api.promoteCardToAutonomous when user presses Escape', async () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    fireEvent.click(screen.getByRole('button', { name: /Switch to Autonomous/ }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    await act(async () => {
      fireEvent.keyDown(window, { key: 'Escape' });
    });

    expect(mockPromoteCardToAutonomous).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('CardChat — cardLogs prop', () => {
  it('renders log entries passed via cardLogs prop', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'text', content: 'hello from runner' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/hello from runner/)).toBeInTheDocument();
  });

  it('shows "No messages yet" when cardLogs is empty', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByText(/No messages yet/)).toBeInTheDocument();
  });
});

describe('CardChat — message type filter bar', () => {
  it('renders the filter bar with three checkboxes when session is active', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByRole('checkbox', { name: /Text/i })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /Tool calls/i })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /Thinking/i })).toBeInTheDocument();
  });

  it('Text checkbox is checked by default', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByRole('checkbox', { name: /Text/i })).toBeChecked();
  });

  it('Tool calls checkbox is unchecked by default', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByRole('checkbox', { name: /Tool calls/i })).not.toBeChecked();
  });

  it('Thinking checkbox is unchecked by default', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByRole('checkbox', { name: /Thinking/i })).not.toBeChecked();
  });

  it('text messages are shown when Text filter is on (default)', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'text', content: 'text message visible' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/text message visible/)).toBeInTheDocument();
  });

  it('text messages are hidden when Text filter is turned off', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'text', content: 'text message hidden' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    fireEvent.click(screen.getByRole('checkbox', { name: /Text/i }));
    expect(screen.queryByText(/text message hidden/)).not.toBeInTheDocument();
  });

  it('tool_call messages are hidden by default (Tool calls filter off)', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'tool_call', content: 'tool call hidden' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.queryByText(/tool call hidden/)).not.toBeInTheDocument();
  });

  it('tool_call messages are shown when Tool calls filter is turned on', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'tool_call', content: 'tool call shown' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    fireEvent.click(screen.getByRole('checkbox', { name: /Tool calls/i }));
    expect(screen.getByText(/tool call shown/)).toBeInTheDocument();
  });

  it('thinking messages are hidden by default (Thinking filter off)', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'thinking', content: 'thinking hidden' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.queryByText(/thinking hidden/)).not.toBeInTheDocument();
  });

  it('thinking messages are shown when Thinking filter is turned on', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'thinking', content: 'thinking shown' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    fireEvent.click(screen.getByRole('checkbox', { name: /Thinking/i }));
    expect(screen.getByText(/thinking shown/)).toBeInTheDocument();
  });

  it('user messages are always shown regardless of filters', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'user', content: 'user message always' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/user message always/)).toBeInTheDocument();
  });

  it('stderr messages are always shown regardless of filters', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'stderr', content: 'stderr always shown' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/stderr always shown/)).toBeInTheDocument();
  });

  it('system messages are always shown regardless of filters', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'system', content: 'system always shown' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/system always shown/)).toBeInTheDocument();
  });

  it('gap messages are always shown regardless of filters', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'gap', content: 'gap always shown' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    expect(screen.getByText(/gap always shown/)).toBeInTheDocument();
  });

  it('toggling Text off then on again restores text message visibility', () => {
    const logs: LogEntry[] = [
      { ts: '2026-01-01T00:00:01Z', card_id: 'TEST-001', type: 'text', content: 'toggle me' },
    ];
    render(<CardChat card={runningCard} cardLogs={logs} />);
    const textCheckbox = screen.getByRole('checkbox', { name: /Text/i });

    // Initially visible
    expect(screen.getByText(/toggle me/)).toBeInTheDocument();

    // Turn off
    fireEvent.click(textCheckbox);
    expect(screen.queryByText(/toggle me/)).not.toBeInTheDocument();

    // Turn back on
    fireEvent.click(textCheckbox);
    expect(screen.getByText(/toggle me/)).toBeInTheDocument();
  });

  it('filter bar remains visible when session is not running (transcript stays filterable)', () => {
    render(<CardChat card={stoppedCard} cardLogs={noLogs} />);
    expect(screen.getByRole('checkbox', { name: /Text/i })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /Tool calls/i })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /Thinking/i })).toBeInTheDocument();
  });
});

describe('CardChat — error state lifecycle', () => {
  it('shows the error banner after a failed send', async () => {
    mockSendCardMessage.mockRejectedValueOnce({ error: 'network down' });
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/) as HTMLTextAreaElement;

    fireEvent.change(textarea, { target: { value: 'first attempt' } });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
    });

    expect(screen.getByText('network down')).toBeInTheDocument();
  });

  it('clears the error banner after a subsequent successful send', async () => {
    mockSendCardMessage.mockRejectedValueOnce({ error: 'network down' });
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const textarea = screen.getByPlaceholderText(/Type a message/) as HTMLTextAreaElement;

    // First send fails, error visible.
    fireEvent.change(textarea, { target: { value: 'first' } });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
    });
    expect(screen.getByText('network down')).toBeInTheDocument();

    // Second send succeeds — error banner should disappear.
    mockSendCardMessage.mockResolvedValueOnce({ ok: true, message_id: 'msg-2' });
    fireEvent.change(textarea, { target: { value: 'second' } });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
    });
    expect(screen.queryByText('network down')).not.toBeInTheDocument();
  });
});
