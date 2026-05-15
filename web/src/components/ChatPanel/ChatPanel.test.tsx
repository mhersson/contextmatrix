import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatPanel } from './ChatPanel';
import type { LogEntry } from '../../types';

const logs: LogEntry[] = [
  { ts: '2026-05-13T10:00:00Z', card_id: '', type: 'user', content: 'hello' },
  { ts: '2026-05-13T10:00:01Z', card_id: '', type: 'text', content: 'world' },
  { ts: '2026-05-13T10:00:02Z', card_id: '', type: 'tool_call', content: 'Read: foo.go' },
];

describe('ChatPanel', () => {
  it('renders user and assistant text by default; tool_call hidden', () => {
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
    expect(screen.getByText('hello')).toBeInTheDocument();
    expect(screen.getByText('world')).toBeInTheDocument();
    expect(screen.queryByText('Read: foo.go')).not.toBeInTheDocument();
  });

  it('shows tool_call when Tool calls filter is toggled on', () => {
    render(<ChatPanel logs={logs} onSend={() => {}} sendDisabled={false} />);
    fireEvent.click(screen.getByLabelText('Tool calls'));
    expect(screen.getByText('Read: foo.go')).toBeInTheDocument();
  });

  it('sends on Enter, newline on Shift+Enter', () => {
    const onSend = vi.fn();
    render(<ChatPanel logs={[]} onSend={onSend} sendDisabled={false} />);
    const ta = screen.getByPlaceholderText(/Type a message/);
    fireEvent.change(ta, { target: { value: 'hi' } });
    fireEvent.keyDown(ta, { key: 'Enter', shiftKey: false });
    expect(onSend).toHaveBeenCalledWith('hi');
  });

  it('disables compose when sendDisabled is true', () => {
    render(<ChatPanel logs={[]} onSend={() => {}} sendDisabled={true} />);
    expect(screen.getByPlaceholderText(/Type a message/)).toBeDisabled();
  });

  it('shows readOnlyMessage instead of compose when set', () => {
    render(<ChatPanel logs={[]} onSend={() => {}} sendDisabled={false} readOnlyMessage="Session ended" />);
    expect(screen.getByRole('status')).toHaveTextContent('Session ended');
    expect(screen.queryByPlaceholderText(/Type a message/)).not.toBeInTheDocument();
  });
});
