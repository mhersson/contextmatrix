import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { BoardHeaderActions } from './BoardHeaderActions';

function renderActions(ui: React.ReactElement) {
  return render(ui, { wrapper: MemoryRouter });
}

describe('BoardHeaderActions', () => {
  const base = {
    settingsHref: '/projects/p/settings',
    onToggleConsole: vi.fn(),
    onStopAll: vi.fn(),
  };

  it('always offers Settings as a link to the settings route', () => {
    renderActions(<BoardHeaderActions {...base} />);
    expect(screen.getByRole('link', { name: /settings/i })).toHaveAttribute('href', '/projects/p/settings');
  });

  it('shows the Console toggle only when a task backend is configured, with pressed state', () => {
    const { rerender } = renderActions(<BoardHeaderActions {...base} />);
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
    const { rerender } = renderActions(<div className="slot"><BoardHeaderActions {...base} onStopAll={onStopAll} /></div>);
    expect(screen.queryByRole('button', { name: /stop all/i })).toBeNull();

    rerender(<div className="slot"><BoardHeaderActions {...base} hasActiveWorkers onStopAll={onStopAll} /></div>);
    fireEvent.click(screen.getByRole('button', { name: /^stop all$/i }));
    expect(onStopAll).not.toHaveBeenCalled();
    const dialog = screen.getByRole('dialog');
    // The band is a sticky stacking context on phones; the dialog must escape
    // the slot it is rendered from so it is not trapped beneath board chrome.
    expect(dialog.closest('.slot')).toBeNull();

    fireEvent.click(within(dialog).getByRole('button', { name: /stop all/i }));
    expect(onStopAll).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
