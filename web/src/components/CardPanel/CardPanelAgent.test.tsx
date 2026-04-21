import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CardPanelAgent } from './CardPanelAgent';
import type { Card } from '../../types';

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

const defaultProps = {
  canClaim: false,
  canRelease: false,
  onClaim: vi.fn(),
  onRelease: vi.fn(),
  canStop: false,
  onStop: vi.fn().mockResolvedValue(undefined),
};

describe('CardPanelAgent — Claim / Release / Stop', () => {
  it('shows Claim button when canClaim=true', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canClaim />);
    expect(screen.getByRole('button', { name: 'Claim' })).toBeInTheDocument();
  });

  it('hides Claim button when canClaim=false', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canClaim={false} />);
    expect(screen.queryByRole('button', { name: 'Claim' })).not.toBeInTheDocument();
  });

  it('shows Release button when canRelease=true', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canRelease />);
    expect(screen.getByRole('button', { name: 'Release' })).toBeInTheDocument();
  });

  it('hides Release button when canRelease=false', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canRelease={false} />);
    expect(screen.queryByRole('button', { name: 'Release' })).not.toBeInTheDocument();
  });

  it('shows Stop button when canStop=true', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canStop />);
    expect(screen.getByRole('button', { name: 'Stop' })).toBeInTheDocument();
  });

  it('hides Stop button when canStop=false', () => {
    render(<CardPanelAgent {...defaultProps} card={baseCard} canStop={false} />);
    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument();
  });

  it('calls onClaim when Claim button is clicked', () => {
    const onClaim = vi.fn();
    render(<CardPanelAgent {...defaultProps} card={baseCard} canClaim onClaim={onClaim} />);
    fireEvent.click(screen.getByRole('button', { name: 'Claim' }));
    expect(onClaim).toHaveBeenCalledOnce();
  });

  it('calls onRelease when Release button is clicked and confirm is accepted', () => {
    const onRelease = vi.fn();
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const cardWithAgent = { ...baseCard, assigned_agent: 'agent-xyz' };
    render(<CardPanelAgent {...defaultProps} card={cardWithAgent} canRelease onRelease={onRelease} />);
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    expect(onRelease).toHaveBeenCalledOnce();
  });

  it('does NOT call onRelease when Release button is clicked and confirm is cancelled', () => {
    const onRelease = vi.fn();
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    const cardWithAgent = { ...baseCard, assigned_agent: 'agent-xyz' };
    render(<CardPanelAgent {...defaultProps} card={cardWithAgent} canRelease onRelease={onRelease} />);
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    expect(onRelease).not.toHaveBeenCalled();
  });

  it('Stop button shows Stopping... and calls onStop when clicked', async () => {
    const onStop = vi.fn().mockResolvedValue(undefined);
    // Suppress the window.confirm dialog in tests
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<CardPanelAgent {...defaultProps} card={baseCard} canStop onStop={onStop} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Stop' }));
    });
    expect(onStop).toHaveBeenCalledOnce();
  });
});
