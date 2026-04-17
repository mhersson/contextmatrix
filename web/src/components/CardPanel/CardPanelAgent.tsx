import { useState } from 'react';
import type { Card } from '../../types';
import { runnerStatusStyles } from '../../types';
import { formatRelativeTime } from './utils';

interface CardPanelAgentProps {
  card: Card;
  canClaim: boolean;
  canRelease: boolean;
  onClaim: () => void;
  onRelease: () => void;
  canRun: boolean;
  canStop: boolean;
  onRun: (interactive: boolean) => Promise<void>;
  onStop: () => Promise<void>;
}

export function CardPanelAgent({
  card,
  canClaim,
  canRelease,
  onClaim,
  onRelease,
  canRun,
  canStop,
  onRun,
  onStop,
}: CardPanelAgentProps) {
  const [runLoading, setRunLoading] = useState(false);
  const [stopLoading, setStopLoading] = useState(false);
  const [interactive, setInteractive] = useState(!card.autonomous);

  const handleRun = async () => {
    setRunLoading(true);
    try { await onRun(interactive); } finally { setRunLoading(false); }
  };

  const handleStop = async () => {
    if (!window.confirm('Stop this task? The container will be destroyed and uncommitted work discarded.')) return;
    setStopLoading(true);
    try { await onStop(); } finally { setStopLoading(false); }
  };

  return (
    <div className="p-3 rounded bg-[var(--bg0)] border border-[var(--bg3)]">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div>
          <div className="text-xs text-[var(--grey1)] mb-1">Assigned Agent</div>
          {card.assigned_agent ? (
            <div className="flex items-center gap-2">
              <span className="text-sm text-[var(--aqua)]">{card.assigned_agent}</span>
              {card.last_heartbeat && (
                <span className="text-xs text-[var(--grey0)]">
                  · {formatRelativeTime(card.last_heartbeat)}
                </span>
              )}
            </div>
          ) : (
            <span className="text-sm text-[var(--grey0)]">Unassigned</span>
          )}
        </div>
        <div className="flex gap-2">
          {canClaim && (
            <button
              type="button"
              onClick={onClaim}
              className="px-3 py-1.5 rounded bg-[var(--bg-blue)] text-[var(--aqua)] hover:opacity-90 transition-opacity text-sm"
            >
              Claim
            </button>
          )}
          {canRelease && (
            <button
              type="button"
              onClick={onRelease}
              className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm"
            >
              Release
            </button>
          )}
          {canRun && (
            <div className="flex items-center gap-2">
              <label
                className="flex items-center gap-1.5 cursor-pointer"
                title="Start the task in interactive HITL mode. Leave unchecked to run the workflow unattended (autonomous mode)."
              >
                <input
                  type="checkbox"
                  checked={interactive}
                  onChange={(e) => setInteractive(e.target.checked)}
                  className="rounded border-[var(--bg3)] bg-[var(--bg2)] text-[var(--green)] focus:ring-[var(--green)]"
                />
                <span className="text-sm text-[var(--fg)]">Interactive</span>
              </label>
              <button
                type="button"
                onClick={handleRun}
                disabled={runLoading}
                className="px-3 py-1.5 rounded bg-[var(--bg-green)] text-[var(--green)] hover:opacity-90 transition-opacity text-sm disabled:opacity-50"
              >
                {runLoading ? 'Starting...' : 'Run Now'}
              </button>
            </div>
          )}
          {canStop && (
            <button
              type="button"
              onClick={handleStop}
              disabled={stopLoading}
              className="px-3 py-1.5 rounded bg-[var(--bg-red)] text-[var(--red)] hover:opacity-90 transition-opacity text-sm disabled:opacity-50"
            >
              {stopLoading ? 'Stopping...' : 'Stop'}
            </button>
          )}
        </div>
      </div>
      {card.runner_status && runnerStatusStyles[card.runner_status] && (
        <div
          className={`mt-2 px-2 py-1 rounded text-xs${card.runner_status === 'running' ? ' animate-pulse' : ''}`}
          style={{
            backgroundColor: runnerStatusStyles[card.runner_status].bg,
            color: runnerStatusStyles[card.runner_status].text,
          }}
        >
          {runnerStatusStyles[card.runner_status].label}
        </div>
      )}
    </div>
  );
}
