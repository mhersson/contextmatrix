import type { MouseEvent } from 'react';
import type { AttentionItem, PlaybookNext, PlaybookSummary } from '../../types';
import { formatExactTime, formatRelativeTime } from '../../lib/format';
import { sourceIcon } from '../icons';
import { PinIcon, PlaybookIcon } from '../SessionIcons';
import { WaitBadge } from '../WaitBadge';
import { runStatusChip } from './playbookUtils';
import { RouteTrack } from './RouteTrack';

export interface PlaybookTableRowProps {
  playbook: PlaybookSummary;
  ticked: boolean;
  onToggle: (id: string) => void;
  onPin: (playbook: PlaybookSummary) => void;
  onOpen: (id: string) => void;
  waiting?: AttentionItem;
}

// The row opens its playbook on click; the tick box and pin cells must not.
const stop = (e: MouseEvent) => e.stopPropagation();

function NextUp({ next }: { next: PlaybookNext }) {
  return (
    <div className="pbt-next">
      <span className="pb-nextup-chip">next up</span>
      {next.type === 'card' && <span className="pbt-next-id">{next.card}</span>}
      <span className="pbt-next-title">{next.type === 'card' ? next.title || '(unknown card)' : next.title}</span>
    </div>
  );
}

export function PlaybookTableRow({ playbook: p, ticked, onToggle, onPin, onOpen, waiting }: PlaybookTableRowProps) {
  const pinned = p.pinned === true;
  const agentActive = p.segments.includes('active');

  return (
    <tr className={`ssn-row${ticked ? ' ssn-row--sel' : ''}`} onClick={() => onOpen(p.id)}>
      <td className="ssn-col-tick" onClick={stop}>
        <input
          type="checkbox"
          className="ssn-tick"
          aria-label={`Select ${p.title}`}
          checked={ticked}
          onChange={() => onToggle(p.id)}
        />
      </td>
      <td className="ssn-col-pin" onClick={stop}>
        <button
          type="button"
          className={`ssn-pin${pinned ? ' ssn-pin--on' : ''}`}
          aria-label={`Pin ${p.title}`}
          aria-pressed={pinned}
          onClick={() => onPin(p)}
        >
          <PinIcon filled={pinned} />
        </button>
      </td>
      <td className="ssn-col-title">
        <div className="ssn-title-line">
          <button type="button" className="ssn-open" onClick={(e) => { stop(e); onOpen(p.id); }}>
            <span className="pbt-icon"><PlaybookIcon /></span>
            <span className="ssn-open-title" title={p.title}>{p.title}</span>
          </button>
          {waiting && (
            <>
              <WaitBadge />
              <span className="ssn-wait-reason" title={waiting.reason}>{waiting.reason}</span>
            </>
          )}
          {p.source && (
            <span className="pbt-source" title={`Imported from ${p.source.system}`}>
              {sourceIcon(p.source.system) ?? p.source.system}
              {p.source.external_id}
            </span>
          )}
          {p.segments.includes('missing') && <span className="pbl-pill pbl-pill-missing">missing card</span>}
        </div>
        {p.next && <NextUp next={p.next} />}
      </td>
      <td className="ssn-col-projects" title={p.project_names.length > 0 ? p.project_names.join(', ') : undefined}>
        {p.project_names.length > 0 ? p.project_names.join(', ') : '-'}
      </td>
      <td>
        {p.run_status || agentActive ? (
          <span className="pbt-run">
            {p.run_status && (
              <span className={`pbl-pill pbl-pill-run-${p.run_status}`}>{runStatusChip(p.run_status).label}</span>
            )}
            {agentActive && <span className="pbl-pill pbl-pill-active">agent active</span>}
          </span>
        ) : (
          '-'
        )}
      </td>
      <td>
        <span className="pbt-progress">
          <RouteTrack segments={p.segments} gates={p.gates} className="pbt-track" />
          <span className="ssn-mono" aria-hidden="true">{p.complete} / {p.total}</span>
        </span>
      </td>
      <td className="ssn-muted" title={formatExactTime(p.updated_at)}>{formatRelativeTime(p.updated_at)}</td>
    </tr>
  );
}
