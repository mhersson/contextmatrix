import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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

describe('ChatMarkdown failure containment', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('degrades a text message to plain source instead of killing the panel', async () => {
    const logs: LogEntry[] = [
      { ts: '2026-07-25T10:00:00Z', card_id: 'C-1', type: 'text', content: '# heading text' },
    ];
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);

    // The raw source stays visible (Suspense fallback first, error fallback
    // once the throwing component mounts) and the panel chrome survives.
    expect(await screen.findByText('# heading text')).toBeInTheDocument();
    expect(screen.getByLabelText('Tool calls')).toBeInTheDocument();
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument();
  });
});
