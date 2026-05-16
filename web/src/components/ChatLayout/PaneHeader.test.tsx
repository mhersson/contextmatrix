import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PaneHeader } from './PaneHeader';
import type { AvailableChat } from './types';

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
