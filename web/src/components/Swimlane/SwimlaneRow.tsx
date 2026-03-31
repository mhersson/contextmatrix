import { Link } from 'react-router-dom';
import type { Card } from '../../types';
import { SwimlaneCell } from './SwimlaneCell';

interface SwimlaneRowProps {
  project: string;
  cards: Card[];
  states: string[];
}

export function SwimlaneRow({ project, cards, states }: SwimlaneRowProps) {
  const cardsByState = new Map<string, Card[]>();
  for (const state of states) {
    cardsByState.set(state, []);
  }
  for (const card of cards) {
    const list = cardsByState.get(card.state);
    if (list) list.push(card);
  }

  return (
    <tr>
      <td
        className="px-3 py-2 font-medium text-sm sticky left-0 z-10"
        style={{ backgroundColor: 'var(--bg-dim)', color: 'var(--aqua)' }}
      >
        <Link to={`/projects/${project}`} className="hover:underline">
          {project}
        </Link>
      </td>
      {states.map((state) => (
        <td key={state} className="px-1 py-1">
          <SwimlaneCell cards={cardsByState.get(state) || []} />
        </td>
      ))}
    </tr>
  );
}
