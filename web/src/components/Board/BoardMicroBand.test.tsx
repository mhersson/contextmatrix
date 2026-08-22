import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BoardMicroBand } from './BoardMicroBand';

describe('BoardMicroBand', () => {
  const props = {
    projectName: 'contextmatrix',
    displayName: 'ContextMatrix · core',
    activeAgents: 4,
    openCount: 23,
    inReviewCount: 7,
    stalled: 2,
    shippedToday: 3,
    onCreateCard: vi.fn(),
  };

  it('shows the display name and the inline summary numbers', () => {
    render(<BoardMicroBand {...props} />);
    expect(screen.getByRole('heading', { name: 'ContextMatrix · core' })).toBeInTheDocument();
    expect(screen.getByText('4 agents')).toBeInTheDocument();
    expect(screen.getByText('23 open')).toBeInTheDocument();
    expect(screen.getByText('7 review')).toBeInTheDocument();
    expect(screen.getByText('2 stalled')).toBeInTheDocument();
    expect(screen.getByText('3 today')).toBeInTheDocument();
  });

  it('falls back to the project name without a display name', () => {
    render(<BoardMicroBand {...props} displayName={undefined} />);
    expect(screen.getByRole('heading', { name: 'contextmatrix' })).toBeInTheDocument();
  });

  it('shows the 7d count with delta when prior data exists', () => {
    render(<BoardMicroBand {...props} shippedLast7d={14} shippedPrior7d={11} />);
    expect(screen.getByText(/14 · 7d/)).toBeInTheDocument();
    expect(screen.getByText(/\+27%/)).toBeInTheDocument();
  });

  it('omits the delta when there is no prior window', () => {
    render(<BoardMicroBand {...props} shippedLast7d={14} shippedPrior7d={0} />);
    expect(screen.getByText(/14 · 7d/)).toBeInTheDocument();
    expect(screen.queryByText(/%/)).toBeNull();
  });

  it('invokes onCreateCard when the compact New Card is clicked', () => {
    const onCreateCard = vi.fn();
    render(<BoardMicroBand {...props} onCreateCard={onCreateCard} />);
    fireEvent.click(screen.getByRole('button', { name: /new card/i }));
    expect(onCreateCard).toHaveBeenCalledTimes(1);
  });

  it('renders an expand toggle after New Card when a handler is provided', () => {
    const onToggleCollapsed = vi.fn();
    const { container } = render(<BoardMicroBand {...props} onToggleCollapsed={onToggleCollapsed} />);

    const toggle = screen.getByRole('button', { name: /expand board header/i });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    const actions = container.querySelector('.board-microband__actions');
    expect(actions).toContainElement(toggle);
    const newCard = screen.getByRole('button', { name: /new card/i });
    expect(actions).toContainElement(newCard);
    expect(newCard.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    fireEvent.click(toggle);
    expect(onToggleCollapsed).toHaveBeenCalledTimes(1);
  });

  it('omits the expand toggle without a handler', () => {
    render(<BoardMicroBand {...props} />);
    expect(screen.queryByRole('button', { name: /board header/i })).toBeNull();
  });

  it('renders the actions slot before New Card, with the expand toggle still last', () => {
    const { container } = render(
      <BoardMicroBand {...props} actions={<button type="button">Console</button>} onToggleCollapsed={() => {}} />
    );
    const actions = container.querySelector('.board-microband__actions');
    const consoleBtn = screen.getByRole('button', { name: 'Console' });
    const newCard = screen.getByRole('button', { name: /new card/i });
    const toggle = screen.getByRole('button', { name: /expand board header/i });
    expect(actions).toContainElement(consoleBtn);
    expect(consoleBtn.compareDocumentPosition(newCard) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(newCard.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('renders a sidebar menu button before the title when onOpenSidebar is provided', () => {
    const onOpenSidebar = vi.fn();
    render(<BoardMicroBand {...props} onOpenSidebar={onOpenSidebar} />);
    const btn = screen.getByRole('button', { name: /open menu/i });
    const title = screen.getByRole('heading', { name: 'ContextMatrix · core' });
    expect(btn.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    fireEvent.click(btn);
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);
  });
});
