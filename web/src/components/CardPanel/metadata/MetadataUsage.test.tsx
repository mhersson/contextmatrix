import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { SUBSCRIPTION_TOOLTIP } from '../../../lib/subscription';
import type { Card, UsageBucket } from '../../../types';
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

function bucket(partial: Partial<UsageBucket>): UsageBucket {
  return {
    agent: 'cmx-agent-cmx-001',
    model: 'openai/model-1',
    prompt_tokens: 100,
    completion_tokens: 50,
    cost_usd: 0.01,
    cost_source: 'actual',
    ...partial,
  };
}

describe('MetadataUsage', () => {
  it('renders nothing without a breakdown', () => {
    const { container } = render(<MetadataUsage card={makeCard({})} />);
    expect(container.firstChild).toBeNull();
  });

  it('lists each bucket with its full model name as the tooltip and its cost', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ model: 'anthropic/claude-sonnet-4.6', cost_usd: 0.0123 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByTitle('anthropic/claude-sonnet-4.6')).toHaveTextContent(
      'anthropic/claude-sonnet-4.6',
    );
    expect(screen.getByText('anthropic/')).toHaveClass('text-[var(--grey0)]');
    // Total row and the bucket row both carry the amount.
    expect(screen.getAllByText('$0.0123')).toHaveLength(2);
  });

  it('shows a plain total and no split when there is no subtask spend', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.0123 },
      usage_breakdown: [bucket({ role: 'execute', cost_usd: 0.0123 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total')).toBeInTheDocument();
    // Total cell and the bucket's cost cell both carry the amount.
    expect(screen.getAllByText('$0.0123')).toHaveLength(2);
    expect(screen.queryByText(/incl\. subtasks/)).not.toBeInTheDocument();
    expect(screen.queryByRole('img', { name: 'cost split by role' })).not.toBeInTheDocument();
  });

  it('shows the run total incl. subtasks with a bar and legend instead of a split line', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 4.42 },
      subtask_cost_usd: 0.57,
      usage_breakdown: [bucket({ model: 'z-ai/some-model', role: 'execute', cost_usd: 4.42 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total incl. subtasks')).toBeInTheDocument();
    expect(screen.getByText('$4.99')).toBeInTheDocument();
    expect(screen.queryByText(/this card \$4\.42 · subtasks/)).not.toBeInTheDocument();

    const bar = screen.getByRole('img', { name: 'cost split by role' });
    expect(bar.children).toHaveLength(2);
    expect(bar.children[0]).toHaveAttribute('title', 'execute $4.42 (89%)');
    expect(bar.children[1]).toHaveAttribute('title', 'subtasks $0.57 (11%)');
    expect(bar.children[1]).toHaveClass('bf-usage-seg--hatch');

    expect(screen.getByText('subtasks')).toBeInTheDocument();
    expect(screen.getByText('$0.57')).toBeInTheDocument();
  });

  it('names this card with its own total above the role groups when subtasks cost something', () => {
    const card = makeCard({
      id: 'CMX-779',
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 4.42 },
      subtask_cost_usd: 0.57,
      usage_breakdown: [bucket({ role: 'execute', cost_usd: 4.42 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('CMX-779')).toBeInTheDocument();
    expect(screen.getByText('this card')).toBeInTheDocument();
  });

  it('totals the buckets when the card carries no cumulative token_usage', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ cost_usd: 0.3 }), bucket({ model: 'openai/model-2', cost_usd: 0.2 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total')).toBeInTheDocument();
    // Total row and the two-row "other" group's subtotal.
    expect(screen.getAllByText('$0.50')).toHaveLength(2);
  });

  it('marks estimated shares in the legend and on the bar', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 1.5 },
      subtask_cost_usd: 0.5,
      subtask_cost_has_estimates: true,
      usage_breakdown: [
        bucket({ role: 'execute', cost_usd: 1 }),
        bucket({ model: 'openai/model-2', role: 'plan', cost_usd: 0.5, cost_source: 'estimated' }),
      ],
    });
    render(<MetadataUsage card={card} />);
    const bar = screen.getByRole('img', { name: 'cost split by role' });
    expect(bar.children[0]).toHaveAttribute('title', 'execute $1.00 (50%)');
    expect(bar.children[1]).toHaveAttribute('title', 'plan $0.50* (25%)');
    expect(bar.children[2]).toHaveAttribute('title', 'subtasks $0.50* (25%)');
    // Legend amounts for the estimated shares carry the marker; the plan row does too.
    expect(screen.getAllByText('$0.50*')).toHaveLength(3);
  });

  it('shows a sub-percent share as <1%', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 100 },
      usage_breakdown: [
        bucket({ role: 'review', cost_usd: 99.9 }),
        bucket({ model: 'openai/model-2', role: 'gates', cost_usd: 0.1 }),
      ],
    });
    render(<MetadataUsage card={card} />);
    const bar = screen.getByRole('img', { name: 'cost split by role' });
    expect(bar.children[1]).toHaveAttribute('title', 'gates $0.10 (<1%)');
  });

  it('renders the total alone when spend is entirely in subtasks', () => {
    const card = makeCard({ subtask_cost_usd: 0.57 });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total incl. subtasks')).toBeInTheDocument();
    expect(screen.getByText('$0.57')).toBeInTheDocument();
    expect(screen.queryByText(/this card/)).not.toBeInTheDocument();
  });

  it('never shows the agent id', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ model: 'openai/model-1' }), bucket({ model: 'openai/model-2' })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.queryByText('cmx-agent-cmx-001')).not.toBeInTheDocument();
  });

  it('groups buckets under role headers, largest role first, with subtotal and step words', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 300, completion_tokens: 150, estimated_cost_usd: 13.98 },
      usage_breakdown: [
        bucket({ model: 'openai/gpt-5.6-sol', role: 'plan', cost_usd: 0.62 }),
        bucket({ model: 'anthropic/claude-fable-5.1', role: 'review', steps: ['mob_seat'], cost_usd: 7.82 }),
        bucket({ model: 'anthropic/claude-opus-5', role: 'review', steps: ['mob_moderator'], cost_usd: 5.54 }),
      ],
    });
    render(<MetadataUsage card={card} />);

    const headers = screen.getAllByText(/^(review|plan)$/).map((el) => el.textContent);
    // Legend first (largest first), then the role headers in the same order.
    expect(headers).toEqual(['review', 'plan', 'review', 'plan']);
    expect(screen.getAllByText('$13.36')).toHaveLength(2);
    expect(screen.getByText('seat, moderator')).toBeInTheDocument();

    const bar = screen.getByRole('img', { name: 'cost split by role' });
    expect(bar.children[0]).toHaveAttribute('title', 'review $13.36 (96%)');
    expect(bar.children[1]).toHaveAttribute('title', 'plan $0.62 (4%)');
  });

  it('lists a model that served two roles once per role', () => {
    const card = makeCard({
      usage_breakdown: [
        bucket({ model: 'openai/gpt-5.6-sol', role: 'plan', cost_usd: 0.62 }),
        bucket({ model: 'openai/gpt-5.6-sol', role: 'review', steps: ['mob_moderator'], cost_usd: 0.93 }),
      ],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getAllByTitle('openai/gpt-5.6-sol')).toHaveLength(2);
  });

  it('files buckets written before roles existed under "other" with an explanation', () => {
    const card = makeCard({ usage_breakdown: [bucket({ cost_usd: 0.5 })] });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('other')).toHaveAttribute(
      'title',
      'spend with no role: reported before roles existed or without a phase',
    );
  });

  it('drops a step word that repeats the role', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ role: 'judge', steps: ['judge'], cost_usd: 0.5 })],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getAllByText('judge')).toHaveLength(1);
  });

  it('renders one block per subtask with its id, total and model rows, and opens the card on click', () => {
    const onSubtaskClick = vi.fn();
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 1 },
      subtask_cost_usd: 0.35,
      usage_breakdown: [bucket({ role: 'plan', cost_usd: 1 })],
      subtask_usage: [
        {
          card_id: 'CMX-002',
          buckets: [bucket({ model: 'deepseek/deepseek-v4-flash', role: 'execute', cost_usd: 0.3 })],
        },
        {
          card_id: 'CMX-003',
          buckets: [bucket({ model: 'deepseek/deepseek-v4-flash', cost_usd: 0.05, cost_source: 'estimated' })],
        },
      ],
    });
    render(<MetadataUsage card={card} onSubtaskClick={onSubtaskClick} />);

    expect(screen.getAllByText('subtask')).toHaveLength(2);
    expect(screen.getByText('$0.30')).toBeInTheDocument();
    expect(screen.getByText('$0.0500*')).toBeInTheDocument();
    expect(screen.getAllByTitle('deepseek/deepseek-v4-flash')).toHaveLength(2);

    fireEvent.click(screen.getByRole('button', { name: 'CMX-003' }));
    expect(onSubtaskClick).toHaveBeenCalledWith('CMX-003');
  });

  it('shows a subtask total only when the subtask has more than one model row', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 1 },
      subtask_cost_usd: 0.45,
      usage_breakdown: [bucket({ role: 'plan', cost_usd: 1 })],
      subtask_usage: [
        {
          card_id: 'CMX-002',
          buckets: [
            bucket({ model: 'deepseek/deepseek-v4-flash', cost_usd: 0.3 }),
            bucket({ model: 'anthropic/claude-sonnet-5', cost_usd: 0.15 }),
          ],
        },
      ],
    });
    render(<MetadataUsage card={card} />);
    // Header total, legend amount, and the two rows.
    expect(screen.getAllByText('$0.45')).toHaveLength(2);
    expect(screen.getByText('$0.30')).toBeInTheDocument();
    expect(screen.getByText('$0.15')).toBeInTheDocument();
  });

  it('still lists subtask blocks when the summed subtask cost is absent', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ role: 'plan', cost_usd: 1 })],
      subtask_usage: [{ card_id: 'CMX-002', buckets: [bucket({ cost_usd: 0.3 })] }],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.getByText('Total')).toBeInTheDocument();
    expect(screen.getByText('CMX-002')).toBeInTheDocument();
    expect(screen.getByText('subtask')).toBeInTheDocument();
  });

  it('renders subtask ids as plain text without a click handler', () => {
    const card = makeCard({
      subtask_cost_usd: 0.3,
      subtask_usage: [{ card_id: 'CMX-002', buckets: [bucket({ cost_usd: 0.3 })] }],
    });
    render(<MetadataUsage card={card} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.getByText('CMX-002')).toBeInTheDocument();
  });

  it('shows compact token counts with the exact count as tooltip', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ prompt_tokens: 500000, completion_tokens: 83000, cost_usd: 0.27 })],
    });
    render(<MetadataUsage card={card} />);
    const tokens = screen.getByText('583k');
    expect(tokens).toHaveAttribute('title', `${(583000).toLocaleString()} tokens`);
  });

  it('marks estimated costs with an asterisk and tooltip', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ cost_usd: 0.0123, cost_source: 'estimated' })],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByTitle('agent-reported · estimated from rate table');
    expect(cost).toHaveTextContent('$0.0123*');
  });

  it('leaves actual costs unmarked with the actual-cost tooltip', () => {
    const card = makeCard({ usage_breakdown: [bucket({ cost_usd: 0.0123 })] });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByTitle('agent-reported · actual provider cost');
    expect(cost).toHaveTextContent('$0.0123');
    expect(cost).not.toHaveTextContent('*');
  });

  it('labels collector-measured token counts as measured in the tooltip', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ cost_usd: 0.0123, counts_source: 'collector' })],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByTitle('measured (collector-reported) · actual provider cost');
    expect(cost).toHaveTextContent('$0.0123');
  });

  it('labels self-reported token counts as agent-reported in the tooltip', () => {
    const card = makeCard({
      usage_breakdown: [bucket({ cost_usd: 0.0123, counts_source: 'self' })],
    });
    render(<MetadataUsage card={card} />);
    const cost = screen.getByTitle('agent-reported · actual provider cost');
    expect(cost).toHaveTextContent('$0.0123');
  });

  it('marks the Total line with an asterisk when any bucket is estimated', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.5 },
      usage_breakdown: [bucket({ cost_usd: 0.0123, cost_source: 'estimated' })],
    });
    render(<MetadataUsage card={card} />);
    const total = screen.getByText('$0.50*');
    expect(total).toHaveAttribute('title', 'includes costs estimated from the rate table');
  });

  it('leaves the Total line unmarked when all costs are actual with no subtask spend', () => {
    const card = makeCard({
      token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.5 },
      usage_breakdown: [bucket({ cost_usd: 0.0123 })],
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
      usage_breakdown: [bucket({ model: 'z-ai/some-model', cost_usd: 4.42 })],
    });
    render(<MetadataUsage card={card} />);
    const total = screen.getByText('$4.99');
    expect(total).not.toHaveAttribute('title');
  });

  describe('subscription usage', () => {
    const sub = (partial: Partial<UsageBucket> = {}) =>
      bucket({ model: 'claude-sonnet-5-5', cost_usd: 0.5, cost_source: 'estimated', cost_basis: 'subscription', ...partial });

    it('marks a subscription bucket with a trailing ≈ and the subscription tooltip, not the rate-table asterisk', () => {
      render(<MetadataUsage card={makeCard({ usage_breakdown: [sub()] })} />);
      const cell = screen.getByText('$0.50≈');
      expect(cell).toHaveAttribute('title', SUBSCRIPTION_TOOLTIP);
      expect(cell).not.toHaveTextContent('*');
    });

    it('labels an all-subscription total as API-equivalent', () => {
      const card = makeCard({
        token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 0.5 },
        usage_breakdown: [sub()],
      });
      render(<MetadataUsage card={card} />);
      expect(screen.getByText('≈ $0.50 API-equivalent')).toHaveAttribute('title', SUBSCRIPTION_TOOLTIP);
      expect(screen.queryByText('Total')).toBeInTheDocument();
    });

    it('splits a mixed total into the subscription estimate', () => {
      const card = makeCard({
        token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 2.5 },
        usage_breakdown: [bucket({ role: 'execute', cost_usd: 2 }), sub({ role: 'review' })],
      });
      render(<MetadataUsage card={card} />);
      expect(screen.getByText('$2.50 total · $0.50 of it subscription estimate')).toHaveAttribute(
        'title',
        SUBSCRIPTION_TOOLTIP,
      );
      // The metered bucket keeps its plain amount.
      expect(screen.getByTitle('agent-reported · actual provider cost')).toHaveTextContent(/^\$2\.00$/);
    });

    it('keeps the rate-table asterisk on a mixed total only for metered estimates', () => {
      const card = makeCard({
        token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 2.5 },
        usage_breakdown: [bucket({ role: 'execute', cost_usd: 2, cost_source: 'estimated' }), sub({ role: 'review' })],
      });
      render(<MetadataUsage card={card} />);
      expect(screen.getByText('$2.50* total · $0.50 of it subscription estimate')).toBeInTheDocument();
    });

    it('gives every ≈ the subscription tooltip: legend, bar, role subtotal, this card and subtask subtotals', () => {
      const card = makeCard({
        id: 'CMX-001',
        token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 1.5 },
        subtask_cost_usd: 1,
        usage_breakdown: [bucket({ role: 'execute', cost_usd: 0.5 }), sub({ role: 'execute', model: 'claude-opus-5-5', cost_usd: 1 })],
        subtask_usage: [{ card_id: 'CMX-002', buckets: [sub({ cost_usd: 0.4 }), sub({ model: 'claude-haiku-5-5', cost_usd: 0.6 })] }],
      });
      render(<MetadataUsage card={card} />);
      const marked = screen.getAllByText(/≈$/);
      // Legend (execute, subtasks), role subtotal, this card, subtask subtotal and the three subscription rows.
      expect(marked).toHaveLength(8);
      for (const el of marked) expect(el).toHaveAttribute('title', SUBSCRIPTION_TOOLTIP);
      const bar = screen.getByRole('img', { name: 'cost split by role' });
      for (const seg of Array.from(bar.children)) expect(seg.getAttribute('title')).toContain(SUBSCRIPTION_TOOLTIP);
    });

    it('counts subtask subscription spend in the run total', () => {
      const card = makeCard({
        token_usage: { prompt_tokens: 100, completion_tokens: 50, estimated_cost_usd: 1 },
        subtask_cost_usd: 0.5,
        usage_breakdown: [bucket({ role: 'execute', cost_usd: 1 })],
        subtask_usage: [{ card_id: 'CMX-002', buckets: [sub()] }],
      });
      render(<MetadataUsage card={card} />);
      expect(screen.getByText('$1.50 total · $0.50 of it subscription estimate')).toBeInTheDocument();
      expect(screen.getByText('Total incl. subtasks')).toBeInTheDocument();
    });
  });
});
