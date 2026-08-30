import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ClampedMarkdown } from './ClampedMarkdown';

vi.mock('./ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="markdown-stub">{source}</div>,
}));

describe('ClampedMarkdown', () => {
  it('clamps the markdown initially and expands on Read more', () => {
    render(<ClampedMarkdown source={'# briefing\n\nlots of text'} />);

    expect(screen.getByTestId('clamped-markdown')).toHaveStyle({ overflow: 'hidden' });
    expect(screen.getByTestId('markdown-stub')).toHaveTextContent('briefing');

    const toggle = screen.getByRole('button', { name: /read more/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(toggle);
    expect(screen.getByTestId('clamped-markdown')).not.toHaveStyle({ overflow: 'hidden' });
    expect(screen.getByRole('button', { name: /show less/i })).toHaveAttribute('aria-expanded', 'true');

    fireEvent.click(screen.getByRole('button', { name: /show less/i }));
    expect(screen.getByTestId('clamped-markdown')).toHaveStyle({ overflow: 'hidden' });
  });
});
