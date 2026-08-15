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
    expect(screen.getByText('$0.0123')).toBeInTheDocument();
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
    expect(screen.getByText('Total')).toBeInTheDocument();
    // Total cell and the bucket's cost cell both carry the amount.
    expect(screen.getAllByText('$0.0123')).toHaveLength(2);
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
    expect(screen.getByText('Total incl. subtasks')).toBeInTheDocument();
    expect(screen.getByText('$4.99')).toBeInTheDocument();
    expect(screen.getByText(/this card \$4\.42 · subtasks \$0\.57/)).toBeInTheDocument();
  });

  it('renders the total alone when spend is entirely in subtasks', () => {
    const card = makeCard({ subtask_cost_usd: 0.57 });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total incl. subtasks')).toBeInTheDocument();
    expect(screen.getByText('$0.57')).toBeInTheDocument();
    expect(screen.queryByText(/this card/)).not.toBeInTheDocument();
  });

  it('prints the agent once above its buckets', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.01,
          cost_source: 'actual',
        },
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-2',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.02,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getAllByText('cmx-agent-cmx-001')).toHaveLength(1);
    expect(screen.getByText('openai/model-1')).toBeInTheDocument();
    expect(screen.getByText('openai/model-2')).toBeInTheDocument();
  });

  it('shows compact token counts with the exact count as tooltip', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 500000,
          completion_tokens: 83000,
          cost_usd: 0.27,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const tokens = screen.getByText('583k');
    expect(tokens).toHaveAttribute('title', `${(583000).toLocaleString()} tokens`);
  });

  it('marks estimated costs with an asterisk and tooltip', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'estimated',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByText('$0.0123*');
    expect(cost).toHaveAttribute('title', 'agent-reported · estimated from rate table');
  });

  it('leaves actual costs unmarked with the actual-cost tooltip', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByText('$0.0123');
    expect(cost).toHaveAttribute('title', 'agent-reported · actual provider cost');
  });

  it('labels collector-measured token counts as measured in the tooltip', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'actual',
          counts_source: 'collector',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByText('$0.0123');
    expect(cost).toHaveAttribute('title', 'measured (collector-reported) · actual provider cost');
  });

  it('labels self-reported token counts as agent-reported in the tooltip', () => {
    const card = makeCard({
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'actual',
          counts_source: 'self',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByText('$0.0123');
    expect(cost).toHaveAttribute('title', 'agent-reported · actual provider cost');
  });

  it('marks the Total line with an asterisk when any bucket is estimated', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.5 },
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'estimated',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const total = screen.getByText('$0.50*');
    expect(total).toHaveAttribute('title', 'includes costs estimated from the rate table');
  });

  it('leaves the Total line unmarked when all costs are actual with no subtask spend', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.5 },
      usage_breakdown: [
        {
          agent: 'cmx-agent-cmx-001',
          model: 'openai/model-1',
          prompt_tokens: 100,
          completion_tokens: 50,
          cost_usd: 0.0123,
          cost_source: 'actual',
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    const total = screen.getByText('$0.50');
    expect(total).not.toHaveAttribute('title');
  });

  it('leaves the run total unmarked when subtask_cost_has_estimates is absent and everything is actual', () => {
    // The server never serializes a false bool (json omitempty), so an
    // all-actual subtask rollup arrives over the wire with the field
    // entirely absent, not explicit false. The default must not treat
    // absence as "assume estimated".
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
    const total = screen.getByText('$4.99');
    expect(total).not.toHaveAttribute('title');
  });
});
