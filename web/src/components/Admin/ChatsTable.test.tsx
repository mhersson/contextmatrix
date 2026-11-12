import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { SUBSCRIPTION_TOOLTIP } from '../../lib/subscription';
import type { ChatSession } from '../../types';
import { ChatsTable } from './ChatsTable';

function chat(o: Partial<ChatSession>): ChatSession {
  return {
    id: 'c1',
    title: 'Chat',
    status: 'cold',
    created_at: '2026-11-01T10:00:00Z',
    last_active: '2026-11-01T10:00:00Z',
    created_by: 'human:alice',
    ...o,
  };
}

function renderTable(chats: ChatSession[]) {
  render(<ChatsTable chats={chats} loading={false} error={null} onEnd={() => {}} onDelete={() => {}} />);
}

describe('ChatsTable cost column', () => {
  it('labels the subscription share as the Sessions page does', () => {
    renderTable([
      chat({ id: 'c1', title: 'Mixed', estimated_cost_usd: 1.5, subscription_cost_usd: 0.5, llm_channel: 'claude_code' }),
      chat({ id: 'c2', title: 'Sub', estimated_cost_usd: 0.4, subscription_cost_usd: 0.4, llm_channel: 'claude_code' }),
    ]);
    expect(screen.getByText('$1.50 (≈ $0.50 subscription)')).toHaveAttribute('title', SUBSCRIPTION_TOOLTIP);
    expect(screen.getByText('≈ $0.40')).toHaveAttribute('title', SUBSCRIPTION_TOOLTIP);
  });

  it('keeps a metered cost plain and a zero cost blank', () => {
    renderTable([
      chat({ id: 'c1', title: 'Metered', estimated_cost_usd: 1.5, llm_channel: 'endpoint' }),
      chat({ id: 'c2', title: 'Free' }),
    ]);
    expect(screen.getByText('$1.50')).not.toHaveAttribute('title');
    const free = screen.getByText('Free').closest('tr')!;
    const costHeader = screen.getAllByRole('columnheader').findIndex((h) => h.textContent === 'Cost');
    expect(within(free).getAllByRole('cell')[costHeader]).toHaveTextContent(/^-$/);
  });
});
