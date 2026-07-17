import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, useSearchParams } from 'react-router-dom';
import { TopCardsPanel } from './TopCardsPanel';
import type { CardCost, ProjectConfig } from '../../types';

const cardCosts: CardCost[] = [
  { card_id: 'ALPHA-1', card_title: 'Top card',    assigned_agent: 'agent-a', prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 9.0 },
  { card_id: 'ALPHA-2', card_title: 'Second',      assigned_agent: undefined, prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 1.0 },
  { card_id: 'ZETA-1',  card_title: 'Orphan card', assigned_agent: undefined, prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 0.5 },
];

const prefixMap = new Map<string, string>([
  ['ALPHA', 'alpha'],
  ['ZETA', 'zeta'],
]);

// Minimum shape needed for the dropdown. Cast through `unknown` so we can omit
// fields the component does not read (states/types/transitions/etc.). If the
// ProjectConfig type in web/src/types/index.ts requires additional fields,
// extend the literal to satisfy the compiler - do NOT widen `as` casts to skip
// real type errors.
const projects = ([
  { name: 'zeta',  display_name: 'Zeta Project',  prefix: 'ZETA'  },
  { name: 'alpha', display_name: 'Alpha Project', prefix: 'ALPHA' },
] as unknown) as ProjectConfig[];

function SearchParamsProbe() {
  const [params] = useSearchParams();
  return <div data-testid="search-params">{params.toString()}</div>;
}

function renderPanel(
  opts: {
    initialUrl?: string;
    cards?: CardCost[];
    prefixMap?: Map<string, string>;
  } = {},
) {
  return render(
    <MemoryRouter initialEntries={[opts.initialUrl ?? '/']}>
      <TopCardsPanel
        cardCosts={opts.cards ?? cardCosts}
        prefixMap={opts.prefixMap ?? prefixMap}
        projects={projects}
      />
      <SearchParamsProbe />
    </MemoryRouter>,
  );
}

describe('TopCardsPanel', () => {
  it('does not render a "Full breakdown" button', () => {
    renderPanel();
    expect(screen.queryByText(/Full breakdown/i)).toBeNull();
  });

  it('row link points at the board with the card ID in the query', () => {
    renderPanel();
    const row = screen.getByText('Top card').closest('a');
    expect(row).not.toBeNull();
    expect(row!.getAttribute('href')).toBe('/projects/alpha?card=ALPHA-1');
  });

  it('does not wrap rows with unmapped prefix in a link', () => {
    // Drop ZETA from the prefix map so ZETA-1 is unmapped.
    renderPanel({ prefixMap: new Map<string, string>([['ALPHA', 'alpha']]) });
    const orphan = screen.getByText('Orphan card');
    expect(orphan.closest('a')).toBeNull();
  });

  it('does not render the assigned agent name or "unassigned" subtext', () => {
    renderPanel();
    expect(screen.queryByText('agent-a')).toBeNull();
    expect(screen.queryByText(/unassigned/i)).toBeNull();
  });
});

describe('TopCardsPanel project filter', () => {
  it('renders a project dropdown with "All projects" default plus options sorted A→Z', () => {
    renderPanel();
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    const optionTexts = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(optionTexts).toEqual(['All projects', 'Alpha Project', 'Zeta Project']);
    expect(select.value).toBe('');
  });

  it('selecting a project narrows the card list to that project', () => {
    renderPanel();
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'alpha' } });
    expect(screen.getByText('Top card')).toBeInTheDocument();
    expect(screen.queryByText('Orphan card')).toBeNull();
  });

  it('selecting a project writes ?project=<name> to the URL', () => {
    renderPanel();
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'alpha' } });
    expect(screen.getByTestId('search-params').textContent).toBe('project=alpha');
  });

  it('initial URL ?project=<name> pre-selects the dropdown and applies the filter', () => {
    renderPanel({ initialUrl: '/?project=alpha' });
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    expect(select.value).toBe('alpha');
    expect(screen.queryByText('Orphan card')).toBeNull();
  });

  it('selecting "All projects" clears the URL parameter and shows all cards', () => {
    renderPanel({ initialUrl: '/?project=alpha' });
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: '' } });
    expect(screen.getByTestId('search-params').textContent).toBe('');
    expect(screen.getByText('Orphan card')).toBeInTheDocument();
  });

  it('empty-state copy names the filtered project when no cards match', () => {
    renderPanel({ initialUrl: '/?project=alpha', cards: [cardCosts[2]] });
    expect(screen.getByText(/No cards in Alpha Project/i)).toBeInTheDocument();
  });

  it('search input and project filter compose', () => {
    renderPanel({ initialUrl: '/?project=alpha' });
    const search = screen.getByPlaceholderText(/Search by card ID/i);
    fireEvent.change(search, { target: { value: 'ALPHA-1' } });
    expect(screen.getByText('Top card')).toBeInTheDocument();
    expect(screen.queryByText('Second')).toBeNull();
    expect(screen.queryByText('Orphan card')).toBeNull();
  });

  it('unknown ?project=<slug> falls back to "All projects" with the full list', () => {
    renderPanel({ initialUrl: '/?project=ghost' });
    const select = screen.getByRole('combobox', { name: /project/i }) as HTMLSelectElement;
    expect(select.value).toBe('');
    expect(screen.getByText('Top card')).toBeInTheDocument();
    expect(screen.getByText('Orphan card')).toBeInTheDocument();
  });
});
