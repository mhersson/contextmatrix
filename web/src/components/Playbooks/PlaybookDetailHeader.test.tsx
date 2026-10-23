import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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
  '- A2 (assumed by the responder) see [the spec](https://example.com/spec)',
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
  render(
    <MemoryRouter>
      <PlaybookDetailHeader
        detail={detail(description)}
        editingTitle={false}
        editingDescription={editingDescription}
        onStartEditTitle={() => {}}
        onStartEditDescription={onStartEditDescription}
        onSaveTitle={() => {}}
        onSaveDescription={onSaveDescription}
        onDeleteClick={() => {}}
      />
    </MemoryRouter>,
  );
  return { onStartEditDescription, onSaveDescription };
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
