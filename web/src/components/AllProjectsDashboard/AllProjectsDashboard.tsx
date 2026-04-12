import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummaries } from '../../hooks/useProjectSummaries';
import { SummaryCards } from '../Dashboard/SummaryCards';
import { ActiveAgentsFeed } from '../Dashboard/ActiveAgentsFeed';
import { CostTable } from '../Dashboard/CostTable';
import type { DashboardData } from '../../types';

function aggregateDashboards(summaries: Map<string, DashboardData>): DashboardData {
  const stateCounts: Record<string, number> = {};
  let totalCost = 0;
  let completedToday = 0;
  const allAgents: DashboardData['active_agents'] = [];
  const agentCostMap = new Map<string, DashboardData['agent_costs'][0]>();
  const allCardCosts: DashboardData['card_costs'] = [];

  for (const data of summaries.values()) {
    for (const [state, count] of Object.entries(data.state_counts)) {
      stateCounts[state] = (stateCounts[state] || 0) + count;
    }
    totalCost += data.total_cost_usd;
    completedToday += data.cards_completed_today;
    allAgents.push(...data.active_agents);
    allCardCosts.push(...data.card_costs);
    for (const ac of data.agent_costs) {
      const existing = agentCostMap.get(ac.agent_id);
      if (existing) {
        existing.prompt_tokens += ac.prompt_tokens;
        existing.completion_tokens += ac.completion_tokens;
        existing.estimated_cost_usd += ac.estimated_cost_usd;
        existing.card_count += ac.card_count;
      } else {
        agentCostMap.set(ac.agent_id, { ...ac });
      }
    }
  }

  return {
    state_counts: stateCounts,
    active_agents: allAgents,
    total_cost_usd: totalCost,
    cards_completed_today: completedToday,
    agent_costs: Array.from(agentCostMap.values()),
    card_costs: allCardCosts,
  };
}

export function AllProjectsDashboard() {
  const { projects } = useProjects();
  const projectNames = useMemo(() => projects.map((p) => p.name), [projects]);
  const { summaries, loading } = useProjectSummaries(projectNames);

  const aggregated = useMemo(() => aggregateDashboards(summaries), [summaries]);

  if (loading && summaries.size === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>Loading dashboard...</div>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6 overflow-y-auto h-full">
      <h2 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
        All Projects Dashboard
      </h2>

      <SummaryCards
        stateCounts={aggregated.state_counts}
        totalCost={aggregated.total_cost_usd}
        completedToday={aggregated.cards_completed_today}
      />

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {projects.map((p) => {
          const data = summaries.get(p.name);
          const total = data ? Object.values(data.state_counts).reduce((a, b) => a + b, 0) : 0;
          return (
            <Link
              key={p.name}
              to={`/projects/${p.name}/dashboard`}
              className="rounded-lg p-3 transition-colors hover:brightness-110"
              style={{ backgroundColor: 'var(--bg1)' }}
            >
              <div className="text-sm font-medium" style={{ color: 'var(--aqua)' }}>
                {p.jira?.epic_key && <><span style={{ color: 'var(--grey1)' }}>{p.jira.epic_key}</span>{' '}</>}
                {p.name}
              </div>
              <div className="flex items-baseline gap-2 mt-1">
                <span className="text-lg font-bold" style={{ color: 'var(--fg)' }}>{total}</span>
                <span className="text-xs" style={{ color: 'var(--grey0)' }}>cards</span>
              </div>
              {data && (
                <div className="text-xs mt-1" style={{ color: 'var(--yellow)' }}>
                  ${data.total_cost_usd.toFixed(2)}
                </div>
              )}
            </Link>
          );
        })}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ActiveAgentsFeed agents={aggregated.active_agents} />
        <CostTable agentCosts={aggregated.agent_costs} cardCosts={aggregated.card_costs} />
      </div>
    </div>
  );
}
