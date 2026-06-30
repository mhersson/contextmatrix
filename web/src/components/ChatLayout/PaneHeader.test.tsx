import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, fireEvent } from '@testing-library/react';
import { PaneHeader } from './PaneHeader';
import type { AvailableChat } from './types';
import { setChatLiveData, clearChatLiveData } from '../../hooks/useChatLiveData';
import { useChatModels } from '../../utils/chatModels';

// Mock useChatModels so PaneContextUsage can render a model label without
// an API call. The default return covers 'config' mode (model-x, 200k tokens)
// so existing tests run unmodified. Individual tests that need a different
// source/model can use vi.mocked(useChatModels).mockReturnValueOnce(...).
vi.mock('../../utils/chatModels', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../utils/chatModels')>();
  return {
    ...actual,
    useChatModels: vi.fn().mockReturnValue({
      models: [{ id: 'model-x', label: 'Model X', max_tokens: 200_000 }],
      source: 'config' as const,
    }),
  };
});

const baseProps = {
  slot: 'TL' as const,
  draggable: false,
  chatId: 'chat-1',
  isFocused: false,
  showSplit: false,
  showClose: true,
  onClose: () => {},
  onSplit: () => {},
};

function activeChat(): AvailableChat {
  return { id: 'chat-1', title: 'Test chat', status: 'active' };
}

function coldChat(): AvailableChat {
  return { id: 'chat-1', title: 'Test chat', status: 'cold' };
}

afterEach(() => {
  clearChatLiveData('chat-1');
  clearChatLiveData('chat-2');
});

describe('PaneHeader Clear context menu', () => {
  it('renders Clear context menu item when chat is running and onClearContext is set', () => {
    const onClearContext = vi.fn();
    render(
      <PaneHeader
        {...baseProps}
        chat={activeChat()}
        onClearContext={onClearContext}
        onEndSession={() => {}}
      />,
    );
    fireEvent.click(screen.getByLabelText('More chat actions'));
    expect(screen.getByRole('menuitem', { name: 'Clear context' })).toBeInTheDocument();
  });

  it('hides Clear context when session is cold (matches End Session gating)', () => {
    render(
      <PaneHeader
        {...baseProps}
        chat={coldChat()}
        onClearContext={() => {}}
        onReopenChat={() => {}}
      />,
    );
    fireEvent.click(screen.getByLabelText('More chat actions'));
    expect(screen.queryByRole('menuitem', { name: 'Clear context' })).not.toBeInTheDocument();
    // Reopen should be the visible action for a cold session.
    expect(screen.getByRole('menuitem', { name: 'Reopen' })).toBeInTheDocument();
  });

  it('invokes onClearContext when the menu item is clicked', () => {
    const onClearContext = vi.fn();
    render(
      <PaneHeader
        {...baseProps}
        chat={activeChat()}
        onClearContext={onClearContext}
      />,
    );
    fireEvent.click(screen.getByLabelText('More chat actions'));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Clear context' }));
    expect(onClearContext).toHaveBeenCalledTimes(1);
  });

  it('omits Clear context when onClearContext is undefined (CardChat case)', () => {
    render(
      <PaneHeader
        {...baseProps}
        chat={activeChat()}
        onEndSession={() => {}}
      />,
    );
    fireEvent.click(screen.getByLabelText('More chat actions'));
    expect(screen.queryByRole('menuitem', { name: 'Clear context' })).not.toBeInTheDocument();
  });
});

describe('PaneContextUsage cost glyph', () => {
  it('renders cost glyph with correct value and tooltip when estimatedCostUsd > 0', () => {
    act(() => {
      setChatLiveData('chat-1', {
        model: 'model-x',
        contextTokens: 1000,
        estimatedCostUsd: 0.05,
        promptTokens: 100,
        completionTokens: 50,
        cacheReadTokens: 25,
        cacheCreationTokens: 10,
      });
    });

    render(<PaneHeader {...baseProps} />);

    const glyph = screen.getByText('$0.05');
    expect(glyph).toBeInTheDocument();

    expect(glyph).toHaveAttribute('title', 'Input: 100 · Output: 50 · Cache read: 25 · Cache create: 10');
  });

  it('hides cost glyph when estimatedCostUsd is 0', () => {
    act(() => {
      setChatLiveData('chat-1', {
        model: 'model-x',
        contextTokens: 1000,
        estimatedCostUsd: 0,
        promptTokens: 100,
        completionTokens: 50,
      });
    });

    render(<PaneHeader {...baseProps} />);

    expect(screen.queryByText(/^\$/)).not.toBeInTheDocument();
    expect(screen.getByText(/Model X/)).toBeInTheDocument();
  });

  it('shows independent cost values in two panes with no cross-talk', () => {
    act(() => {
      setChatLiveData('chat-1', {
        model: 'model-x',
        contextTokens: 1000,
        estimatedCostUsd: 0.10,
        promptTokens: 200,
        completionTokens: 100,
      });
      setChatLiveData('chat-2', {
        model: 'model-x',
        contextTokens: 2000,
        estimatedCostUsd: 0.25,
        promptTokens: 400,
        completionTokens: 200,
      });
    });

    const { container: c1 } = render(
      <PaneHeader {...baseProps} chatId="chat-1" />,
    );
    const { container: c2 } = render(
      <PaneHeader {...baseProps} chatId="chat-2" />,
    );

    expect(c1.querySelector('[title]')?.textContent).toContain('Model X');
    expect(c2.querySelector('[title]')?.textContent).toContain('Model X');

    // Each pane shows its own cost.
    expect(c1.textContent).toContain('$0.10');
    expect(c2.textContent).toContain('$0.25');
    expect(c1.textContent).not.toContain('$0.25');
    expect(c2.textContent).not.toContain('$0.10');

    // Updating chat-2's cost does not affect chat-1.
    act(() => {
      setChatLiveData('chat-2', { estimatedCostUsd: 0.99 });
    });
    expect(c1.textContent).toContain('$0.10');
    expect(c1.textContent).not.toContain('$0.99');
    expect(c2.textContent).toContain('$0.99');
  });
});

describe('PaneContextUsage endpoint mode context-window denominator', () => {
  it('uses server max_tokens as the context-window denominator for endpoint source', () => {
    // Override useChatModels for this test only: endpoint source, model-a with 200k tokens.
    vi.mocked(useChatModels).mockReturnValueOnce({
      models: [{ id: 'model-a', label: 'Model A', max_tokens: 200_000 }],
      source: 'endpoint' as const,
    });

    act(() => {
      setChatLiveData('chat-1', {
        model: 'model-a',
        contextTokens: 100_000,
        estimatedCostUsd: 0,
        promptTokens: 0,
        completionTokens: 0,
      });
    });

    render(<PaneHeader {...baseProps} />);

    // 100_000 / 200_000 = 50%. Endpoint mode must use the server-provided
    // max_tokens as the denominator (same path as 'config'), not OpenRouter.
    expect(screen.getByText('50%')).toBeInTheDocument();
    // useOpenRouterContextLengths should stay disabled — no OR network call.
    expect(screen.getByText('Model A')).toBeInTheDocument();
  });
});
