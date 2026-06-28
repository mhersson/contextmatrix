import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { ChatPane } from './ChatPane';

// Mock isTouchDevice so we can control the draggable path.
vi.mock('../../utils/isTouchDevice', () => ({
  isTouchDevice: vi.fn(() => false),
}));

// Mock all ChatPane dependencies that require external context.
vi.mock('./PaneAccentStripe', () => ({
  PaneAccentStripe: () => null,
}));
vi.mock('../../hooks/useChatLiveData', () => ({
  useChatLiveData: () => null,
}));
vi.mock('../../utils/chatModels', () => ({
  useChatModels: () => ({ models: [], source: 'config' }),
  contextPct: () => 0,
  modelMaxTokens: () => 0,
  usageColor: () => 'var(--fg)',
}));
import { isTouchDevice } from '../../utils/isTouchDevice';
import { PANE_SOURCE_MIME } from './dragProtocol';

function defaultProps(overrides: Partial<Parameters<typeof ChatPane>[0]> = {}) {
  return {
    slot: 'TL' as const,
    chatId: 'chat-1',
    isFocused: false,
    connected: false,
    showSplit: false,
    showClose: true,
    draggingChatId: 'chat-1',
    onFocus: vi.fn(),
    onClose: vi.fn(),
    onSplit: vi.fn(),
    onDropChat: vi.fn(),
    onMovePane: vi.fn(),
    ...overrides,
  };
}

/**
 * Build a DataTransfer-like object that dataTransfer.getData() will use.
 * jsdom's fireEvent doesn't set dataTransfer, so we simulate it.
 */
function makeDropEvent(data: Record<string, string>) {
  const dataTransfer = {
    data: { ...data },
    getData(type: string) { return this.data[type] ?? ''; },
    setData(type: string, value: string) { this.data[type] = value; },
    dropEffect: 'none',
  };
  return {
    preventDefault: vi.fn(),
    dataTransfer,
  };
}

describe('ChatPane — drop routing', () => {
  beforeEach(() => {
    vi.mocked(isTouchDevice).mockReturnValue(false);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('routes pane-source drop to onMovePane, not onDropChat', () => {
    const onMovePane = vi.fn();
    const onDropChat = vi.fn();
    const { container } = render(
      <ChatPane {...defaultProps({ slot: 'TR', onMovePane, onDropChat, draggingChatId: 'chat-1' })} />,
    );
    const pane = container.querySelector('.chat-pane')!;

    const dropEvent = makeDropEvent({
      'text/plain': 'chat-1',
      [PANE_SOURCE_MIME]: 'TL',  // from-slot TL, dropping onto TR
    });

    // Simulate handleDrop by calling the component's onDrop handler.
    fireEvent.drop(pane, {
      dataTransfer: dropEvent.dataTransfer,
    });

    expect(onMovePane).toHaveBeenCalledWith('TL');
    expect(onDropChat).not.toHaveBeenCalled();
  });

  it('routes sidebar drop (no source-slot MIME) to onDropChat', () => {
    const onMovePane = vi.fn();
    const onDropChat = vi.fn();
    const { container } = render(
      <ChatPane {...defaultProps({ slot: 'TR', onMovePane, onDropChat, draggingChatId: 'chat-1' })} />,
    );
    const pane = container.querySelector('.chat-pane')!;

    fireEvent.drop(pane, {
      dataTransfer: makeDropEvent({
        'text/plain': 'chat-1',
        // no PANE_SOURCE_MIME → sidebar drag
      }).dataTransfer,
    });

    expect(onDropChat).toHaveBeenCalledWith('chat-1');
    expect(onMovePane).not.toHaveBeenCalled();
  });

  it('drops from same pane onto itself: neither callback fires', () => {
    const onMovePane = vi.fn();
    const onDropChat = vi.fn();
    const { container } = render(
      <ChatPane {...defaultProps({ slot: 'TL', onMovePane, onDropChat, draggingChatId: 'chat-1' })} />,
    );
    const pane = container.querySelector('.chat-pane')!;

    fireEvent.drop(pane, {
      dataTransfer: makeDropEvent({
        'text/plain': 'chat-1',
        [PANE_SOURCE_MIME]: 'TL',  // same slot
      }).dataTransfer,
    });

    expect(onMovePane).not.toHaveBeenCalled();
    expect(onDropChat).not.toHaveBeenCalled();
  });

  it('PaneHeader is not draggable when chatId is null (no chat loaded)', () => {
    // TOUCH is a module-scope constant (false in test env). The other half of
    // the guard is chatId != null — verify it controls draggability too.
    const { container } = render(
      <ChatPane {...defaultProps({ slot: 'TL', chatId: null })} />,
    );
    const header = container.querySelector('.chat-pane-header')!;
    expect(header.getAttribute('draggable')).not.toBe('true');
  });

  it('Drop with malformed source-slot value falls through to onDropChat', () => {
    const onMovePane = vi.fn();
    const onDropChat = vi.fn();
    const { container } = render(
      <ChatPane {...defaultProps({ slot: 'TR', onMovePane, onDropChat, draggingChatId: 'chat-1' })} />,
    );
    const pane = container.querySelector('.chat-pane')!;

    // 'XX' is not a valid Slot — should fall through to sidebar path.
    fireEvent.drop(pane, {
      dataTransfer: makeDropEvent({
        [PANE_SOURCE_MIME]: 'XX',
        'text/plain': 'chat-1',
      }).dataTransfer,
    });

    expect(onMovePane).not.toHaveBeenCalled();
    expect(onDropChat).toHaveBeenCalledWith('chat-1');
  });
});
