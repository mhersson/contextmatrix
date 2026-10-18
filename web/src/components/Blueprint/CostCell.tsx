import { useCallback, useRef, useState, type CSSProperties } from 'react';
import { useMenuDismiss } from '../../hooks/useMenuDismiss';
import type { BlueprintCost } from '../../types';
import { CapPopover } from './CapPopover';
import { costLabel, money } from './stages';

export function CostCell({ chatId, cost: raw }: { chatId: string; cost: BlueprintCost }) {
  const cost = costLabel(raw);
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useMenuDismiss(wrapRef, open, close, triggerRef);
  const hasCap = raw.cap_usd > 0;
  const name = hasCap
    ? `Cost: ${money(raw.spent_usd)} of ${money(raw.cap_usd)} cap, change cap`
    : `Cost: ${money(raw.spent_usd)}, no cap, set a cap`;
  return (
    <div
      ref={wrapRef}
      className="bp-cost-wrap"
      // Marks Escape handled so an enclosing overlay does not also close; useMenuDismiss closes the popover.
      onKeyDown={(e) => {
        if (open && e.key === 'Escape') e.preventDefault();
      }}
    >
      <button
        ref={triggerRef}
        type="button"
        className={`bp-cost bp-cost--${cost.tone}`}
        aria-label={name}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span>{cost.text}</span>
        {cost.percent !== null && (
          <i aria-hidden="true" style={{ '--bp-cost-pct': `${cost.percent}%` } as CSSProperties} />
        )}
      </button>
      {open && (
        <CapPopover
          chatId={chatId}
          cost={raw}
          onClose={() => {
            triggerRef.current?.focus();
            setOpen(false);
          }}
        />
      )}
    </div>
  );
}
