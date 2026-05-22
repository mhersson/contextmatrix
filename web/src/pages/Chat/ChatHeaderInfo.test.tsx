import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ChatHeaderInfo } from './ChatHeaderInfo';

// Stub out the models hook so tests do not need a real API.
vi.mock('../../utils/chatModels', () => ({
  useChatModels: () => [{ id: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6', max_tokens: 200000 }],
  modelMaxTokens: (_models: unknown[], _model: string | undefined) => 200000,
  contextPct: (tokens: number, max: number) => (max > 0 ? Math.round((tokens / max) * 100) : 0),
  formatTokens: (n: number) => n.toLocaleString(),
  usageColor: () => 'var(--green)',
}));

describe('ChatHeaderInfo', () => {
  it('hides cost span when estimatedCostUsd is 0', () => {
    render(
      <ChatHeaderInfo
        model="claude-sonnet-4-6"
        contextTokens={1000}
        estimatedCostUsd={0}
      />,
    );

    // The cost span should not be present.
    const spans = screen.getAllByRole('generic', { hidden: true });
    const costSpan = spans.find((el) => el.textContent?.startsWith('$'));
    expect(costSpan).toBeUndefined();
  });

  it('hides cost span when estimatedCostUsd is undefined', () => {
    render(
      <ChatHeaderInfo
        model="claude-sonnet-4-6"
        contextTokens={1000}
      />,
    );

    const spans = document.querySelectorAll('span');
    const costSpan = Array.from(spans).find((el) => el.textContent?.startsWith('$'));
    expect(costSpan).toBeUndefined();
  });

  it('renders cost span with $X.YY format when cost > 0', () => {
    render(
      <ChatHeaderInfo
        model="claude-sonnet-4-6"
        contextTokens={5000}
        estimatedCostUsd={0.1234}
        promptTokens={1000}
        completionTokens={500}
        cacheReadTokens={3000}
        cacheCreationTokens={200}
      />,
    );

    const spans = document.querySelectorAll('span');
    const costSpan = Array.from(spans).find((el) => el.textContent?.startsWith('$'));
    expect(costSpan).toBeDefined();
    // toFixed(2) → "$0.12"
    expect(costSpan?.textContent).toBe('$0.12');
  });

  it('tooltip on cost span contains all four token counters via toLocaleString', () => {
    render(
      <ChatHeaderInfo
        model="claude-sonnet-4-6"
        contextTokens={5000}
        estimatedCostUsd={0.05}
        promptTokens={1000}
        completionTokens={500}
        cacheReadTokens={3000}
        cacheCreationTokens={200}
      />,
    );

    const spans = document.querySelectorAll('span');
    const costSpan = Array.from(spans).find((el) => el.textContent?.startsWith('$'));
    expect(costSpan).toBeDefined();

    const title = costSpan?.getAttribute('title') ?? '';
    expect(title).toContain((1000).toLocaleString());
    expect(title).toContain((500).toLocaleString());
    expect(title).toContain((3000).toLocaleString());
    expect(title).toContain((200).toLocaleString());
  });
});
