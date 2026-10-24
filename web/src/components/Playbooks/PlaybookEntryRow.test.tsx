import { afterEach, describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybookEntryRowView, type PlaybookEntryRowViewProps } from './PlaybookEntryRow';
import type { PlaybookEntry } from '../../types';

const noop = { onToggleDone: vi.fn(), onSaveNote: vi.fn(), onSaveText: vi.fn(), onRemove: vi.fn() };

function cardEntry(over: Partial<PlaybookEntry> = {}): PlaybookEntry {
  return {
    id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-101',
    card_title: 'Do the thing', card_state: 'in_progress', complete: false, ...over,
  };
}

function renderRow(entry: PlaybookEntry, extra: Partial<PlaybookEntryRowViewProps> = {}) {
  render(<MemoryRouter><PlaybookEntryRowView entry={entry} index={0} isFrontier={false} {...noop} {...extra} /></MemoryRouter>);
}

describe('PlaybookEntryRowView', () => {
  it('renders a card entry with project badge, mono id, and state chip', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry()} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('ALPHA-101')).toBeInTheDocument();
    expect(screen.getByText('Do the thing')).toBeInTheDocument();
    expect(screen.getByText(/agent active/i)).toBeInTheDocument();
  });

  it('renders a missing card ref with a warning chip and keeps the entry', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry({ missing: true, card_title: undefined, card_state: undefined })} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.getByText(/missing/i)).toBeInTheDocument();
    expect(screen.getByText('ALPHA-101')).toBeInTheDocument();
  });

  it('toggles a manual entry through its checkbox', () => {
    const onToggleDone = vi.fn();
    render(<MemoryRouter><PlaybookEntryRowView
      entry={{ id: 'e2', type: 'manual', text: 'redeploy', complete: false }}
      index={1} isFrontier={false} {...noop} onToggleDone={onToggleDone} /></MemoryRouter>);
    fireEvent.click(screen.getByRole('checkbox', { name: /redeploy/i }));
    expect(onToggleDone).toHaveBeenCalledWith('e2', true);
  });

  it('shows the note in the display serif voice', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry({ note: 'brew coffee first' })} index={0} isFrontier={false} {...noop} /></MemoryRouter>);
    const note = screen.getByText(/brew coffee first/);
    expect(note).toHaveStyle({ fontStyle: 'italic' });
  });

  it('marks the frontier row', () => {
    render(<MemoryRouter><PlaybookEntryRowView entry={cardEntry()} index={0} isFrontier={true} {...noop} /></MemoryRouter>);
    expect(screen.getByText('next up')).toBeInTheDocument();
  });

  it('formats done_at as a relative time, not a raw ISO string', () => {
    const doneAt = '2020-01-01T00:00:00Z';
    render(<MemoryRouter><PlaybookEntryRowView
      entry={{ id: 'e3', type: 'manual', text: 'redeploy', complete: true, done: true, done_by: 'human:alice', done_at: doneAt }}
      index={1} isFrontier={false} {...noop} /></MemoryRouter>);
    expect(screen.queryByText(doneAt)).not.toBeInTheDocument();
    expect(screen.getByText(/\d+d ago/)).toBeInTheDocument();
  });

  it('shows a queued chip on incomplete cards while a run is active', () => {
    renderRow({ id: 'e2', type: 'card', project: 'alpha', card: 'ALPHA-2', card_title: 'Second', card_state: 'todo', complete: false },
      { run: { status: 'running', started_at: 'x', updated_at: 'x', entry: 'e1' } });
    expect(screen.getByText('queued')).toBeInTheDocument();
  });

  it('shows the waiting reason on the run entry', () => {
    renderRow({ id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-1', card_title: 'First', card_state: 'review', complete: false },
      { run: { status: 'waiting', started_at: 'x', updated_at: 'x', entry: 'e1', reason: 'ALPHA-1 parked: merge refused' } });
    expect(screen.getByText('ALPHA-1 parked: merge refused')).toBeInTheDocument();
    expect(screen.queryByText('queued')).not.toBeInTheDocument();
  });

  it('shows the reason on the frontier row, not queued, when a run is held with no entry', () => {
    renderRow({ id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-1', card_title: 'First', card_state: 'todo', complete: false },
      { isFrontier: true, run: { status: 'waiting', started_at: 'x', updated_at: 'x', entry: '', reason: 'ALPHA-1 not vetted' } });
    expect(screen.getByText('ALPHA-1 not vetted')).toBeInTheDocument();
    expect(screen.queryByText('queued')).not.toBeInTheDocument();
  });

  it('keeps later rows queued without the reason during a vetting hold', () => {
    renderRow({ id: 'e2', type: 'card', project: 'alpha', card: 'ALPHA-2', card_title: 'Second', card_state: 'todo', complete: false },
      { isFrontier: false, run: { status: 'waiting', started_at: 'x', updated_at: 'x', entry: '', reason: 'ALPHA-1 not vetted' } });
    expect(screen.getByText('queued')).toBeInTheDocument();
    expect(screen.queryByText('ALPHA-1 not vetted')).not.toBeInTheDocument();
  });

  it('shows nothing extra when no run is active', () => {
    renderRow({ id: 'e2', type: 'card', project: 'alpha', card: 'ALPHA-2', card_title: 'Second', card_state: 'todo', complete: false },
      { run: { status: 'stopped', started_at: 'x', updated_at: 'x' } });
    expect(screen.queryByText('queued')).not.toBeInTheDocument();
  });

  it('renders a long card title in full without truncating it', () => {
    const title = 'Rework the import pipeline so that every stage reports progress to the board and retries on transient failures';
    renderRow(cardEntry({ card_title: title }));
    expect(screen.getByText(title)).not.toHaveClass('truncate');
  });
});

describe('PlaybookEntryRowView manual step text', () => {
  const longStep = 'On a fresh cluster, ./setup.sh && task start completes with no undocumented steps; kubectl get ns shows every namespace';

  function manualEntry(): PlaybookEntry {
    return { id: 'e2', type: 'manual', text: longStep, complete: false };
  }

  function openEditor(onSaveText = vi.fn()) {
    renderRow(manualEntry(), { onSaveText });
    fireEvent.click(screen.getByRole('button', { name: longStep }));
    return { textbox: screen.getByRole('textbox') as HTMLTextAreaElement, onSaveText };
  }

  // jsdom has no layout; the stub shadows Element.prototype's getter, so deleting it restores that.
  let restoreScrollHeight: (() => void) | undefined;
  afterEach(() => {
    restoreScrollHeight?.();
    restoreScrollHeight = undefined;
  });

  it('renders the full step text without truncating it', () => {
    renderRow(manualEntry());
    expect(screen.getByRole('button', { name: longStep })).not.toHaveClass('truncate');
  });

  it('opens a textarea holding the full text when clicked', () => {
    const { textbox } = openEditor();
    expect(textbox.tagName).toBe('TEXTAREA');
    expect(textbox).toHaveValue(longStep);
    expect(textbox).toHaveFocus();
  });

  it('saves the trimmed text on Enter without inserting a newline', () => {
    const { textbox, onSaveText } = openEditor();
    fireEvent.change(textbox, { target: { value: '  redeploy the stack  ' } });
    expect(fireEvent.keyDown(textbox, { key: 'Enter' })).toBe(false);
    expect(onSaveText).toHaveBeenCalledWith('e2', 'redeploy the stack');
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('cancels on Escape and restores the original text', () => {
    const { textbox, onSaveText } = openEditor();
    fireEvent.change(textbox, { target: { value: 'something else' } });
    fireEvent.keyDown(textbox, { key: 'Escape' });
    expect(onSaveText).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: longStep }));
    expect(screen.getByRole('textbox')).toHaveValue(longStep);
  });

  it('saves the trimmed text on blur', () => {
    const { textbox, onSaveText } = openEditor();
    fireEvent.change(textbox, { target: { value: ' run the smoke test ' } });
    fireEvent.blur(textbox);
    expect(onSaveText).toHaveBeenCalledWith('e2', 'run the smoke test');
  });

  it('keeps a pasted multi-line step on one line', () => {
    const { textbox } = openEditor();
    fireEvent.change(textbox, { target: { value: 'build the image\r\npush it\nroll out' } });
    expect(textbox).toHaveValue('build the image push it roll out');
  });

  it('grows the textarea to fit its content as the text changes', () => {
    let contentHeight = 48;
    const proto = HTMLTextAreaElement.prototype as unknown as Record<string, unknown>;
    Object.defineProperty(proto, 'scrollHeight', { configurable: true, get: () => contentHeight });
    restoreScrollHeight = () => { delete proto.scrollHeight; };

    const { textbox } = openEditor();
    expect(textbox.style.height).toBe('48px');

    contentHeight = 96;
    fireEvent.change(textbox, { target: { value: `${longStep} and the ingress answers on every host` } });
    expect(textbox.style.height).toBe('96px');
  });
});
