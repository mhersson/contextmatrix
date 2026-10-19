import { useId } from 'react';
import { POC_EXPLANATION } from '../../lib/poc';

export interface PocSectionProps {
  value: boolean;
  onChange: (next: boolean) => void;
}

export function PocSection({ value, onChange }: PocSectionProps) {
  const descId = useId();
  return (
    <div className="bf-auto-stack">
      <label className="bf-switch">
        <input
          type="checkbox"
          checked={value}
          aria-describedby={descId}
          onChange={(e) => onChange(e.target.checked)}
        />
        <span>Proof of concept project</span>
      </label>
      <p id={descId} className="ps-lead">
        {POC_EXPLANATION}
      </p>
    </div>
  );
}
