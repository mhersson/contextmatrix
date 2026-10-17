import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BodyMarkdown } from './BodyMarkdown';

vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

describe('BodyMarkdown', () => {
  it('clouds only the changed paragraph of the newest version', () => {
    render(<BodyMarkdown body={'Same.\n\nNew wording.'} previous={'Same.\n\nOld wording.'} mode="clouds" version={2} />);
    const marks = screen.getAllByText('Changed in rev 2.');
    expect(marks).toHaveLength(1);
    const cloud = marks[0].closest('.bp-cloud')!;
    expect(cloud).toHaveTextContent('New wording.');
    expect(cloud).not.toHaveTextContent('Same.');
  });

  it('draws no clouds in plain mode', () => {
    render(<BodyMarkdown body={'A.\n\nB.'} previous={'A.'} mode="plain" version={2} />);
    expect(screen.queryByText(/Changed in rev/)).not.toBeInTheDocument();
    expect(screen.getAllByTestId('md')).toHaveLength(2);
  });

  it('strikes removed words and marks added ones in compare mode', () => {
    const { container } = render(
      <BodyMarkdown body={'Same.\n\nCall SetIndent.'} previous={'Same.\n\nCall MarshalIndent.'} mode="compare" version={2} />,
    );
    expect(container.querySelector('del')).toHaveTextContent('MarshalIndent');
    expect(container.querySelector('ins')).toHaveTextContent('SetIndent');
    expect(screen.getByTestId('md')).toHaveTextContent('Same.');
  });
});
