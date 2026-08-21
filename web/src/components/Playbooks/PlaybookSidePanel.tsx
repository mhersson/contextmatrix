import { Link } from 'react-router';
import type { NewPlaybookEntry, PlaybookDetail, PlaybookSegment } from '../../types';
import { frontierIndex } from './playbookUtils';
import { SegmentedProgress } from './SegmentedProgress';
import { AddEntryComposer } from './AddEntryComposer';

interface PlaybookSidePanelProps {
  detail: PlaybookDetail;
  segments: PlaybookSegment[];
  onAdd: (entry: NewPlaybookEntry) => Promise<void>;
}

/** Sticky workbench panel: a live overview of the playbook plus the
 * add-entry composer, keeping the entry track purely the route. */
export function PlaybookSidePanel({ detail, segments, onAdd }: PlaybookSidePanelProps) {
  const { entries } = detail;
  const projectCount = new Set(
    entries.flatMap((e) => (e.type === 'card' && e.project ? [e.project] : [])),
  ).size;
  const agentsActive = entries.filter((e) => e.card_state === 'in_progress' && !e.missing).length;
  const manualGates = entries.filter((e) => e.type === 'manual').length;
  const frontier = frontierIndex(entries);
  const next = frontier >= 0 ? entries[frontier] : null;

  return (
    <aside className="pb-side flex flex-col gap-3.5">
      <div className="pb-side-card">
        <h2 className="pb-eyebrow">Overview</h2>
        <SegmentedProgress segments={segments} />
        <p className="font-mono text-xs mt-2" style={{ color: 'var(--grey1)' }}>
          {detail.complete} of {detail.total} complete
        </p>

        <div className="mt-3 pt-2.5 text-sm" style={{ borderTop: '1px solid var(--bg1)' }}>
          <OverviewStat label="Projects" value={projectCount} />
          <OverviewStat label="Agents active" value={agentsActive} />
          <OverviewStat label="Manual gates" value={manualGates} />
        </div>

        <div className="mt-3 pt-2.5" style={{ borderTop: '1px solid var(--bg1)' }}>
          <p className="text-xs" style={{ color: 'var(--grey0)' }}>Next up</p>
          {entries.length === 0 ? (
            <p className="text-sm mt-0.5" style={{ color: 'var(--grey2)' }}>No entries yet</p>
          ) : next ? (
            next.type === 'card' ? (
              <Link
                to={`/projects/${next.project}?card=${next.card}`}
                className="flex items-baseline gap-2 mt-0.5"
                style={{ textDecoration: 'none' }}
              >
                <span className="font-mono text-xs shrink-0" style={{ color: 'var(--purple)' }}>
                  {next.card}
                </span>
                <span className="text-sm truncate" style={{ color: 'var(--fg)' }}>
                  {next.card_title ?? '(unknown card)'}
                </span>
              </Link>
            ) : (
              <p className="text-sm mt-0.5" style={{ color: 'var(--fg)' }}>{next.text}</p>
            )
          ) : (
            <p className="text-sm mt-0.5" style={{ color: 'var(--green)' }}>All entries complete</p>
          )}
        </div>
      </div>

      <AddEntryComposer onAdd={onAdd} entries={entries} />
    </aside>
  );
}

function OverviewStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex items-center justify-between py-0.5">
      <span style={{ color: 'var(--grey2)' }}>{label}</span>
      <span className="font-mono text-xs" style={{ color: 'var(--fg)' }}>{value}</span>
    </div>
  );
}
