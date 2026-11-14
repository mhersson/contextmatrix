import type { SentinelGuidanceRevision, SentinelStandingPR } from '../../../../types';
import { formatExactTime, formatRelativeTime } from '../../../../lib/format';
import { Empty, ExternalLink, Pill, Table } from './SentinelParts';

const PR_STATES: Record<string, string> = {
  open: 'open',
  changed_outside: 'changed outside Sentinel',
  merged: 'merged',
  closed: 'closed',
};

function Age({ iso }: { iso: string }) {
  return <td title={formatExactTime(iso)}>{formatRelativeTime(iso)}</td>;
}

/** The project's standing guidance PR, then its newest revision when that did not go out. */
export function SentinelGuidancePR({
  pr,
  revision,
}: {
  pr: SentinelStandingPR | null;
  revision: SentinelGuidanceRevision | null;
}) {
  if (!pr && !revision) return <Empty>No guidance PR.</Empty>;
  return (
    <Table kv>
      {pr && (
        <tbody>
          <tr>
            <td>Pull request</td>
            <td title={pr.url}>
              <ExternalLink href={pr.url}>{pr.url.replace(/^https?:\/\//, '')}</ExternalLink>
            </td>
          </tr>
          {PR_STATES[pr.state] && (
            <tr>
              <td>State</td>
              <td>
                <Pill state={pr.state}>{PR_STATES[pr.state]}</Pill>
              </td>
            </tr>
          )}
          <tr>
            <td>File</td>
            <td className="snt-mono" title={pr.file}>
              {pr.file}
            </td>
          </tr>
          <tr>
            <td>Last push</td>
            <Age iso={pr.pushed_at} />
          </tr>
        </tbody>
      )}
      {revision && (
        <tbody>
          <tr>
            <td>Last revision</td>
            <td>
              <Pill state={revision.stage} />
            </td>
          </tr>
          {revision.reason && (
            <tr>
              <td>Reason</td>
              <td className="snt-wrap">{revision.reason}</td>
            </tr>
          )}
          <tr>
            <td>When</td>
            <Age iso={revision.updated_at} />
          </tr>
        </tbody>
      )}
    </Table>
  );
}
