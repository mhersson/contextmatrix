import { Fragment } from 'react';
import type { Card, UsageBucket } from '../../../types';
import { formatCost, formatTokens } from '../../../lib/format';
import {
  SUBSCRIPTION_TOOLTIP,
  cardTotalText,
  isSubscriptionBucket,
  markTitle,
  subscriptionUSD,
} from '../../../lib/subscription';
import { groupBucketsByRole } from '../utils';

interface MetadataUsageProps {
  card: Card;
  onSubtaskClick?: (cardId: string) => void;
}

/** Role hues follow the board's state chips: review is yellow, execute is blue. */
const ROLE_COLOR: Record<string, string> = {
  review: 'var(--yellow)',
  execute: 'var(--blue)',
  plan: 'var(--purple)',
  judge: 'var(--orange)',
  document: 'var(--aqua)',
  gates: 'var(--green)',
};
const OTHER_COLOR = 'var(--grey1)';

const STEP_WORD: Record<string, string> = {
  mob_seat: 'seat',
  mob_moderator: 'moderator',
  gate: 'gate',
  judge: 'judge',
  checkpoint: 'checkpoint',
  brainstorm: 'brainstorm',
  verify_propose: 'verify',
};

const GRID =
  'grid grid-cols-[minmax(0,1fr)_auto_auto] items-baseline gap-x-3 gap-y-1 text-[12px] text-[var(--fg)]';

interface Segment {
  key: string;
  word: string;
  color: string;
  cost: number;
  hatch: boolean;
  /** '≈' over subscription spend, '*' over rate-table estimates, else ''. */
  mark: string;
}

function roleWord(role: string): string {
  return role || 'other';
}

function roleColor(role: string): string {
  return ROLE_COLOR[role] ?? OTHER_COLOR;
}

function stepWord(step: string): string {
  return STEP_WORD[step] ?? step;
}

function bucketsCost(buckets: UsageBucket[]): number {
  return buckets.reduce((sum, b) => sum + b.cost_usd, 0);
}

function bucketsEstimated(buckets: UsageBucket[]): boolean {
  return buckets.some((b) => b.cost_source === 'estimated');
}

/** Rate-table estimates only: subscription buckets are estimated too, but carry their own mark. */
function meteredEstimated(buckets: UsageBucket[]): boolean {
  return bucketsEstimated(buckets.filter((b) => !isSubscriptionBucket(b)));
}

function costMark(buckets: UsageBucket[]): string {
  if (buckets.some(isSubscriptionBucket)) return '≈';
  return bucketsEstimated(buckets) ? '*' : '';
}

function share(cost: number, total: number): string {
  const pct = (cost / total) * 100;
  return pct < 0.5 ? '<1%' : `${Math.round(pct)}%`;
}

function BucketRow({ bucket: b, indent = false }: { bucket: UsageBucket; indent?: boolean }) {
  const tokens = b.prompt_tokens + b.completion_tokens;
  const model = b.model || '(unknown)';
  const slash = model.indexOf('/');
  return (
    <>
      <span className={`font-mono truncate${indent ? ' pl-[13px]' : ''}`} title={model}>
        {slash > 0 ? (
          <>
            <span className="text-[var(--grey0)]">{model.slice(0, slash + 1)}</span>
            {model.slice(slash + 1)}
          </>
        ) : (
          model
        )}
      </span>
      <span
        className="text-right tabular-nums text-[var(--grey1)]"
        title={`${tokens.toLocaleString()} tokens`}
      >
        {formatTokens(tokens)}
      </span>
      {isSubscriptionBucket(b) ? (
        <span className="text-right tabular-nums" title={SUBSCRIPTION_TOOLTIP}>
          {formatCost(b.cost_usd)}≈
        </span>
      ) : (
        <span
          className="text-right tabular-nums"
          title={`${
            b.counts_source === 'collector' ? 'measured (collector-reported)' : 'agent-reported'
          } · ${b.cost_source === 'actual' ? 'actual provider cost' : 'estimated from rate table'}`}
        >
          {formatCost(b.cost_usd)}
          {b.cost_source === 'estimated' ? '*' : ''}
        </span>
      )}
    </>
  );
}

/**
 * Info-rail section answering "how much did this card cost, and where did it
 * go": the total including subtasks, a cost bar and legend split by role,
 * this card's buckets grouped under role headers, then one block per costed
 * subtask. A header shows a subtotal only over two or more rows; a single
 * row already carries the amount in the same column. Color means role and
 * nothing else. Renders nothing when there is neither a breakdown nor
 * subtask spend.
 */
export function MetadataUsage({ card, onSubtaskClick }: MetadataUsageProps) {
  const buckets = card.usage_breakdown ?? [];
  const ownCost = card.token_usage?.estimated_cost_usd ?? bucketsCost(buckets);
  const subtaskCost = card.subtask_cost_usd ?? 0;
  const total = ownCost + subtaskCost;
  if (buckets.length === 0 && subtaskCost === 0) {
    return null;
  }

  const subtasks = card.subtask_usage ?? [];
  const subtaskBuckets = subtasks.flatMap((s) => s.buckets);
  const subtaskSubscription = subtaskCost > 0 ? subscriptionUSD(subtaskBuckets) : 0;
  const subscription = subscriptionUSD(buckets) + subtaskSubscription;

  // The server's subtask flag counts subscription buckets as estimates; the
  // buckets themselves tell the two apart when any subtask ran on the subscription.
  const subtaskMetered =
    subtaskSubscription > 0 ? meteredEstimated(subtaskBuckets) : (card.subtask_cost_has_estimates ?? false);
  const hasEstimates =
    meteredEstimated(buckets) ||
    (buckets.length === 0 && ownCost > 0) ||
    (subtaskCost > 0 && subtaskMetered);
  const totalText = cardTotalText(total, subscription, formatCost, hasEstimates ? '*' : '');

  const groups = groupBucketsByRole(buckets);
  const segments: Segment[] = groups.map((g) => ({
    key: g.role || 'other',
    word: roleWord(g.role),
    color: roleColor(g.role),
    cost: g.cost,
    hatch: false,
    mark: costMark(g.buckets),
  }));
  if (buckets.length === 0 && ownCost > 0) {
    segments.push({
      key: 'other',
      word: 'other',
      color: OTHER_COLOR,
      cost: ownCost,
      hatch: false,
      mark: '*',
    });
  }
  if (subtaskCost > 0) {
    segments.push({
      key: 'subtasks',
      word: 'subtasks',
      color: OTHER_COLOR,
      cost: subtaskCost,
      hatch: true,
      mark: subtaskSubscription > 0 ? '≈' : (card.subtask_cost_has_estimates ?? false) ? '*' : '',
    });
  }
  const showSplit = total > 0 && segments.length > 1;
  const totalLabel = `Total${subtaskCost > 0 ? ' incl. subtasks' : ''}`;

  return (
    <section className="bf-aside-section">
      <h4>Models used</h4>
      {totalText === null ? (
        <div className={GRID}>
          <span className="truncate font-semibold">{totalLabel}</span>
          <span aria-hidden="true" />
          <span
            className="text-right tabular-nums font-semibold"
            title={hasEstimates ? 'includes costs estimated from the rate table' : undefined}
          >
            {formatCost(total)}
            {hasEstimates ? '*' : ''}
          </span>
        </div>
      ) : (
        // The labelled total is too wide for the amount column; it wraps under the label.
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 text-[12px] text-[var(--fg)]">
          <span className="font-semibold">{totalLabel}</span>
          <span
            className="ml-auto text-right tabular-nums font-semibold"
            title={
              hasEstimates
                ? `${SUBSCRIPTION_TOOLTIP}\n* includes costs estimated from the rate table`
                : SUBSCRIPTION_TOOLTIP
            }
          >
            {totalText}
          </span>
        </div>
      )}

      {showSplit && (
        <>
          <div className="bf-usage-bar" role="img" aria-label="cost split by role">
            {segments.map((s) => (
              <span
                key={s.key}
                className={`bf-usage-seg${s.hatch ? ' bf-usage-seg--hatch' : ''}`}
                style={{ flex: `${s.cost / total} 0 0`, backgroundColor: s.hatch ? undefined : s.color }}
                title={[`${s.word} ${formatCost(s.cost)}${s.mark} (${share(s.cost, total)})`, markTitle(s.mark)]
                  .filter(Boolean)
                  .join('\n')}
              />
            ))}
          </div>
          <div className="bf-usage-legend">
            {segments.map((s) => (
              <span key={s.key} className="bf-usage-legend-item">
                <span
                  className={`bf-usage-swatch${s.hatch ? ' bf-usage-swatch--hatch' : ''}`}
                  style={{ backgroundColor: s.hatch ? undefined : s.color }}
                  aria-hidden="true"
                />
                <span className="text-[var(--grey2)]">{s.word}</span>
                <span className="tabular-nums" title={markTitle(s.mark)}>
                  {formatCost(s.cost)}
                  {s.mark}
                </span>
              </span>
            ))}
          </div>
        </>
      )}

      {buckets.length > 0 && (
        <>
          {subtaskCost > 0 && (
            <div className="bf-usage-card bf-usage-card--own">
              <span className="bf-usage-id">{card.id}</span>
              <span className="bf-usage-kind">this card</span>
              <span className="bf-usage-amt" title={markTitle(costMark(buckets))}>
                {formatCost(ownCost)}
                {costMark(buckets)}
              </span>
            </div>
          )}
          <div className={GRID}>
            {groups.map((g) => {
              // "judge judge" reads as a stutter: a step named like its role adds nothing.
              const steps = g.steps.map(stepWord).filter((w) => w !== roleWord(g.role));
              return (
                <Fragment key={g.role || 'other'}>
                  <div className="bf-usage-role">
                    <span
                      className="bf-usage-swatch"
                      style={{ backgroundColor: roleColor(g.role) }}
                      aria-hidden="true"
                    />
                    <span
                      className="text-[var(--grey2)]"
                      title={
                        g.role
                          ? undefined
                          : 'spend with no role: reported before roles existed or without a phase'
                      }
                    >
                      {roleWord(g.role)}
                    </span>
                    {steps.length > 0 && <span className="bf-usage-steps">{steps.join(', ')}</span>}
                    {g.buckets.length > 1 && (
                      <span className="bf-usage-amt" title={markTitle(costMark(g.buckets))}>
                        {formatCost(g.cost)}
                        {costMark(g.buckets)}
                      </span>
                    )}
                  </div>
                  {g.buckets.map((b, i) => (
                    <BucketRow key={`${b.model}:${i}`} bucket={b} indent />
                  ))}
                </Fragment>
              );
            })}
          </div>
        </>
      )}

      {subtasks.map((s) => (
        <Fragment key={s.card_id}>
          <div className="bf-usage-card">
            {onSubtaskClick ? (
              <button
                type="button"
                className="bf-usage-id bf-usage-link"
                onClick={() => onSubtaskClick(s.card_id)}
              >
                {s.card_id}
              </button>
            ) : (
              <span className="bf-usage-id">{s.card_id}</span>
            )}
            <span className="bf-usage-kind">subtask</span>
            {s.buckets.length > 1 && (
              <span className="bf-usage-amt" title={markTitle(costMark(s.buckets))}>
                {formatCost(bucketsCost(s.buckets))}
                {costMark(s.buckets)}
              </span>
            )}
          </div>
          <div className={GRID}>
            {s.buckets.map((b, i) => (
              <BucketRow key={`${b.model}:${i}`} bucket={b} />
            ))}
          </div>
        </Fragment>
      ))}
    </section>
  );
}
