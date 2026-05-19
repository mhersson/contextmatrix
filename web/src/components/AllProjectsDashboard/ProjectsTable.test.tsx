import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ProjectsTable } from './ProjectsTable';
import type { ProjectConfig, DashboardData } from '../../types';

function project(name: string, prefix: string): ProjectConfig {
  return {
    name,
    prefix,
    next_id: 1,
    states: ['todo', 'done'],
    types: ['task'],
    priorities: ['medium'],
    transitions: { todo: ['done'], done: [] },
    remote_execution: { enabled: false },
  } as ProjectConfig;
}

function summary(total: number): DashboardData {
  return {
    state_counts: { todo: total, done: 0 },
    active_agents: [],
    total_cost_usd: 0,
    cards_completed_today: 0,
    cards_completed_last_7d: 0,
    cards_completed_prior_7d: 0,
    metric_series: { points: [] },
    agent_costs: [],
    card_costs: [],
    model_costs: [],
  } as unknown as DashboardData;
}

describe('ProjectsTable', () => {
  // Totals are chosen so card-count sort (charlie=5 > bravo=3 > alpha=1)
  // produces a DIFFERENT order than alphabetical (alpha → bravo → charlie).
  const projects = [project('charlie', 'CHA'), project('alpha', 'ALP'), project('bravo', 'BRA')];
  const summaries = new Map<string, DashboardData>([
    ['charlie', summary(5)],
    ['alpha', summary(1)],
    ['bravo', summary(3)],
  ]);

  function renderTable() {
    return render(
      <MemoryRouter>
        <ProjectsTable projects={projects} summaries={summaries} />
      </MemoryRouter>,
    );
  }

  it('sorts projects alphabetically by name', () => {
    renderTable();
    const links = screen.getAllByRole('link');
    const names = links.map((l) => l.textContent ?? '');
    const idxAlpha = names.findIndex((n) => n.includes('alpha'));
    const idxBravo = names.findIndex((n) => n.includes('bravo'));
    const idxCharlie = names.findIndex((n) => n.includes('charlie'));
    expect(idxAlpha).toBeLessThan(idxBravo);
    expect(idxBravo).toBeLessThan(idxCharlie);
  });

  it('row link points at the project board, not the cost dashboard', () => {
    renderTable();
    const alphaLink = screen.getAllByRole('link').find((l) => l.textContent?.includes('alpha'));
    expect(alphaLink).toBeDefined();
    expect(alphaLink!.getAttribute('href')).toBe('/projects/alpha');
  });

  it('does not render a Status column', () => {
    renderTable();
    expect(screen.queryByText(/status/i)).toBeNull();
    expect(screen.queryByText(/on track/i)).toBeNull();
  });

  it('does not render an Active agents column', () => {
    renderTable();
    expect(screen.queryByText(/active agents/i)).toBeNull();
  });
});
