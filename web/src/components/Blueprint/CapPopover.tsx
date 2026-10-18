import { useEffect, useId, useRef, useState } from 'react';
import { api } from '../../api/client';
import type { BlueprintCost } from '../../types';
import { money } from './stages';

const RAISES = [2, 5, 10];

const toCents = (usd: number) => Math.round(usd * 100);

interface CapPopoverProps {
  chatId: string;
  cost: BlueprintCost;
  onClose: () => void;
}

/** The shown cap is always the view's: a save posts the command and the architect's republish moves the cap. */
export function CapPopover({ chatId, cost, onClose }: CapPopoverProps) {
  const hasCap = cost.cap_usd > 0;
  const [amount, setAmount] = useState(hasCap ? cost.cap_usd.toFixed(2) : '');
  const [noCap, setNoCap] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const hintId = useId();

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const trimmed = amount.trim();
  const parsed = /^\d+(\.\d{1,2})?$|^\.\d{1,2}$/.test(trimmed) ? Number(trimmed) : null;
  let reason: string | null = null;
  if (!noCap && trimmed !== '') {
    if (parsed === null || toCents(parsed) <= 0) reason = 'Enter an amount in dollars, like 4.00.';
    else if (toCents(parsed) <= toCents(cost.spent_usd)) reason = `The cap must be above the ${money(cost.spent_usd)} spent.`;
  }
  const valid = noCap || (trimmed !== '' && reason === null);

  const raise = (by: number) => {
    setAmount(((hasCap ? cost.cap_usd : cost.spent_usd) + by).toFixed(2));
    setNoCap(false);
    setError(null);
    inputRef.current?.focus();
  };

  const save = async () => {
    if (!valid || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.setChatCostCap(chatId, noCap || parsed === null ? 0 : toCents(parsed) / 100);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not change the cap.');
      setBusy(false);
    }
  };

  return (
    <form
      className="bp-cap"
      role="dialog"
      aria-label="Cost cap"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <b>{money(cost.spent_usd)} spent</b>
      <small>{hasCap ? `Cap ${money(cost.cap_usd)}` : 'No cap'}</small>
      <div className="bp-cap-raises" role="group" aria-label="Raise the cap">
        {RAISES.map((r) => (
          <button key={r} type="button" aria-label={`Raise by $${r}`} onClick={() => raise(r)}>
            +${r}
          </button>
        ))}
      </div>
      <label className="bp-cap-amount">
        <span>Cap in USD</span>
        <input
          ref={inputRef}
          type="text"
          inputMode="decimal"
          value={amount}
          disabled={noCap}
          aria-invalid={reason !== null}
          aria-describedby={hintId}
          onChange={(e) => setAmount(e.target.value)}
        />
      </label>
      <label className="bp-cap-none">
        <input type="checkbox" checked={noCap} onChange={(e) => setNoCap(e.target.checked)} />
        No cap
      </label>
      <small id={hintId} className="bp-cap-reason">
        {reason}
      </small>
      {error && (
        <p className="bp-cap-error" role="alert">
          {error}
        </p>
      )}
      <div className="bp-cap-actions">
        <button type="button" onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className="bp-cap-save" disabled={!valid || busy}>
          {busy ? 'Saving...' : 'Save'}
        </button>
      </div>
    </form>
  );
}
