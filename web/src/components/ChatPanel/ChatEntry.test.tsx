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

// jsdom has no layout engine, so the clipping these guard against cannot be
// observed directly - only the two properties that prevent it. `wrap-anywhere`
// is `overflow-wrap: anywhere`, which unlike `break-word` counts toward
// min-content sizing; `max-w-full` clamps the shrink-to-fit bubble to its 85%
// parent. Without both, a long unbreakable token sets a min-content width
// wider than the cap, the bubble grows to match, and the transcript's
// overflow-x-hidden slices the text.
describe('ChatEntry long-token wrapping', () => {
  const longToken =
    'FlowMetricsServiceTest.explicitLabelsRemainAnIndependentConjunctiveConstraintWithPiScope';

  it('keeps an agent bubble inside its column when the text has no break opportunity', () => {
    render(<ChatEntry entry={makeEntry(1, 'text', longToken)} />);

    const bubble = screen.getByTestId('markdown-stub').parentElement;
    expect(bubble).toHaveClass('wrap-anywhere');
    expect(bubble).toHaveClass('max-w-full');
  });

  it('keeps a user bubble inside its column when the text has no break opportunity', () => {
    render(<ChatEntry entry={makeEntry(1, 'user', longToken)} />);

    const bubble = screen.getByText(longToken);
    expect(bubble).toHaveClass('wrap-anywhere');
    expect(bubble).toHaveClass('max-w-full');
  });

  // `wrap-anywhere` cannot rescue content that refuses to wrap at all - a
  // fenced code block is `white-space: pre`, and a percentage max-width does
  // not clamp min-content contribution. The wrapper is a flex item, so its
  // automatic minimum size would size it to that unwrappable content and beat
  // its own max-width; `min-w-0` is what lets the 85% cap win. The code block
  // then scrolls inside itself, which is what `pre > code` already does.
  it('clamps both bubble wrappers so unshrinkable content cannot widen the column', () => {
    const { rerender } = render(<ChatEntry entry={makeEntry(1, 'text', '```\nlong\n```')} />);
    expect(screen.getByTestId('markdown-stub').parentElement?.parentElement).toHaveClass('min-w-0');

    rerender(<ChatEntry entry={makeEntry(2, 'user', longToken)} />);
    expect(screen.getByText(longToken).parentElement).toHaveClass('min-w-0');
  });
});

describe('ChatEntry structured and long text rendering', () => {
  const longJson = JSON.stringify({
    subtasks: Array.from({ length: 20 }, (_, i) => ({
      title: `subtask ${i}`,
      description: 'd'.repeat(60),
    })),
  });

  it('renders a raw-JSON text entry as a structured code block, not markdown', () => {
    markdownRenders.mockClear();
    const e = { ...makeEntry(1, 'text', longJson), agent: 'moderator', model: 'claude-sonnet-5' };
    render(<ChatEntry entry={e} />);

    expect(screen.getByTestId('structured-payload')).toBeInTheDocument();
    expect(markdownRenders).not.toHaveBeenCalled();
    expect(screen.getByTestId('speaker-chip')).toHaveTextContent('moderator');
  });

  it('does not treat a markdown link line as JSON', () => {
    markdownRenders.mockClear();
    render(<ChatEntry entry={makeEntry(1, 'text', '[docs](https://example.com)')} />);

    expect(screen.queryByTestId('structured-payload')).not.toBeInTheDocument();
    expect(markdownRenders).toHaveBeenCalled();
  });

  it('clamps a very long markdown text entry behind Read more', () => {
    render(<ChatEntry entry={makeEntry(2, 'text', 'line of briefing text\n'.repeat(300))} />);

    expect(screen.getByTestId('clamped-markdown')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /read more/i })).toBeInTheDocument();
  });

  it('clamps a line-dense text entry that stays under the char threshold', () => {
    // 60 short bullet lines: ~420 chars but tall enough to swamp the chat.
    render(<ChatEntry entry={makeEntry(2, 'text', '- item\n'.repeat(60))} />);

    expect(screen.getByTestId('clamped-markdown')).toBeInTheDocument();
  });

  it('renders a short text entry without any clamp or toggle', () => {
    render(<ChatEntry entry={makeEntry(3, 'text', 'hello')} />);

    expect(screen.queryByTestId('clamped-markdown')).not.toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
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
