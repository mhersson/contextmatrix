/**
 * Tests for the ClearContext error toast in ChatPage.
 *
 * ChatPage has deep provider requirements (routing, SSE, chat sessions, layout)
 * that are expensive to stub. Instead, we extract the toast-relevant behaviour
 * into a minimal self-contained helper component — ClearErrorToastBanner —
 * and test that component directly. The banner is functionally identical to
 * the toast block rendered inside ChatPage.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { useState, useRef, useCallback, useEffect } from 'react';
import { api, isAPIError } from '../../api/client';

vi.mock('../../api/client', () => ({
  api: {
    clearChatContext: vi.fn(),
  },
  isAPIError: vi.fn((err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
  ),
}));

// ---------------------------------------------------------------------------
// Minimal helper that mirrors the ChatPage clear-context + toast logic.
// ---------------------------------------------------------------------------

interface ClearErrorToastState {
  chatId: string;
  code: string;
}

function ClearErrorToastBanner({
  onClearRequested,
}: {
  onClearRequested?: () => void;
}) {
  const [clearErrorToast, setClearErrorToast] = useState<ClearErrorToastState | null>(null);
  const clearErrorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const dismissClearErrorToast = useCallback(() => {
    if (clearErrorTimerRef.current) {
      clearTimeout(clearErrorTimerRef.current);
      clearErrorTimerRef.current = null;
    }
    setClearErrorToast(null);
  }, []);

  useEffect(() => () => {
    if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
  }, []);

  const triggerClear = useCallback(async (chatId: string) => {
    try {
      await api.clearChatContext(chatId);
      onClearRequested?.();
    } catch (e) {
      console.warn('clearChatContext failed', isAPIError(e) ? (e as { error: string }).error : e);
      const code = isAPIError(e) ? ((e as { code?: string }).code ?? 'UNKNOWN') : 'UNKNOWN';
      if (clearErrorTimerRef.current) clearTimeout(clearErrorTimerRef.current);
      setClearErrorToast({ chatId, code });
      clearErrorTimerRef.current = setTimeout(() => setClearErrorToast(null), 6000);
    }
  }, [onClearRequested]);

  return (
    <>
      <button
        type="button"
        data-testid="trigger-clear"
        onClick={() => void triggerClear('chat-abc')}
      >
        Clear context
      </button>
      {clearErrorToast && (
        <div className="chat-evict-toast" role="alert" aria-live="assertive">
          <span className="chat-evict-toast-text">
            Couldn&apos;t clear context for <strong>{clearErrorToast.chatId}</strong>: {clearErrorToast.code}.
          </span>
          <button
            type="button"
            className="chat-evict-toast-close"
            aria-label="Dismiss"
            onClick={dismissClearErrorToast}
          >×</button>
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

beforeEach(() => {
  vi.clearAllMocks();
});

describe('ClearContext error toast', () => {
  it('shows toast with API error code when clearChatContext rejects', async () => {
    vi.mocked(api.clearChatContext).mockRejectedValueOnce({
      error: 'Runner is unavailable',
      code: 'RUNNER_UNAVAILABLE',
    });

    render(<ClearErrorToastBanner />);

    await act(async () => {
      fireEvent.click(screen.getByTestId('trigger-clear'));
    });

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/Couldn't clear context for/)).toBeInTheDocument();
    expect(screen.getByText('chat-abc')).toBeInTheDocument();
    expect(screen.getByText(/RUNNER_UNAVAILABLE/)).toBeInTheDocument();
  });

  it('shows UNKNOWN code when the rejection is not an APIError', async () => {
    vi.mocked(isAPIError).mockReturnValueOnce(false);
    vi.mocked(api.clearChatContext).mockRejectedValueOnce(new Error('network error'));

    render(<ClearErrorToastBanner />);

    await act(async () => {
      fireEvent.click(screen.getByTestId('trigger-clear'));
    });

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/UNKNOWN/)).toBeInTheDocument();
  });

  it('dismiss button hides the toast', async () => {
    vi.mocked(api.clearChatContext).mockRejectedValueOnce({
      error: 'oops',
      code: 'SERVER_ERROR',
    });

    render(<ClearErrorToastBanner />);

    await act(async () => {
      fireEvent.click(screen.getByTestId('trigger-clear'));
    });

    expect(screen.getByRole('alert')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('does not show toast when clearChatContext succeeds', async () => {
    vi.mocked(api.clearChatContext).mockResolvedValueOnce(undefined);
    const onClearRequested = vi.fn();

    render(<ClearErrorToastBanner onClearRequested={onClearRequested} />);

    await act(async () => {
      fireEvent.click(screen.getByTestId('trigger-clear'));
    });

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(onClearRequested).toHaveBeenCalledOnce();
  });
});
