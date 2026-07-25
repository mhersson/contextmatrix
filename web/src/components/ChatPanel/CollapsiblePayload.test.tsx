import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CollapsiblePayload } from './CollapsiblePayload';
import { COLLAPSE_CHAR_THRESHOLD } from './chatEntryUtils';

function renderPayload(content: string) {
  return render(
    <CollapsiblePayload content={content} accent="var(--aqua)" textColor="var(--aqua)" />,
  );
}

describe('CollapsiblePayload', () => {
  it('renders short content in full with no toggle', () => {
    renderPayload('short output');
    expect(screen.getByText('short output')).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('collapses content over the character threshold to a preview', () => {
    const content = 'x'.repeat(COLLAPSE_CHAR_THRESHOLD + 100);
    const { container } = renderPayload(content);
    expect(container.textContent).not.toContain(content);
    expect(screen.getByRole('button')).toHaveTextContent(/Show more/);
  });

  it('collapses content with many lines even when short', () => {
    const content = Array.from({ length: 10 }, (_, i) => `line-${i}`).join('\n');
    const { container } = renderPayload(content);
    // Preview cuts at 4 lines.
    expect(container.textContent).toContain('line-3');
    expect(container.textContent).not.toContain('line-5');
    expect(screen.getByRole('button')).toHaveTextContent(/Show more/);
  });

  it('shows the payload size in the toggle label', () => {
    const content = 'y'.repeat(2048);
    renderPayload(content);
    expect(screen.getByRole('button')).toHaveTextContent('Show more (2.0 KB)');
  });

  it('expand/collapse round-trip', () => {
    const content = Array.from({ length: 10 }, (_, i) => `row-${i}`).join('\n');
    const { container } = renderPayload(content);

    fireEvent.click(screen.getByRole('button'));
    expect(container.textContent).toContain('row-9');
    expect(screen.getByRole('button')).toHaveTextContent('Show less');

    fireEvent.click(screen.getByRole('button'));
    expect(container.textContent).not.toContain('row-9');
    expect(screen.getByRole('button')).toHaveTextContent(/Show more/);
  });
});
