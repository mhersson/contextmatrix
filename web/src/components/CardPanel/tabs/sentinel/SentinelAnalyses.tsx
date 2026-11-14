import type { SentinelAnalysisView } from '../../../../types';
import { formatAge, formatCost, formatExactTime } from '../../../../lib/format';
import { Empty, Pill, Table } from './SentinelParts';

// The column names the analysis, so its stages read as bare results.
const RESULTS: Record<string, string> = {
  analyse: 'analysing',
  analyse_failed: 'failed',
  analyse_skipped: 'skipped',
};

/** The card's run analyses, newest first. */
export function SentinelAnalyses({ analyses }: { analyses: SentinelAnalysisView[] }) {
  if (analyses.length === 0) return <Empty>No analyses.</Empty>;
  return (
    <Table>
      <thead>
        <tr>
          <th style={{ width: 52 }}>When</th>
          <th style={{ width: 96 }}>Result</th>
          <th>Detail</th>
          <th className="snt-right" style={{ width: 72 }}>
            Cost
          </th>
        </tr>
      </thead>
      <tbody>
        {analyses.map((a) => (
          <tr key={a.id}>
            <td className="snt-num" title={formatExactTime(a.created_at)}>
              {formatAge(a.created_at)}
            </td>
            <td>
              <Pill state={a.stage}>{RESULTS[a.stage]}</Pill>
            </td>
            <td className="snt-muted" title={a.reason || undefined}>
              {a.reason}
            </td>
            <td className="snt-num snt-right">{formatCost(a.spend_usd)}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
