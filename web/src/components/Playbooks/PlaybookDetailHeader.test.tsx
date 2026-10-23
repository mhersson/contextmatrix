import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybookDetailHeader } from './PlaybookDetailHeader';
import type { PlaybookDetail } from '../../types';

// The description renders through the real lazy markdown chunk; paying the
// cold import up front keeps it from outlasting a waitFor under load.
beforeAll(() => import('../Markdown/SafeMarkdownPreview'));

const DESCRIPTION = [
  'Problem:',
  'Boards drift **out of sync** with `main`.',
  '',
  'Assumptions, each with its latest status:',
  '- A1 (confirmed by the user) the sync runs hourly',
  '- A2 (assumed by the responder) see [the spec](#spec)',
  '',
  'Emitted by contextmatrix-architect from blueprint bp-1.',
].join('\n');

function detail(description?: string): PlaybookDetail {
  return {
    id: 'roll', title: 'Roll', description,
    created_at: '2026-08-20T09:00:00Z', updated_at: '2026-08-20T09:00:00Z',
    complete: 0, total: 0, entries: [],
  };
}

function renderHeader(description: string | undefined, editingDescription = false) {
  const onStartEditDescription = vi.fn();
  const onSaveDescription = vi.fn();
  const header = (text: string | undefined) => (
    <MemoryRouter>
      <PlaybookDetailHeader
        detail={detail(text)}
        editingTitle={false}
        editingDescription={editingDescription}
        onStartEditTitle={() => {}}
        onStartEditDescription={onStartEditDescription}
        onSaveTitle={() => {}}
        onSaveDescription={onSaveDescription}
        onDeleteClick={() => {}}
      />
    </MemoryRouter>
  );
  const { rerender } = render(header(description));
  return { onStartEditDescription, onSaveDescription, rerender: (text: string) => rerender(header(text)) };
}

// Stands in for the browser's observer so a test can report a resize to
// whatever is being observed.
const observers: ResizeObserverCallback[] = [];
class ManualResizeObserver {
  private readonly callback: ResizeObserverCallback;
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
  }
  observe() {
    observers.push(this.callback);
  }
  disconnect() {}
}

// jsdom has no layout: every element reports a zero height unless stubbed.
// The stubs shadow Element.prototype's getters, so deleting them restores those.
function stubOverflowingLayout() {
  const proto = HTMLElement.prototype as unknown as Record<string, unknown>;
  Object.defineProperty(proto, 'scrollHeight', { configurable: true, get: () => 600 });
  Object.defineProperty(proto, 'clientHeight', { configurable: true, get: () => 100 });
  return () => {
    delete proto.scrollHeight;
    delete proto.clientHeight;
  };
}

describe('PlaybookDetailHeader description', () => {
  let restoreLayout: (() => void) | undefined;
  afterEach(() => {
    restoreLayout?.();
    restoreLayout = undefined;
    observers.length = 0;
    vi.unstubAllGlobals();
    window.getSelection()?.removeAllRanges();
  });

  it('renders the description as markdown', async () => {
    renderHeader(DESCRIPTION);
    const bold = await screen.findByText('out of sync');
    expect(bold.tagName).toBe('STRONG');
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
  });

  it('starts editing on a click in the description but not on a link in it', async () => {
    const { onStartEditDescription } = renderHeader(DESCRIPTION);
    fireEvent.click(await screen.findByRole('link', { name: 'the spec' }));
    expect(onStartEditDescription).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText(/Emitted by contextmatrix-architect/));
    expect(onStartEditDescription).toHaveBeenCalledOnce();
  });

  it('leaves the copy control and a summary in the description to themselves', async () => {
    const { onStartEditDescription } = renderHeader(
      'Run it:\n\n```\nmake test\n```\n\n<details><summary>Why</summary>Because.</details>',
    );
    await waitFor(() => expect(document.querySelector('.copied')).not.toBeNull());
    fireEvent.click(document.querySelector('.copied')!);
    fireEvent.click(screen.getByText('Why'));
    expect(onStartEditDescription).not.toHaveBeenCalled();
  });

  it('does not start editing when the click ends a text selection', async () => {
    const { onStartEditDescription } = renderHeader(DESCRIPTION);
    const text = await screen.findByText(/Emitted by contextmatrix-architect/);
    const range = document.createRange();
    range.selectNodeContents(text);
    window.getSelection()!.addRange(range);
    fireEvent.click(text);
    expect(onStartEditDescription).not.toHaveBeenCalled();
  });

  it('collapses a long description behind a Show more toggle that does not start editing', async () => {
    restoreLayout = stubOverflowingLayout();
    const { onStartEditDescription } = renderHeader(DESCRIPTION);

    const box = screen.getByTestId('playbook-description');
    const more = await screen.findByRole('button', { name: 'Show more' });
    expect(more).toHaveAttribute('aria-expanded', 'false');
    expect(box).toHaveStyle({ overflow: 'hidden' });
    fireEvent.click(more);

    const less = screen.getByRole('button', { name: 'Show less' });
    expect(less).toHaveAttribute('aria-expanded', 'true');
    expect(box).not.toHaveStyle({ overflow: 'hidden' });
    fireEvent.click(less);
    expect(screen.getByRole('button', { name: 'Show more' })).toHaveAttribute('aria-expanded', 'false');
    expect(box).toHaveStyle({ overflow: 'hidden' });
    expect(onStartEditDescription).not.toHaveBeenCalled();
  });

  it('collapses again when the description changes while expanded', async () => {
    restoreLayout = stubOverflowingLayout();
    const { rerender } = renderHeader(DESCRIPTION);
    fireEvent.click(await screen.findByRole('button', { name: 'Show more' }));

    rerender(`${DESCRIPTION}\n\nRevised.`);
    expect(screen.getByRole('button', { name: 'Show more' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('offers the toggle once a resize makes the description overflow', async () => {
    vi.stubGlobal('ResizeObserver', ManualResizeObserver);
    renderHeader(DESCRIPTION);
    await screen.findByText('out of sync');
    expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();

    restoreLayout = stubOverflowingLayout();
    act(() => observers.forEach((notify) => notify([], {} as ResizeObserver)));
    expect(screen.getByRole('button', { name: 'Show more' })).toBeInTheDocument();
  });

  it('prompts for a description when it is only whitespace', () => {
    const { onStartEditDescription } = renderHeader(' \n ');
    fireEvent.click(screen.getByText('Add a description'));
    expect(onStartEditDescription).toHaveBeenCalledOnce();
  });

  it('shows no toggle for a description that fits', async () => {
    renderHeader('One line.');
    await screen.findByText('One line.');
    await waitFor(() => expect(document.querySelector('.wmde-markdown')).not.toBeNull());
    expect(screen.queryByRole('button', { name: /show (more|less)/i })).toBeNull();
  });

  it('edits the raw markdown with its line breaks intact', () => {
    const { onSaveDescription } = renderHeader(DESCRIPTION, true);
    const box = screen.getByRole('textbox');
    expect(box).toHaveValue(DESCRIPTION);
    fireEvent.blur(box);
    expect(onSaveDescription).toHaveBeenCalledWith(DESCRIPTION);
  });
});
