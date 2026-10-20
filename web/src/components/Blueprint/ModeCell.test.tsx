import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { ModeCell } from './ModeCell';
import { modeLabel } from './mode';
import { api } from '../../api/client';
import { CHAT_SESSIONS_CHANGED_EVENT } from '../../hooks/useChatSessions';
import { BLUEPRINT_MODES } from '../../pages/Chat/blueprintForm';
import { viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import type { BlueprintView } from '../../types';

vi.mock('../../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/client')>()),
  api: { setChatMode: vi.fn() },
}));

function at(over: Partial<BlueprintView> = {}): BlueprintView {
  return { ...viewAtDesignGate(), ...over };
}

function atSignoff(): BlueprintView {
  const v = viewAtDesignGate();
  const i = v.pipeline.findIndex((s) => s.stage === 'signoff');
  v.pipeline = v.pipeline.map((s, j) =>
    j < i ? { ...s, status: 'done' as const } : j === i ? { ...s, status: 'waiting' as const } : s,
  );
  v.open = { prompt_id: 'gate:signoff:v1', kind: 'gate' };
  return v;
}

const trigger = () => screen.getByRole('button', { name: /^Mode:/ });
const pick = (label: string) => fireEvent.click(screen.getByRole('radio', { name: label }));
const confirm = () => screen.getByRole('button', { name: /^Switch/ });

function open(view: BlueprintView = at()) {
  const utils = render(<ModeCell chatId="S1" view={view} />);
  fireEvent.click(trigger());
  return utils;
}

describe('ModeCell', () => {
  beforeEach(() => {
    vi.mocked(api.setChatMode).mockReset().mockResolvedValue({} as never);
  });

  it("opens on the view's mode with the new-blueprint dialog's four modes, focus on the current one", () => {
    open(at({ mode: 'auto' }));
    const radios = screen.getAllByRole('radio');
    expect(radios.map((r) => r.getAttribute('aria-label'))).toEqual(BLUEPRINT_MODES.map((m) => m.label));
    expect(screen.getByRole('radio', { name: modeLabel('auto') })).toBeChecked();
    expect(screen.getByRole('radio', { name: modeLabel('auto') })).toHaveFocus();
    expect(confirm()).toBeDisabled();
  });

  it('sends the picked mode and closes, returning focus to the cell', async () => {
    open();
    pick(modeLabel('auto'));
    fireEvent.click(confirm());
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(api.setChatMode).toHaveBeenCalledWith('S1', 'auto');
    expect(trigger()).toHaveFocus();
  });

  it("keeps showing the view's mode until the architect republishes it", async () => {
    const { rerender } = open();
    pick(modeLabel('auto+emit'));
    fireEvent.click(confirm());
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(trigger().getAttribute('aria-label')).toContain(modeLabel('default'));
    rerender(<ModeCell chatId="S1" view={at({ mode: 'auto+emit', seq: 43 })} />);
    expect(trigger().getAttribute('aria-label')).toContain(modeLabel('auto+emit'));
    expect(trigger().getAttribute('aria-label')).not.toContain(modeLabel('default'));
  });

  it('confirms yolo with its own run-naming button and sends it only from there', async () => {
    open();
    pick(modeLabel('auto'));
    const plain = confirm().textContent;
    pick(modeLabel('yolo'));
    expect(api.setChatMode).not.toHaveBeenCalled();
    expect(confirm().textContent).not.toBe(plain);
    fireEvent.click(confirm());
    await waitFor(() => expect(api.setChatMode).toHaveBeenCalledWith('S1', 'yolo'));
  });

  it('stays open with the error when the switch fails, and announces nothing', async () => {
    const changed = vi.fn();
    window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, changed);
    try {
      vi.mocked(api.setChatMode).mockRejectedValueOnce({ error: 'architect unreachable', code: 'INTERNAL_ERROR' });
      open();
      pick(modeLabel('auto'));
      fireEvent.click(confirm());
      expect(await screen.findByRole('alert')).toHaveTextContent('architect unreachable');
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      expect(confirm()).toBeEnabled();
      expect(changed).not.toHaveBeenCalled();
      fireEvent.click(confirm());
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(changed).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, changed);
    }
  });

  it('closes on Escape and on Cancel without switching', () => {
    open();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(trigger()).toHaveFocus();
    fireEvent.click(trigger());
    pick(modeLabel('auto'));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(api.setChatMode).not.toHaveBeenCalled();
  });

  it.each(['emitting', 'emitted', 'done', 'ended'])('is locked once the view is %s, still showing the mode', (state) => {
    render(<ModeCell chatId="S1" view={at({ mode: 'auto', state })} />);
    expect(screen.queryByRole('button', { name: /^Mode:/ })).not.toBeInTheDocument();
    expect(screen.getByText(modeLabel('auto'))).toBeInTheDocument();
  });

  it('is locked once the sign-off is approved, even while the state is idle', () => {
    const v = atSignoff();
    v.pipeline = v.pipeline.map((s) => (s.stage === 'signoff' ? { ...s, status: 'done' as const } : s));
    render(<ModeCell chatId="S1" view={{ ...v, state: 'idle', open: undefined }} />);
    expect(screen.queryByRole('button', { name: /^Mode:/ })).not.toBeInTheDocument();
  });

  it.each(['new', 'running', 'waiting', 'idle', 'failed'])('can change while the view is %s', (state) => {
    render(<ModeCell chatId="S1" view={at({ state })} />);
    expect(trigger()).toBeEnabled();
  });

  it('closes an open popover when a republished view locks the mode', () => {
    const { rerender } = open();
    pick(modeLabel('yolo'));
    rerender(<ModeCell chatId="S1" view={at({ state: 'emitting', seq: 43 })} />);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Mode:/ })).not.toBeInTheDocument();
    expect(api.setChatMode).not.toHaveBeenCalled();
  });

  it('notes that a waiting round goes to the responder, only when leaving Default', () => {
    open(at({ open: { prompt_id: 'round:1', kind: 'round' } }));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    pick(modeLabel('auto'));
    expect(screen.getByRole('note')).toBeInTheDocument();
    pick(modeLabel('default'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('notes no handover for a waiting round when the blueprint is already past Default', () => {
    open(at({ mode: 'auto', open: { prompt_id: 'round:1', kind: 'round' } }));
    pick(modeLabel('yolo'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    pick(modeLabel('default'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('notes that a waiting mockup goes to the responder, only when leaving Default', () => {
    open(at({ open: { prompt_id: 'mockup:1', kind: 'mockup' } }));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    pick(modeLabel('auto'));
    expect(screen.getByRole('note')).toBeInTheDocument();
    pick(modeLabel('default'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('notes the waiting mockup offer differently from an ordinary gate', () => {
    const gate = at({ open: { prompt_id: 'gate:1', kind: 'gate' } });
    open(gate);
    pick(modeLabel('auto'));
    const ordinary = screen.getByRole('note').textContent;
    cleanup();
    open(at({ open: { prompt_id: 'mockup:offer', kind: 'gate' } }));
    pick(modeLabel('auto'));
    expect(screen.getByRole('note').textContent).not.toBe(ordinary);
  });

  it('notes no handover for a waiting mockup once past Default', () => {
    open(at({ mode: 'auto', open: { prompt_id: 'mockup:1', kind: 'mockup' } }));
    pick(modeLabel('yolo'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('notes that a waiting sign-off is decided automatically only for the emitting modes', () => {
    open(atSignoff());
    pick(modeLabel('auto'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    pick(modeLabel('auto+emit'));
    expect(screen.getByRole('note')).toBeInTheDocument();
    pick(modeLabel('yolo'));
    expect(screen.getByRole('note')).toBeInTheDocument();
  });

  it('notes nothing for a sign-off already held under an emitting mode', () => {
    open({ ...atSignoff(), mode: 'auto+emit' });
    pick(modeLabel('yolo'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('notes that a waiting stage gate may be approved, only when leaving Default', () => {
    open();
    pick(modeLabel('auto'));
    expect(screen.getByRole('note')).toBeInTheDocument();
    pick(modeLabel('default'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('follows a republished mode while nothing is picked, and keeps a pick', () => {
    const { rerender } = open();
    rerender(<ModeCell chatId="S1" view={at({ mode: 'auto', seq: 43 })} />);
    expect(screen.getByRole('radio', { name: modeLabel('auto') })).toBeChecked();
    expect(confirm()).toBeDisabled();
    pick(modeLabel('yolo'));
    rerender(<ModeCell chatId="S1" view={at({ mode: 'auto+emit', seq: 44 })} />);
    expect(screen.getByRole('radio', { name: modeLabel('yolo') })).toBeChecked();
    expect(confirm()).toBeEnabled();
  });

  it('shows the plain Switch label when yolo is already current and nothing changed', () => {
    open(at({ mode: 'auto' }));
    pick(modeLabel('default'));
    const plain = confirm().textContent;
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    cleanup();
    open(at({ mode: 'yolo' }));
    expect(confirm()).toBeDisabled();
    expect(confirm().textContent).toBe(plain);
  });

  it('notes nothing when nothing waits on you', () => {
    open(at({ open: undefined, state: 'running' }));
    pick(modeLabel('yolo'));
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
  });

  it('handles a mode the dialog does not know', () => {
    open(at({ mode: 'turbo' }));
    expect(trigger()).toHaveAccessibleName(/^Mode: turbo,/);
    expect(screen.getAllByRole('radio').some((r) => (r as HTMLInputElement).checked)).toBe(false);
    expect(confirm()).toBeDisabled();
    pick(modeLabel('default'));
    expect(confirm()).toBeEnabled();
  });
});
