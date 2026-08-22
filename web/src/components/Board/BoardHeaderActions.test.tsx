import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { BoardHeaderActions } from './BoardHeaderActions';

describe('BoardHeaderActions', () => {
  const base = {
    onOpenSettings: vi.fn(),
    onToggleConsole: vi.fn(),
    onStopAll: vi.fn(),
  };

  it('always offers Settings and calls back on click', () => {
    const onOpenSettings = vi.fn();
    render(<BoardHeaderActions {...base} onOpenSettings={onOpenSettings} />);
    fireEvent.click(screen.getByRole('button', { name: /settings/i }));
    expect(onOpenSettings).toHaveBeenCalledTimes(1);
  });

  it('shows the Console toggle only when a task backend is configured, with pressed state', () => {
    const { rerender } = render(<BoardHeaderActions {...base} />);
    expect(screen.queryByRole('button', { name: /console/i })).toBeNull();

    const onToggleConsole = vi.fn();
    rerender(<BoardHeaderActions {...base} taskBackendConfigured consoleOpen={false} onToggleConsole={onToggleConsole} />);
    const btn = screen.getByRole('button', { name: /console/i });
    expect(btn).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(btn);
    expect(onToggleConsole).toHaveBeenCalledTimes(1);

    rerender(<BoardHeaderActions {...base} taskBackendConfigured consoleOpen onToggleConsole={onToggleConsole} />);
    expect(screen.getByRole('button', { name: /console/i })).toHaveAttribute('aria-pressed', 'true');
  });

  it('shows Stop All only while workers are active and confirms before stopping', () => {
    const onStopAll = vi.fn();
    const { rerender } = render(<BoardHeaderActions {...base} onStopAll={onStopAll} />);
    expect(screen.queryByRole('button', { name: /stop all/i })).toBeNull();

    rerender(<BoardHeaderActions {...base} hasActiveWorkers onStopAll={onStopAll} />);
    fireEvent.click(screen.getByRole('button', { name: /^stop all$/i }));
    expect(onStopAll).not.toHaveBeenCalled();
    const dialog = screen.getByRole('dialog');

    fireEvent.click(within(dialog).getByRole('button', { name: /stop all/i }));
    expect(onStopAll).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
