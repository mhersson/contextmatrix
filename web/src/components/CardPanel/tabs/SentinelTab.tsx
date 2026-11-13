import type { Card, SentinelRunView } from '../../../types';
import { useCardSentinel } from '../../../hooks/useSentinel';
import { formatRelativeTime } from '../../../lib/format';

const ORIGINS: Record<SentinelRunView['origin'], string> = {
  rest: 'UI or REST',
  mcp: 'MCP',
  playbook: 'Playbook',
  sentinel: 'Sentinel',
};

function lastStop(run: SentinelRunView): string | null {
  if (run.human_stop) return 'stopped by a human';
  if (!run.stop_cause) return null;
  return run.stopped_at ? `${run.stop_cause}, ${formatRelativeTime(run.stopped_at)}` : run.stop_cause;
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3 text-sm">
      <dt style={{ color: 'var(--grey1)' }}>{label}</dt>
      <dd style={{ color: 'var(--fg)' }}>{value}</dd>
    </div>
  );
}

/** Sentinel rail tab: how the card's last run was launched and how it stopped. */
export function SentinelTab({ card }: { card: Card }) {
  const view = useCardSentinel(card.project, card.id);
  if (!view) return null;

  const run = view.run;
  if (!run) {
    return (
      <p className="p-4 text-sm" style={{ color: 'var(--grey1)' }}>
        Sentinel acts only on runs this instance launched.
      </p>
    );
  }

  const stop = lastStop(run);

  return (
    <dl className="p-4 flex flex-col gap-2">
      <Fact label="Launched from" value={ORIGINS[run.origin]} />
      <Fact label="Autonomous at launch" value={run.autonomous ? 'Yes' : 'No'} />
      <Fact label="Heal attempts" value={`${run.attempts} of ${run.max_attempts}`} />
      {stop && <Fact label="Last stop" value={stop} />}
    </dl>
  );
}
