import type { ReactNode } from 'react';
import type { SentinelCardView } from '../../../../types';
import { cardStatus, sentinelSpend } from '../../../../lib/sentinel';
import { formatCost } from '../../../../lib/format';
import { ShieldCheckIcon } from '../../../ShieldCheckIcon';
import { Pill } from './SentinelParts';

function Stat({ label, mono, children }: { label: string; mono?: boolean; children: ReactNode }) {
  return (
    <div className="snt-stat">
      <div className="snt-stat-label">{label}</div>
      <div className={mono ? 'snt-stat-value snt-stat-value--mono' : 'snt-stat-value'}>{children}</div>
    </div>
  );
}

/** Status, heal attempts against the cap, Sentinel's spend on the card and its friction finding count. */
export function SentinelSummary({ view, findings }: { view: SentinelCardView; findings: number }) {
  return (
    <div role="group" aria-label="Sentinel summary" className="snt-summary">
      <Stat label="Status">
        <span className="snt-shield">
          <ShieldCheckIcon size={14} />
        </span>
        <Pill state={cardStatus(view)} />
      </Stat>
      <Stat label="Attempts">{view.run ? `${view.run.attempts} of ${view.run.max_attempts}` : '-'}</Stat>
      <Stat label="Spend" mono>
        {formatCost(sentinelSpend(view))}
      </Stat>
      <Stat label="Findings">{findings}</Stat>
    </div>
  );
}
