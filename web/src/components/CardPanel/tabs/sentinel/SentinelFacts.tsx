import type { SentinelRunView } from '../../../../types';
import { formatRelativeTime } from '../../../../lib/format';
import { Heading, Table } from './SentinelParts';

const ORIGINS: Record<SentinelRunView['origin'], string> = {
  rest: 'UI or REST',
  mcp: 'MCP',
  playbook: 'Playbook',
  sentinel: 'Sentinel',
};

function lastStop(run: SentinelRunView): string | null {
  if (run.human_stop) return 'stopped by a human';
  if (!run.stop_cause && !run.stopped_at) return null;
  const cause = run.stop_cause || 'cause unknown';
  return run.stopped_at ? `${cause}, ${formatRelativeTime(run.stopped_at)}` : cause;
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <tr>
      <td>{label}</td>
      <td title={value}>{value}</td>
    </tr>
  );
}

/** How the card's last run was launched, the heal attempts it used and how it stopped. */
export function SentinelFacts({ run }: { run: SentinelRunView }) {
  const stop = lastStop(run);
  return (
    <section>
      <Heading>Facts</Heading>
      <Table kv testId="sentinel-facts">
        <tbody>
          <Fact label="Launched from" value={ORIGINS[run.origin]} />
          <Fact label="Autonomous at launch" value={run.autonomous ? 'Yes' : 'No'} />
          <Fact label="Heal attempts" value={`${run.attempts} of ${run.max_attempts}`} />
          {stop && <Fact label="Last stop" value={stop} />}
        </tbody>
      </Table>
    </section>
  );
}
