import type { MouseEvent } from 'react';
import type { ChatSession } from '../../types';
import { formatSessionCost, sessionProjects, statusDotColor } from '../../lib/sessions';
import { formatRelativeTime } from '../../lib/format';
import { PinIcon } from '../SessionIcons';

export interface SessionRowProps {
  session: ChatSession;
  showMode: boolean;
  ticked: boolean;
  onToggle: (id: string) => void;
  onPin: (session: ChatSession) => void;
  onOpen: (id: string) => void;
}

// The row opens its session on click; the tick box and pin cells must not.
const stop = (e: MouseEvent) => e.stopPropagation();

function modeTone(mode: string): string {
  if (mode === 'yolo') return ' ssn-tag--yolo';
  return mode.startsWith('auto') ? ' ssn-tag--auto' : '';
}

function exactTime(iso: string): string | undefined {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? undefined : d.toLocaleString();
}

export function SessionRow({ session: s, showMode, ticked, onToggle, onPin, onOpen }: SessionRowProps) {
  const title = s.title || '(untitled)';
  const projects = sessionProjects(s);
  const pinned = s.pinned === true;

  return (
    <tr className={`ssn-row${ticked ? ' ssn-row--sel' : ''}`} onClick={() => onOpen(s.id)}>
      <td className="ssn-col-tick" onClick={stop}>
        <input
          type="checkbox"
          className="ssn-tick"
          aria-label={`Select ${title}`}
          checked={ticked}
          onChange={() => onToggle(s.id)}
        />
      </td>
      <td className="ssn-col-pin" onClick={stop}>
        <button
          type="button"
          className={`ssn-pin${pinned ? ' ssn-pin--on' : ''}`}
          aria-label={`Pin ${title}`}
          aria-pressed={pinned}
          onClick={() => onPin(s)}
        >
          <PinIcon filled={pinned} />
        </button>
      </td>
      <td className="ssn-col-title">
        <button type="button" className="ssn-open" onClick={(e) => { stop(e); onOpen(s.id); }}>
          <span
            className="ssn-dot"
            style={{ background: statusDotColor(s.status) ?? 'var(--bg4)' }}
            title={s.status}
            aria-hidden="true"
          />
          <span className="ssn-open-title">{title}</span>
        </button>
      </td>
      <td>{projects.length > 0 ? projects.join(', ') : '-'}</td>
      {showMode && (
        <td>{s.mode ? <span className={`ssn-tag${modeTone(s.mode)}`}>{s.mode}</span> : '-'}</td>
      )}
      <td className="ssn-mono">{formatSessionCost(s)}</td>
      <td className="ssn-muted" title={exactTime(s.last_active)}>{formatRelativeTime(s.last_active)}</td>
    </tr>
  );
}
