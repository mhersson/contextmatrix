import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CardChat } from './CardChat';
import type { Card } from '../../types';

// Mock useRunnerLogs — no SSE in tests
vi.mock('../../hooks/useRunnerLogs', () => ({
  useRunnerLogs: vi.fn().mockReturnValue({ logs: [], connected: false, error: null, clear: vi.fn() }),
}));

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
    const { container } = render(<CardChat card={stoppedCard} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders when runner_status is "running"', () => {
    render(<CardChat card={runningCard} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeInTheDocument();
  });
});

describe('CardChat — Enter-to-send', () => {
  it('Enter sends message and clears textarea', async () => {
    render(<CardChat card={runningCard} />);
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
    render(<CardChat card={runningCard} />);
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
    render(<CardChat card={runningCard} />);
    const textarea = screen.getByPlaceholderText(/Type a message/);
    const overLimit = 'a'.repeat(8001);

    fireEvent.change(textarea, { target: { value: overLimit } });

    const sendBtn = screen.getByRole('button', { name: /Send/ });
    expect(sendBtn).toBeDisabled();
  });

  it('Send button is enabled when content is within limit', () => {
    render(<CardChat card={runningCard} />);
    const textarea = screen.getByPlaceholderText(/Type a message/);

    fireEvent.change(textarea, { target: { value: 'valid message' } });

    const sendBtn = screen.getByRole('button', { name: /Send/ });
    expect(sendBtn).not.toBeDisabled();
  });
});

describe('CardChat — Switch to Autonomous button', () => {
  it('is visible when card.autonomous === false and runner_status === "running"', () => {
    render(<CardChat card={runningCard} />);
    expect(screen.getByRole('button', { name: /Switch to Autonomous/ })).toBeInTheDocument();
  });

  it('is hidden when card.autonomous === true', () => {
    render(<CardChat card={autonomousCard} />);
    expect(screen.queryByRole('button', { name: /Switch to Autonomous/ })).not.toBeInTheDocument();
  });

  it('calls api.promoteCardToAutonomous after confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    render(<CardChat card={runningCard} />);
    const btn = screen.getByRole('button', { name: /Switch to Autonomous/ });

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(mockPromoteCardToAutonomous).toHaveBeenCalledOnce();
    expect(mockPromoteCardToAutonomous).toHaveBeenCalledWith('test', 'TEST-001');
  });

  it('does NOT call api.promoteCardToAutonomous when user cancels confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(<CardChat card={runningCard} />);
    const btn = screen.getByRole('button', { name: /Switch to Autonomous/ });

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(mockPromoteCardToAutonomous).not.toHaveBeenCalled();
  });
});
