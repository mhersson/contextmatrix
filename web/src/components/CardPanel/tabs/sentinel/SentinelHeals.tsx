import type { SentinelHealView } from '../../../../types';
import { formatAge, formatCost, formatExactTime } from '../../../../lib/format';
import { lineDiff, verifyLines, type DiffLine } from '../../../../lib/sentinel';
import { CaretHeader, DetailLabel, Empty, ExpandRow, Pill, Table } from './SentinelParts';

// Foreground text on the tint: the coloured --green/--red text fell below 3:1 in light palettes.
const DIFF_STYLE: Record<DiffLine['kind'], { background?: string; mark: string }> = {
  same: { mark: ' ' },
  add: { background: 'var(--bg-green)', mark: '+' },
  del: { background: 'var(--bg-red)', mark: '-' },
};

function Diff({ label, before, after }: { label: string; before: string; after: string }) {
  if (before === after) return null;
  return (
    <>
      <DetailLabel>{label}</DetailLabel>
      <pre className="text-xs whitespace-pre-wrap wrap-anywhere rounded p-2" style={{ background: 'var(--bg1)' }}>
        {lineDiff(before, after).map((l, i) => (
          <div key={i} style={{ color: 'var(--fg)', background: DIFF_STYLE[l.kind].background }}>
            {DIFF_STYLE[l.kind].mark} {l.text}
          </div>
        ))}
      </pre>
    </>
  );
}

// A heal records its change before it reruns the card: a parked heal never reran, and a forward row has no rerun.
const RERAN_STAGES = new Set(['rerun', 'healed', 'rerun_failed', 'closed']);

function HealDetail({ heal }: { heal: SentinelHealView }) {
  const change = heal.change;
  const parked = heal.stage === 'parked';
  return (
    <>
      {heal.diagnosis && <p className="snt-text">{heal.diagnosis}</p>}
      {change && (
        <>
          <Diff label="## Sentinel section" before={change.section_before} after={change.section_after} />
          <Diff label="Verify" before={verifyLines(change.verify_before)} after={verifyLines(change.verify_after)} />
          {change.retired.length > 0 && (
            <>
              <DetailLabel>Retired subtasks</DetailLabel>
              <p className="snt-text">{change.retired.join(', ')}</p>
            </>
          )}
          {RERAN_STAGES.has(heal.stage) && (
            <>
              <DetailLabel>Rerun</DetailLabel>
              <p className="snt-muted">
                {change.fresh ? 'The rerun starts fresh.' : 'The rerun resumes where the run stopped.'}
              </p>
            </>
          )}
        </>
      )}
      {heal.dropped.length > 0 && (
        <>
          <DetailLabel>Section dropped</DetailLabel>
          <ul className="snt-text list-disc pl-5">
            {heal.dropped.map((reason, i) => (
              <li key={i}>{reason}</li>
            ))}
          </ul>
        </>
      )}
      {heal.reason && (
        <>
          <DetailLabel>{parked ? 'Parked' : 'Reason'}</DetailLabel>
          <p className={parked ? 'snt-park' : 'snt-muted'}>{heal.reason}</p>
        </>
      )}
      <div className="snt-narrow">
        <DetailLabel>Spend</DetailLabel>
        <p className="snt-num">{formatCost(heal.spend_usd)}</p>
      </div>
    </>
  );
}

function HealRow({ heal, defaultOpen }: { heal: SentinelHealView; defaultOpen: boolean }) {
  const cause = heal.kind === 'forward' ? `Forwarded from ${heal.source_card}` : heal.cause;
  return (
    <ExpandRow
      label={cause}
      span={5}
      defaultOpen={defaultOpen}
      cells={
        <>
          <td className="snt-mono" title={cause}>
            {cause}
          </td>
          <td>
            <Pill state={heal.stage} />
          </td>
          <td className="snt-num snt-right" title={formatExactTime(heal.created_at)}>
            {formatAge(heal.created_at)}
          </td>
          <td className="snt-num snt-right snt-wide">{formatCost(heal.spend_usd)}</td>
        </>
      }
    >
      <HealDetail heal={heal} />
    </ExpandRow>
  );
}

/** The card's heal and forward rows, newest first and open, each expanding to its diagnosis and change. */
export function SentinelHeals({ heals }: { heals: SentinelHealView[] }) {
  if (heals.length === 0) return <Empty>No heals.</Empty>;
  return (
    <Table>
      <thead>
        <tr>
          <CaretHeader />
          <th>Cause</th>
          <th style={{ width: 120 }}>Stage</th>
          <th className="snt-right" style={{ width: 48 }}>
            Age
          </th>
          <th className="snt-right snt-wide">Spend</th>
        </tr>
      </thead>
      <tbody>
        {heals.map((h, i) => (
          <HealRow key={h.id} heal={h} defaultOpen={i === 0} />
        ))}
      </tbody>
    </Table>
  );
}
