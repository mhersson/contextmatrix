import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BoardBand } from './BoardBand';

describe('BoardBand', () => {
  const props = {
    projectName: 'contextmatrix',
    displayName: 'ContextMatrix · core',
    activeAgents: 4,
    openCount: 23,
    inReviewCount: 7,
    shippedToday: 3,
    onCreateCard: vi.fn(),
  };

  it('shows open · in-review · shipped-today', () => {
    render(<BoardBand {...props} />);
    expect(screen.getByText(/23 open/)).toBeInTheDocument();
    expect(screen.getByText(/7 in review/)).toBeInTheDocument();
    expect(screen.getByText(/3 shipped today/)).toBeInTheDocument();
  });

  it('invokes onCreateCard when +New Card is clicked', () => {
    const onCreateCard = vi.fn();
    render(<BoardBand {...props} onCreateCard={onCreateCard} />);
    fireEvent.click(screen.getByRole('button', { name: /new card/i }));
    expect(onCreateCard).toHaveBeenCalledTimes(1);
  });

  it('shows shipped-7d delta when shipped7d + prior7d are provided', () => {
    render(
      <BoardBand
        projectName="p" displayName="P" activeAgents={1} openCount={1} inReviewCount={0} shippedToday={0}
        onCreateCard={() => {}}
        shippedLast7d={14} shippedPrior7d={11}
      />
    );
    expect(screen.getByText(/14 shipped this week/)).toBeInTheDocument();
    expect(screen.getByText(/\+27%/)).toBeInTheDocument();
  });

  it('renders a collapse toggle in the crumb row when a handler is provided', () => {
    const onToggleCollapsed = vi.fn();
    const { container } = render(<BoardBand {...props} onToggleCollapsed={onToggleCollapsed} />);

    const toggle = screen.getByRole('button', { name: /collapse board header/i });
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(container.querySelector('.board-band__top')).toContainElement(toggle);

    fireEvent.click(toggle);
    expect(onToggleCollapsed).toHaveBeenCalledTimes(1);
  });

  it('omits the collapse toggle without a handler', () => {
    render(<BoardBand {...props} />);
    expect(screen.queryByRole('button', { name: /board header/i })).toBeNull();
  });

  it('renders the actions slot before New Card inside the main-row actions', () => {
    const { container } = render(
      <BoardBand {...props} actions={<button type="button">Console</button>} />
    );
    const actions = container.querySelector('.board-band__actions');
    const consoleBtn = screen.getByRole('button', { name: 'Console' });
    const newCard = screen.getByRole('button', { name: /new card/i });
    expect(actions).toContainElement(consoleBtn);
    expect(actions).toContainElement(newCard);
    expect(consoleBtn.compareDocumentPosition(newCard) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('renders a sidebar menu button in the crumb row when onOpenSidebar is provided', () => {
    const onOpenSidebar = vi.fn();
    const { container } = render(<BoardBand {...props} onOpenSidebar={onOpenSidebar} />);
    const btn = screen.getByRole('button', { name: /open menu/i });
    expect(container.querySelector('.board-band__top')).toContainElement(btn);
    fireEvent.click(btn);
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);
  });

  it('omits the sidebar menu button without a handler', () => {
    render(<BoardBand {...props} />);
    expect(screen.queryByRole('button', { name: /open menu/i })).toBeNull();
  });
});
