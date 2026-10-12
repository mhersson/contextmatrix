import type { JiraImportResult, JiraImportStatus } from '../../types';
import type { ImportRun } from './useJiraImportRun';

const STATUS_COLOR: Record<JiraImportStatus, string> = {
  created: 'var(--green)',
  skipped: 'var(--grey1)',
  failed: 'var(--red)',
};

interface JiraImportResultsProps {
  run: ImportRun;
  /** The selected issue keys, in request order. */
  keys: string[];
  onOpen: () => void;
  onClose: () => void;
}

function summary(run: ImportRun): string {
  const count = `${run.results.length} of ${run.total}`;
  if (run.status === 'running') return `Importing… ${count}`;
  if (run.status === 'failed') return `Import stopped after ${count}: ${run.error ?? 'see the results'}`;
  const tally = (status: JiraImportStatus) => run.results.filter((r) => r.status === status).length;
  const others = (['skipped', 'failed'] as const).filter((st) => tally(st) > 0).map((st) => `${tally(st)} ${st}`);
  return `Imported ${tally('created')} of ${run.total}${others.length ? ` (${others.join(', ')})` : ''}`;
}

function ResultRow({ result }: { result: JiraImportResult }) {
  return (
    <li className="flex gap-2 items-baseline">
      <span className="font-mono text-xs w-24 shrink-0" style={{ color: 'var(--fg)' }}>{result.key}</span>
      <span className="w-16 shrink-0" style={{ color: STATUS_COLOR[result.status] }}>{result.status}</span>
      {result.card && <span className="font-mono text-xs shrink-0">{result.project}/{result.card}</span>}
      {result.reason && <span className="min-w-0" style={{ color: 'var(--grey1)' }}>{result.reason}</span>}
    </li>
  );
}

export function JiraImportResults({ run, keys, onOpen, onClose }: JiraImportResultsProps) {
  // A stopped import leaves the selections it never reached without a result;
  // listing them keeps a partial import from looking complete.
  const reached = new Set(run.results.map((r) => r.key));
  const notRun = run.status === 'failed' ? keys.filter((k) => !reached.has(k)) : [];

  return (
    <div className="flex flex-col gap-3">
      <p
        role="status"
        className="text-sm"
        style={{ color: run.status === 'failed' ? 'var(--red)' : 'var(--grey2)' }}
      >
        {summary(run)}
      </p>
      <ul aria-label="Import results" className="flex flex-col gap-1 text-sm max-h-80 overflow-y-auto">
        {run.results.map((r) => <ResultRow key={r.key} result={r} />)}
        {notRun.map((k) => (
          <li key={k} className="flex gap-2 items-baseline">
            <span className="font-mono text-xs w-24 shrink-0" style={{ color: 'var(--fg)' }}>{k}</span>
            <span className="w-16 shrink-0" style={{ color: 'var(--yellow)' }}>not run</span>
          </li>
        ))}
      </ul>
      <div className="flex gap-2 justify-end">
        <button type="button" onClick={onClose} className="bf-btn-ghost bf-btn-sm">Close</button>
        <button
          type="button"
          onClick={onOpen}
          disabled={run.status === 'running' || !run.playbookId}
          className="bf-btn-primary"
        >
          Open playbook
        </button>
      </div>
    </div>
  );
}
