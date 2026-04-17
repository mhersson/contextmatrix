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

const autonomousCard: Card = { ...baseCard, autonomous: true };
const nonAutonomousCard: Card = { ...baseCard, autonomous: false };

const defaultProps = {
  canClaim: false,
  canRelease: false,
  onClaim: vi.fn(),
  onRelease: vi.fn(),
  canStop: false,
  onRun: vi.fn().mockResolvedValue(undefined),
  onStop: vi.fn().mockResolvedValue(undefined),
};

describe('CardPanelAgent — Run Now and Interactive checkbox', () => {
  it('Run button is visible when canRun=true and card.autonomous=false', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
      />,
    );
    expect(screen.getByRole('button', { name: 'Run Now' })).toBeInTheDocument();
  });

  it('Run button is visible when canRun=true and card.autonomous=true', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={autonomousCard}
        canRun
      />,
    );
    expect(screen.getByRole('button', { name: 'Run Now' })).toBeInTheDocument();
  });

  it('Run button is NOT visible when canRun=false', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun={false}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Run Now' })).not.toBeInTheDocument();
  });

  it('Interactive checkbox is visible when canRun=true', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
      />,
    );
    expect(screen.getByRole('checkbox')).toBeInTheDocument();
    expect(screen.getByText('Interactive')).toBeInTheDocument();
  });

  it('Interactive checkbox is NOT visible when canRun=false', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun={false}
      />,
    );
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  });

  it('Interactive checkbox defaults to checked for non-autonomous card', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
      />,
    );
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });

  it('Interactive checkbox defaults to unchecked for autonomous card', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={autonomousCard}
        canRun
      />,
    );
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it('clicking Run with Interactive checkbox CHECKED calls onRun(true)', async () => {
    const onRun = vi.fn().mockResolvedValue(undefined);
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
        onRun={onRun}
      />,
    );
    // Non-autonomous card defaults to interactive=true (checked)
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));
    });
    expect(onRun).toHaveBeenCalledOnce();
    expect(onRun).toHaveBeenCalledWith(true);
  });

  it('clicking Run with Interactive checkbox UNCHECKED calls onRun(false)', async () => {
    const onRun = vi.fn().mockResolvedValue(undefined);
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
        onRun={onRun}
      />,
    );
    // Uncheck the checkbox first
    fireEvent.click(screen.getByRole('checkbox'));
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run Now' }));
    });
    expect(onRun).toHaveBeenCalledOnce();
    expect(onRun).toHaveBeenCalledWith(false);
  });

  it('Interactive checkbox tooltip is set correctly', () => {
    render(
      <CardPanelAgent
        {...defaultProps}
        card={nonAutonomousCard}
        canRun
      />,
    );
    const label = screen.getByText('Interactive').closest('label');
    expect(label).toHaveAttribute(
      'title',
      'Start the task in interactive HITL mode. Leave unchecked to run the workflow unattended (autonomous mode).',
    );
  });
});
