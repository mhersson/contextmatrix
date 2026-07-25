import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ChatEntry } from './ChatEntry';
import type { LogEntry } from '../../types';

// Counting stub: every render of a markdown body bumps the counter, letting
// the memo tests assert exactly how many rows re-rendered.
const markdownRenders = vi.fn();
vi.mock('./ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => {
    markdownRenders(source);
    return <div data-testid="markdown-stub">{source}</div>;
  },
}));

function makeEntry(seq: number, type: LogEntry['type'], content: string): LogEntry {
  return {
    ts: '2026-07-25T10:00:00Z',
    card_id: 'TEST-001',
    type,
    content,
    seq,
  };
}

function EntryList({ entries }: { entries: readonly LogEntry[] }) {
  return (
    <div>
      {entries.map((e) => (
        <ChatEntry key={e.seq} entry={e} />
      ))}
    </div>
  );
}

describe('ChatEntry memoization', () => {
  beforeEach(() => {
    markdownRenders.mockClear();
  });

  it('appending a new entry re-renders only the new row', () => {
    const stable = [
      makeEntry(1, 'text', 'one'),
      makeEntry(2, 'text', 'two'),
      makeEntry(3, 'text', 'three'),
    ];
    const { rerender } = render(<EntryList entries={stable} />);
    expect(markdownRenders).toHaveBeenCalledTimes(3);

    // New array identity, same first-3 entry references - exactly the shape
    // the ring buffer produces on append.
    rerender(<EntryList entries={[...stable, makeEntry(4, 'text', 'four')]} />);

    expect(markdownRenders).toHaveBeenCalledTimes(4);
    expect(markdownRenders).toHaveBeenLastCalledWith('four');
  });

  it('re-rendering with identical entries re-renders no rows', () => {
    const stable = [makeEntry(1, 'text', 'one'), makeEntry(2, 'system', 'sys')];
    const { rerender } = render(<EntryList entries={stable} />);
    markdownRenders.mockClear();

    rerender(<EntryList entries={stable} />);

    expect(markdownRenders).not.toHaveBeenCalled();
  });
});

describe('ChatEntry tool_result rendering', () => {
  it('renders tool_result through the plain-text branch, not markdown', () => {
    markdownRenders.mockClear();
    render(<ChatEntry entry={makeEntry(1, 'tool_result', '# not markdown\nraw output')} />);

    expect(markdownRenders).not.toHaveBeenCalled();
    expect(screen.queryByTestId('markdown-stub')).not.toBeInTheDocument();
    expect(screen.getByText(/raw output/)).toBeInTheDocument();
  });
});
