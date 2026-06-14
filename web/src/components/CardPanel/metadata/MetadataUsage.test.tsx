import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { Card } from '../../../types';
import { MetadataUsage } from './MetadataUsage';

function makeCard(partial: Partial<Card>): Card {
  return {
    id: 'CMX-001',
    title: 'Demo',
    project: 'demo',
    type: 'task',
    state: 'review',
    priority: 'medium',
    created: '2026-06-14T00:00:00Z',
    updated: '2026-06-14T00:00:00Z',
    body: '',
    ...partial,
  } as Card;
}

describe('MetadataUsage', () => {
  it('renders nothing without a breakdown', () => {
    const { container } = render(<MetadataUsage card={makeCard({})} />);
    expect(container.firstChild).toBeNull();
  });

  it('lists each (agent, model) bucket with its cost', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'anthropic/claude-sonnet-4.6',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('anthropic/claude-sonnet-4.6')).toBeInTheDocument();
    expect(screen.getByText(/\$0\.0123/)).toBeInTheDocument();
  });
});
