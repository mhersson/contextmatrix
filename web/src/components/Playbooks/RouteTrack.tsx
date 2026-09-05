import type { PlaybookSegment } from '../../types';
import { SegmentedProgress } from './SegmentedProgress';

interface RouteTrackProps {
  segments: PlaybookSegment[];
  /** Indexes of manual entries, drawn as gates. */
  gates?: number[];
  className?: string;
}

/** Nodes stop reading as a route once they crowd; longer playbooks get the
 * segments strip instead. */
export const ROUTE_NODE_CAP = 20;

export function RouteTrack({ segments, gates = [], className }: RouteTrackProps) {
  if (segments.length > ROUTE_NODE_CAP) {
    return <SegmentedProgress segments={segments} className={className} />;
  }

  const complete = segments.filter((s) => s === 'complete').length;
  const frontier = segments.findIndex((s) => s !== 'complete');

  return (
    <div
      className={`pbl-track ${className ?? ''}`}
      role="img"
      aria-label={`${complete} of ${segments.length} complete`}
    >
      {segments.map((seg, i) => {
        // The ring marks a pending frontier only: an active or missing
        // frontier already carries its own state colour.
        const cls = [
          'pbl-node',
          `pbl-node-${seg}`,
          i === frontier && seg === 'pending' ? 'pbl-node-frontier' : '',
          gates.includes(i) ? 'pbl-node-gate' : '',
        ].filter(Boolean).join(' ');
        return (
          <span key={i} className="contents">
            <span className={cls} />
            {i < segments.length - 1 && (
              <span className={`pbl-rail ${seg === 'complete' ? 'pbl-rail-on' : 'pbl-rail-dash'}`} />
            )}
          </span>
        );
      })}
    </div>
  );
}
