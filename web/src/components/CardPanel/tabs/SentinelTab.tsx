import { Link } from 'react-router';
import type {
  Card,
  SentinelAnalysisView,
  SentinelStandingPR,
  SentinelFindingKind,
  SentinelFindingView,
  SentinelHealView,
  SentinelRunView,
} from '../../../types';
import { useCardSentinel } from '../../../hooks/useSentinel';
import { formatCost, formatRelativeTime } from '../../../lib/format';
import { findingOutcome, lineDiff, verifyLines, type DiffLine } from '../../../lib/sentinel';

const ORIGINS: Record<SentinelRunView['origin'], string> = {
  rest: 'UI or REST',
  mcp: 'MCP',
  playbook: 'Playbook',
  sentinel: 'Sentinel',
};

const FRICTION: { kind: SentinelFindingKind; label: string }[] = [
  { kind: 'guidance', label: 'Guidance' },
  { kind: 'code', label: 'Code' },
  { kind: 'ecosystem', label: 'Ecosystem' },
];

const NOTES: { kind: SentinelFindingKind; label: string }[] = [
  { kind: 'card_advice', label: 'Card advice' },
  { kind: 'project_verify', label: 'Project verify' },
  { kind: 'hard_card', label: 'Hard card' },
];

function lastStop(run: SentinelRunView): string | null {
  if (run.human_stop) return 'stopped by a human';
  if (!run.stop_cause && !run.stopped_at) return null;
  const cause = run.stop_cause || 'cause unknown';
  return run.stopped_at ? `${cause}, ${formatRelativeTime(run.stopped_at)}` : cause;
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3 text-sm">
      <dt style={{ color: 'var(--grey1)' }}>{label}</dt>
      <dd style={{ color: 'var(--fg)' }}>{value}</dd>
    </div>
  );
}

// Foreground text on the tint: the coloured --green/--red text fell below 3:1 in light palettes.
const DIFF_STYLE: Record<DiffLine['kind'], { background?: string; mark: string }> = {
  same: { mark: ' ' },
  add: { background: 'var(--bg-green)', mark: '+' },
  del: { background: 'var(--bg-red)', mark: '-' },
};

function Diff({ label, before, after }: { label: string; before: string; after: string }) {
  if (before === after) return null;
  return (
    <div className="flex flex-col gap-1">
      <h4 className="text-xs uppercase" style={{ color: 'var(--grey1)' }}>
        {label}
      </h4>
      <pre className="text-xs whitespace-pre-wrap wrap-anywhere rounded p-2" style={{ background: 'var(--bg1)' }}>
        {lineDiff(before, after).map((l, i) => (
          <div key={i} style={{ color: 'var(--fg)', background: DIFF_STYLE[l.kind].background }}>
            {DIFF_STYLE[l.kind].mark} {l.text}
          </div>
        ))}
      </pre>
    </div>
  );
}

// A heal records its change before it reruns the card: a parked heal never reran, and a forward row has no rerun.
const RERAN_STAGES = new Set(['rerun', 'healed', 'rerun_failed', 'closed']);

function Heal({ heal }: { heal: SentinelHealView }) {
  const change = heal.change;
  const reran = RERAN_STAGES.has(heal.stage);
  const title = heal.kind === 'forward' ? `Forwarded from ${heal.source_card}` : heal.cause;
  return (
    <section className="flex flex-col gap-2 pb-3" style={{ borderBottom: '1px solid var(--bg3)' }}>
      <header className="flex justify-between gap-3 text-sm">
        <span style={{ color: 'var(--fg)' }}>
          {title}, {heal.stage}
        </span>
        <span style={{ color: 'var(--grey1)' }}>
          {formatRelativeTime(heal.created_at)}, {formatCost(heal.spend_usd)}
        </span>
      </header>
      {heal.reason && (
        <p className="text-sm" style={{ color: 'var(--grey1)' }}>
          {heal.reason}
        </p>
      )}
      {heal.diagnosis && (
        <p className="text-sm whitespace-pre-wrap" style={{ color: 'var(--fg)' }}>
          {heal.diagnosis}
        </p>
      )}
      {change && (
        <>
          <Diff label="## Sentinel" before={change.section_before} after={change.section_after} />
          <Diff label="Verify" before={verifyLines(change.verify_before)} after={verifyLines(change.verify_after)} />
          {change.retired.length > 0 && (
            <p className="text-sm" style={{ color: 'var(--fg)' }}>
              Retired subtasks: {change.retired.join(', ')}
            </p>
          )}
          {reran && (
            <p className="text-sm" style={{ color: 'var(--grey1)' }}>
              {change.fresh ? 'The rerun starts fresh.' : 'The rerun resumes where the run stopped.'}
            </p>
          )}
        </>
      )}
      {heal.dropped.length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 className="text-xs uppercase" style={{ color: 'var(--grey1)' }}>
            Section dropped
          </h4>
          <ul className="text-sm list-disc pl-5 wrap-anywhere" style={{ color: 'var(--fg)' }}>
            {heal.dropped.map((reason, i) => (
              <li key={i}>{reason}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

const PR_STATES: Record<string, string> = {
  open: 'open',
  changed_outside: 'changed outside Sentinel',
  merged: 'merged',
  closed: 'closed',
};

function ExternalLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" style={{ color: 'var(--blue)' }}>
      {children}
    </a>
  );
}

function StandingPR({ pr }: { pr: SentinelStandingPR }) {
  return (
    <section className="flex flex-col gap-1 text-sm">
      <h3 className="font-medium" style={{ color: 'var(--fg)' }}>
        Standing guidance PR
      </h3>
      <p className="wrap-anywhere">
        <ExternalLink href={pr.url}>{pr.url}</ExternalLink>
      </p>
      <p className="flex flex-wrap gap-x-3" style={{ color: 'var(--grey1)' }}>
        {PR_STATES[pr.state] && <span data-testid="standing-pr-state">{PR_STATES[pr.state]}</span>}
        <span>{pr.file}</span>
      </p>
    </section>
  );
}

function Outcome({ finding, project, prURL }: { finding: SentinelFindingView; project: string; prURL?: string }) {
  const outcome = findingOutcome(finding, project, prURL);
  if (!outcome) return null;
  let line: React.ReactNode = outcome.text;
  if (outcome.href && outcome.external) line = <ExternalLink href={outcome.href}>{outcome.text}</ExternalLink>;
  else if (outcome.href) {
    line = (
      <Link to={outcome.href} style={{ color: 'var(--blue)' }}>
        {outcome.text}
      </Link>
    );
  }
  return (
    <p data-testid="finding-outcome" className="text-xs wrap-anywhere" style={{ color: 'var(--grey1)' }}>
      {line}
    </p>
  );
}

function Analyses({ analyses }: { analyses: SentinelAnalysisView[] }) {
  if (analyses.length === 0) return null;
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-medium" style={{ color: 'var(--fg)' }}>
        Analyses
      </h3>
      {analyses.map((a) => (
        <div key={a.id} data-testid="analysis" className="flex flex-col gap-0.5 text-sm">
          <div className="flex justify-between gap-3">
            <span style={{ color: 'var(--fg)' }}>{a.stage}</span>
            <span style={{ color: 'var(--grey1)' }}>
              {formatRelativeTime(a.created_at)}, {formatCost(a.spend_usd)}
            </span>
          </div>
          {a.reason && (
            <p className="wrap-anywhere" style={{ color: 'var(--grey1)' }}>
              {a.reason}
            </p>
          )}
        </div>
      ))}
    </section>
  );
}

function Findings({
  title,
  groups,
  findings,
  project,
  prURL,
}: {
  title: string;
  groups: { kind: SentinelFindingKind; label: string }[];
  findings: SentinelFindingView[];
  project: string;
  prURL?: string;
}) {
  const shown = groups.flatMap((g) => findings.filter((f) => f.kind === g.kind).map((f) => ({ ...f, label: g.label })));
  if (shown.length === 0) return null;
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-medium" style={{ color: 'var(--fg)' }}>
        {title}
      </h3>
      {shown.map((f) => (
        <article key={f.id} className="flex flex-col gap-1 text-sm">
          <h4 style={{ color: 'var(--fg)' }}>
            <span style={{ color: 'var(--grey1)' }}>{f.label}: </span>
            {f.title}
          </h4>
          <p className="whitespace-pre-wrap" style={{ color: 'var(--fg)' }}>
            {f.text}
          </p>
          {f.evidence && (
            <pre className="text-xs whitespace-pre-wrap" style={{ color: 'var(--grey1)' }}>
              {f.evidence}
            </pre>
          )}
          <Outcome finding={f} project={project} prURL={prURL} />
        </article>
      ))}
    </section>
  );
}

/** Sentinel rail tab: how the card's last run stopped, each heal with its diagnosis and change, and the findings. */
export function SentinelTab({ card }: { card: Card }) {
  const view = useCardSentinel(card.project, card.id);
  if (!view) return null;

  const { run, heals, findings, analyses, standing_pr: standingPR } = view;
  if (!run && heals.length === 0 && findings.length === 0 && analyses.length === 0 && !standingPR) {
    return (
      <p className="p-4 text-sm" style={{ color: 'var(--grey1)' }}>
        Sentinel acts only on runs this instance launched.
      </p>
    );
  }

  const stop = run ? lastStop(run) : null;

  return (
    <div className="p-4 flex flex-col gap-4">
      {run && (
        <dl className="flex flex-col gap-2">
          <Fact label="Launched from" value={ORIGINS[run.origin]} />
          <Fact label="Autonomous at launch" value={run.autonomous ? 'Yes' : 'No'} />
          <Fact label="Heal attempts" value={`${run.attempts} of ${run.max_attempts}`} />
          {stop && <Fact label="Last stop" value={stop} />}
        </dl>
      )}
      {heals.map((h) => (
        <Heal key={h.id} heal={h} />
      ))}
      {standingPR && <StandingPR pr={standingPR} />}
      <Findings title="Friction" groups={FRICTION} findings={findings} project={card.project} prURL={standingPR?.url} />
      <Findings title="Notes" groups={NOTES} findings={findings} project={card.project} prURL={standingPR?.url} />
      <Analyses analyses={analyses} />
    </div>
  );
}
