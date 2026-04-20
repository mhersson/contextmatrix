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
  it('returns null when runner_status is not "running"', () => {
    const { container } = render(<CardChat card={stoppedCard} cardLogs={noLogs} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders when runner_status is "running"', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
  });

  it('returns null when runner_status is "running" AND autonomous is true', () => {
    const { container } = render(<CardChat card={autonomousCard} cardLogs={noLogs} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Send/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Switch to Autonomous/ })).not.toBeInTheDocument();
  });

  it('renders normally when runner_status is "running" AND autonomous is false', () => {
    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Send/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Switch to Autonomous/ })).toBeInTheDocument();
  });

  it('chat UI disappears when autonomous flips from false to true (simulates promote)', () => {
    const { container, rerender } = render(<CardChat card={runningCard} cardLogs={noLogs} />);
    // Initially visible
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
    // Simulate HITL→Auto promotion: re-render with autonomous=true
    rerender(<CardChat card={autonomousCard} cardLogs={noLogs} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
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

  it('calls api.promoteCardToAutonomous after confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const btn = screen.getByRole('button', { name: /Switch to Autonomous/ });

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(mockPromoteCardToAutonomous).toHaveBeenCalledOnce();
    expect(mockPromoteCardToAutonomous).toHaveBeenCalledWith('test', 'TEST-001');
  });

  it('does NOT call api.promoteCardToAutonomous when user cancels confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(<CardChat card={runningCard} cardLogs={noLogs} />);
    const btn = screen.getByRole('button', { name: /Switch to Autonomous/ });

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(mockPromoteCardToAutonomous).not.toHaveBeenCalled();
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
