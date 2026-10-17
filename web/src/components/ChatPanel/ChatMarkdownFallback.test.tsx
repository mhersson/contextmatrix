import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import type { MockInstance } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ChatPanel } from './ChatPanel';
import type { LogEntry } from '../../types';

// Separate file from ChatPanel.test.tsx on purpose: this mock makes every
// markdown render throw (as a rejected md-editor chunk import would), which
// must not leak into the suites that exercise the real lazy/Suspense path.
vi.mock('@uiw/react-markdown-preview', () => ({
  default: () => {
    throw new Error('chunk load failed');
  },
}));

// Load the chunk under the hook timeout rather than inside waitFor's.
beforeAll(() => import('../Markdown/SafeMarkdownPreview'));

describe('ChatMarkdown failure containment', () => {
  let consoleError: MockInstance<typeof console.error>;

  beforeEach(() => {
    consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('degrades a text message to plain source instead of killing the panel', async () => {
    const logs: LogEntry[] = [
      { ts: '2026-07-25T10:00:00Z', card_id: 'C-1', type: 'text', content: '# heading text' },
    ];
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);

    // The Suspense fallback shows the same raw source, so wait until the
    // throwing renderer's error is logged: React logs a caught error only
    // after the boundary's fallback commits.
    await waitFor(() =>
      expect(consoleError.mock.calls.flat()).toContainEqual(
        expect.objectContaining({ message: 'chunk load failed' }),
      ),
    );
    expect(screen.getByText('# heading text')).toBeInTheDocument();
    expect(screen.getByLabelText('Tool calls')).toBeInTheDocument();
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument();
  });
});
