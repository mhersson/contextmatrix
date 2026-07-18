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

  it('shows a plain total when there is no subtask spend', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.0123 },
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
    expect(screen.getByText(/Total \$0\.0123/)).toBeInTheDocument();
    expect(screen.queryByText(/incl\. subtasks/)).not.toBeInTheDocument();
  });

  it('shows the run total incl. subtasks with the split line', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 4.42 },
      subtask_cost_usd: 0.57,
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'z-ai/some-model',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 4.42,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText(/Total \$4\.99 incl\. subtasks/)).toBeInTheDocument();
    expect(screen.getByText(/this card \$4\.42 \+ subtasks \$0\.57/)).toBeInTheDocument();
  });

  it('renders the total alone when spend is entirely in subtasks', () => {
    const card = makeCard({ subtask_cost_usd: 0.57 });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText(/Total \$0\.57 incl\. subtasks/)).toBeInTheDocument();
    expect(screen.queryByText(/this card/)).not.toBeInTheDocument();
  });
});
