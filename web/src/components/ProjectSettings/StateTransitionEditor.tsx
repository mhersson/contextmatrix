import type { CSSProperties } from 'react';

export interface StateTransitionEditorProps {
  states: string[];
  transitions: Record<string, string[]>;
  onChange: (next: Record<string, string[]>) => void;
  inputStyle: CSSProperties;
}

export function StateTransitionEditor({
  states,
  transitions,
  onChange,
}: StateTransitionEditorProps) {
  const toggle = (from: string, to: string) => {
    const current = transitions[from] || [];
    const next = current.includes(to)
      ? current.filter((s) => s !== to)
      : [...current, to];
    onChange({ ...transitions, [from]: next });
  };

  return (
    <div>
      <div className="block text-xs mb-2" style={{ color: 'var(--grey1)' }}>
        Transitions
      </div>
      <div className="space-y-2">
        {states.map((from) => (
          <div key={from} className="p-3 rounded" style={{ backgroundColor: 'var(--bg1)' }}>
            <div className="text-xs font-medium mb-1.5" style={{ color: 'var(--fg)' }}>
              {from}
            </div>
            <div className="flex flex-wrap gap-1.5">
              {states
                .filter((s) => s !== from)
                .map((to) => (
                  <button
                    key={to}
                    onClick={() => toggle(from, to)}
                    className="px-2 py-0.5 rounded text-xs transition-colors"
                    style={{
                      backgroundColor: (transitions[from] || []).includes(to)
                        ? 'var(--bg-green)'
                        : 'var(--bg2)',
                      color: (transitions[from] || []).includes(to)
                        ? 'var(--green)'
                        : 'var(--grey1)',
                    }}
                  >
                    {to}
                  </button>
                ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
