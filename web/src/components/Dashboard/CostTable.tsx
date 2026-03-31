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

export function CostTable({ agentCosts, cardCosts }: CostTableProps) {
  const sortedAgents = [...agentCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd);
  const sortedCards = [...cardCosts].sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd);

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
              {sortedCards.map((card) => (
                <tr key={card.card_id} className="border-t" style={{ borderColor: 'var(--bg3)' }}>
                  <td className="py-1.5" style={{ color: 'var(--grey1)', fontFamily: 'var(--font-mono)' }}>{card.card_id}</td>
                  <td className="py-1.5 truncate max-w-48" style={{ color: 'var(--fg)' }}>{card.card_title}</td>
                  <td className="py-1.5" style={{ color: 'var(--aqua)' }}>{card.assigned_agent || '-'}</td>
                  <td className="text-right py-1.5" style={{ color: 'var(--yellow)' }}>${card.estimated_cost_usd.toFixed(4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
