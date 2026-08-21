import { useMemo } from 'react';
import type { Card, ProjectConfig } from '../../types';
import { isTerminalState } from '../../lib/cardState';

interface CardPickerProps {
  projects: ProjectConfig[];
  project: string;
  onProjectChange: (project: string) => void;
  cards: Card[];
  filter: string;
  onFilterChange: (filter: string) => void;
  selectedCard: Card | null;
  onSelectCard: (card: Card) => void;
}

const inputStyle = {
  backgroundColor: 'var(--bg2)',
  border: '1px solid var(--bg3)',
  color: 'var(--fg)',
};

/** Project select + client-side filtered card list, for the composer's Card mode. */
export function CardPicker({
  projects, project, onProjectChange, cards, filter, onFilterChange, selectedCard, onSelectCard,
}: CardPickerProps) {
  const filteredCards = useMemo(() => {
    const term = filter.trim().toLowerCase();
    const openCards = cards.filter((c) => !isTerminalState(c.state));
    const matches = term
      ? openCards.filter((c) => c.id.toLowerCase().includes(term) || c.title.toLowerCase().includes(term))
      : openCards;
    return matches.slice(0, 8);
  }, [cards, filter]);

  return (
    <div className="flex flex-col gap-2">
      <select
        value={project}
        onChange={(e) => onProjectChange(e.target.value)}
        className="px-2 py-1 rounded text-sm"
        style={inputStyle}
      >
        {projects.map((p) => (
          <option key={p.name} value={p.name}>{p.display_name || p.name}</option>
        ))}
      </select>
      <input
        value={filter}
        onChange={(e) => onFilterChange(e.target.value)}
        placeholder="Filter cards by id or title"
        className="px-2 py-1 rounded text-sm"
        style={inputStyle}
      />
      {!selectedCard && filter && (
        <ul className="max-h-40 overflow-y-auto rounded border" style={{ borderColor: 'var(--bg3)' }}>
          {filteredCards.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => onSelectCard(c)}
                className="w-full text-left px-2 py-1 text-sm"
                style={{ color: 'var(--fg)' }}
              >
                <span className="font-mono text-xs mr-2" style={{ color: 'var(--grey1)' }}>{c.id}</span>
                {c.title}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
