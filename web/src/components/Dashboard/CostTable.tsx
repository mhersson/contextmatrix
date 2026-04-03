import { useState } from 'react';
import type { AgentCost, CardCost } from '../../types';

interface CostTableProps {
  agentCosts: AgentCost[];
  cardCosts: CardCost[];
}

function formatTokens(n: number): string {
  if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

const PAGE_SIZE = 25;

/** Filter cards by card ID (case-insensitive substring match). */
export function filterCardCosts(cards: CardCost[], search: string): CardCost[] {
  const q = search.trim().toLowerCase();
  if (!q) return cards;
  return cards.filter((c) => c.card_id.toLowerCase().includes(q));
}

/** Slice an array for a given 1-based page and page size. */
export function paginateItems<T>(
  items: T[],
  page: number,
  pageSize: number,
): { items: T[]; totalPages: number } {
  if (items.length === 0) return { items: [], totalPages: 0 };
  const totalPages = Math.ceil(items.length / pageSize);
  const start = (page - 1) * pageSize;
  const end = start + pageSize;
  return { items: items.slice(start, end), totalPages };
}

export function CostTable({ agentCosts, cardCosts }: CostTableProps) {
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);

  const sortedAgents = [...agentCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd);
  const sortedCards = [...cardCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd);

  const filteredCards = filterCardCosts(sortedCards, search);
  const { items: visibleCards, totalPages } = paginateItems(filteredCards, page, PAGE_SIZE);

  function handleSearchChange(value: string) {
    setSearch(value);
    setPage(1);
  }

  if (sortedAgents.length === 0 && sortedCards.length === 0) {
    return (
      <div className="rounded-lg p-4" style={{ backgroundColor: 'var(--bg1)' }}>
        <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--grey2)' }}>
          Cost Breakdown
        </h3>
        <div className="text-sm py-4 text-center" style={{ color: 'var(--grey0)' }}>
          No cost data yet
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {sortedAgents.length > 0 && (
        <div className="rounded-lg p-4" style={{ backgroundColor: 'var(--bg1)' }}>
          <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--grey2)' }}>
            Cost by Agent
          </h3>
          <table className="w-full text-sm">
            <thead>
              <tr style={{ color: 'var(--grey1)' }}>
                <th className="text-left py-1 font-medium">Agent</th>
                <th className="text-right py-1 font-medium">Prompt</th>
                <th className="text-right py-1 font-medium">Completion</th>
                <th className="text-right py-1 font-medium">Cost</th>
                <th className="text-right py-1 font-medium">Cards</th>
              </tr>
            </thead>
            <tbody>
              {sortedAgents.map((agent) => (
                <tr key={agent.agent_id} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
                  <td className="py-1.5" style={{ color: 'var(--aqua)' }}>{agent.agent_id}</td>
                  <td className="text-right py-1.5" style={{ color: 'var(--fg)' }}>{formatTokens(agent.prompt_tokens)}</td>
                  <td className="text-right py-1.5" style={{ color: 'var(--fg)' }}>{formatTokens(agent.completion_tokens)}</td>
                  <td className="text-right py-1.5" style={{ color: 'var(--yellow)' }}>${agent.estimated_cost_usd.toFixed(4)}</td>
                  <td className="text-right py-1.5" style={{ color: 'var(--grey1)' }}>{agent.card_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {sortedCards.length > 0 && (
        <div className="rounded-lg p-4" style={{ backgroundColor: 'var(--bg1)' }}>
          <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--grey2)' }}>
            Cost by Card
          </h3>

          {/* Search input */}
          <div className="mb-3">
            <input
              type="text"
              value={search}
              onChange={(e) => handleSearchChange(e.target.value)}
              placeholder="Search by card ID…"
              className="w-full text-sm px-3 py-1.5 rounded outline-none"
              style={{
                backgroundColor: 'var(--bg2)',
                color: 'var(--fg)',
                border: '1px solid var(--bg3)',
              }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = 'var(--aqua)';
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = 'var(--bg3)';
              }}
            />
          </div>

          {filteredCards.length === 0 ? (
            <div className="text-sm py-4 text-center" style={{ color: 'var(--grey0)' }}>
              No matching cards
            </div>
          ) : (
            <>
              <table className="w-full text-sm">
                <thead>
                  <tr style={{ color: 'var(--grey1)' }}>
                    <th className="text-left py-1 font-medium">Card</th>
                    <th className="text-left py-1 font-medium">Title</th>
                    <th className="text-left py-1 font-medium">Agent</th>
                    <th className="text-right py-1 font-medium">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleCards.map((card) => (
                    <tr key={card.card_id} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
                      <td className="py-1.5" style={{ color: 'var(--grey1)', fontFamily: 'var(--font-mono)' }}>{card.card_id}</td>
                      <td className="py-1.5 truncate max-w-48" style={{ color: 'var(--fg)' }}>{card.card_title}</td>
                      <td className="py-1.5" style={{ color: 'var(--aqua)' }}>{card.assigned_agent || '-'}</td>
                      <td className="text-right py-1.5" style={{ color: 'var(--yellow)' }}>${card.estimated_cost_usd.toFixed(4)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>

              {/* Pagination controls */}
              {totalPages > 1 && (
                <div
                  className="flex items-center justify-between mt-3 pt-2 text-sm"
                  style={{ borderTop: '1px solid var(--bg3)' }}
                >
                  <button
                    aria-label="Previous page"
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    disabled={page === 1}
                    className="px-2 py-1 rounded text-xs disabled:opacity-40"
                    style={{
                      backgroundColor: 'var(--bg2)',
                      color: 'var(--fg)',
                      border: '1px solid var(--bg3)',
                    }}
                  >
                    Previous
                  </button>
                  <span style={{ color: 'var(--grey1)' }}>
                    Page {page} of {totalPages}
                  </span>
                  <button
                    aria-label="Next page"
                    onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    disabled={page === totalPages}
                    className="px-2 py-1 rounded text-xs disabled:opacity-40"
                    style={{
                      backgroundColor: 'var(--bg2)',
                      color: 'var(--fg)',
                      border: '1px solid var(--bg3)',
                    }}
                  >
                    Next
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
