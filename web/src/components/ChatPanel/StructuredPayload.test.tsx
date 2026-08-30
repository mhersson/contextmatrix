import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { StructuredPayload } from './StructuredPayload';
import { COLLAPSE_CHAR_THRESHOLD, JSON_PREVIEW_LINE_LIMIT } from './chatEntryUtils';

/** Pretty-printed JSON array guaranteed to exceed `minChars`. */
function prettyOverSize(minChars: number): string {
  const items: Array<{ index: number; note: string }> = [];
  let s: string;
  let i = 0;
  do {
    items.push({ index: i++, note: 'x'.repeat(40) });
    s = JSON.stringify(items, null, 2);
  } while (s.length <= minChars);
  return s;
}

describe('StructuredPayload', () => {
  it('renders short JSON in full with no toggle', () => {
    const pretty = JSON.stringify({ ok: true }, null, 2);
    render(<StructuredPayload pretty={pretty} />);

    expect(screen.getByTestId('structured-payload')).toHaveTextContent('"ok": true');
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('collapses long JSON to a few lines and expands on Read more', () => {
    const pretty = prettyOverSize(COLLAPSE_CHAR_THRESHOLD);
    render(<StructuredPayload pretty={pretty} />);

    const collapsed = screen.getByTestId('structured-payload').textContent ?? '';
    expect(collapsed.split('\n').length).toBeLessThanOrEqual(JSON_PREVIEW_LINE_LIMIT);
    expect(collapsed.length).toBeLessThan(pretty.length);

    const toggle = screen.getByRole('button', { name: /read more/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(toggle);
    expect(screen.getByTestId('structured-payload').textContent).toBe(pretty);
    expect(screen.getByRole('button', { name: /show less/i })).toHaveAttribute('aria-expanded', 'true');

    fireEvent.click(screen.getByRole('button', { name: /show less/i }));
    const recollapsed = screen.getByTestId('structured-payload').textContent ?? '';
    expect(recollapsed.length).toBeLessThan(pretty.length);
  });

  it('collapses line-dense JSON that stays under the char threshold', () => {
    // ~40 short array elements: far below COLLAPSE_CHAR_THRESHOLD in
    // characters, but a wall of lines - the shape a file-list verdict takes.
    const pretty = JSON.stringify(Array.from({ length: 40 }, (_, i) => String(i)), null, 2);
    expect(pretty.length).toBeLessThan(COLLAPSE_CHAR_THRESHOLD);
    render(<StructuredPayload pretty={pretty} />);

    const collapsed = screen.getByTestId('structured-payload').textContent ?? '';
    expect(collapsed.split('\n').length).toBeLessThanOrEqual(JSON_PREVIEW_LINE_LIMIT);
    expect(screen.getByRole('button', { name: /read more/i })).toBeInTheDocument();
  });
});
