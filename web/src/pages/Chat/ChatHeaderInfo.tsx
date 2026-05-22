import {
  contextPct,
  formatTokens,
  modelMaxTokens,
  useChatModels,
  usageColor,
} from '../../utils/chatModels';

interface ChatHeaderInfoProps {
  /** Model ID stored on the session row. */
  model?: string;
  /** Context-window tokens reported by the most recent Claude turn. */
  contextTokens?: number;
  /** Running estimated cost in USD. Hidden when undefined or 0. */
  estimatedCostUsd?: number;
  /** Cumulative input tokens (for tooltip). */
  promptTokens?: number;
  /** Cumulative output tokens (for tooltip). */
  completionTokens?: number;
  /** Cumulative cache-read tokens (for tooltip). */
  cacheReadTokens?: number;
  /** Cumulative cache-creation tokens (for tooltip). */
  cacheCreationTokens?: number;
}

/**
 * ChatHeaderInfo renders the model label, context-window usage indicator, and
 * (when non-zero) the running estimated cost in the ChatThread header.
 * Delegates the (shared) model-list fetch + token formatting to utils/chatModels
 * so the multi-pane PaneHeader can reuse the same primitives without re-fetching.
 */
export function ChatHeaderInfo({
  model,
  contextTokens,
  estimatedCostUsd,
  promptTokens,
  completionTokens,
  cacheReadTokens,
  cacheCreationTokens,
}: ChatHeaderInfoProps) {
  const models = useChatModels();

  const m = model ? models.find((x) => x.id === model) : undefined;
  const label = m?.label ?? model;
  const max = modelMaxTokens(models, model);

  // No model selected yet → render nothing (e.g. legacy session row that
  // pre-dates the model column).
  if (!label) return null;

  const tokens = contextTokens ?? 0;
  const pct = contextPct(tokens, max);
  const color = usageColor(pct);

  const contextTooltip =
    max > 0
      ? `Context: ${tokens.toLocaleString()} / ${max.toLocaleString()} tokens (${pct}%)`
      : `Context: ${tokens.toLocaleString()} tokens`;

  const showCost =
    estimatedCostUsd !== undefined && estimatedCostUsd > 0;

  const costTooltip = [
    `Input: ${(promptTokens ?? 0).toLocaleString()}`,
    `Output: ${(completionTokens ?? 0).toLocaleString()}`,
    `Cache read: ${(cacheReadTokens ?? 0).toLocaleString()}`,
    `Cache create: ${(cacheCreationTokens ?? 0).toLocaleString()}`,
  ].join('\n');

  return (
    <>
      <span
        className="text-xs px-2 py-0.5 rounded"
        style={{ backgroundColor: 'var(--bg2)', color: 'var(--grey2)' }}
      >
        {label}
      </span>
      <span
        className="text-xs font-mono"
        style={{ color }}
        title={contextTooltip}
      >
        {max > 0
          ? `${formatTokens(tokens)} / ${formatTokens(max)} (${pct}%)`
          : formatTokens(tokens)}
      </span>
      {showCost && (
        <span
          className="text-xs font-mono"
          style={{ color: 'var(--grey1)' }}
          title={costTooltip}
        >
          ${estimatedCostUsd!.toFixed(2)}
        </span>
      )}
    </>
  );
}
