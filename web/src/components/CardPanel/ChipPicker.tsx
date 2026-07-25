import { HeaderCaret } from '../../lib/header-tokens';
import { chipTint } from '../../lib/chip';

// Shared inline styles for the select element inside a chip-pill picker.
// letter-spacing must stay 'normal': browsers size a <select> from its option
// text WITHOUT letter-spacing, so any tracking (including the 0.02em inherited
// from .chip-pill) renders wider than the measured box and clips the last
// glyph of the widest option (e.g. "Unassigned").
const chipSelectBaseStyle = {
  fontFamily: 'var(--font-mono)',
  fontSize: '11px',
  letterSpacing: 'normal',
} as const;

export interface ChipPickerProps {
  id: string;
  value: string;
  options: string[];
  tint: string;
  ariaLabel: string;
  onChange: (v: string) => void;
  /** When true, collapses the caret and padding, shows not-allowed cursor. */
  disabled?: boolean;
  /** Tooltip on the outer chip div. */
  title?: string;
  /**
   * Override the chip background color. When absent, `chipTint(tint)` is used
   * (22% color-mix). Pass a CSS value (e.g. `'var(--bg-blue)'`) for cases
   * that need a solid rather than tinted background (subtask chips).
   */
  solidBg?: string;
  /** Override the displayed text per option value; falls back to the raw value. */
  optionLabels?: Record<string, string>;
}

/**
 * A select element rendered inside a chip-pill div with a trailing caret.
 * Shared by CreateCardPanel and CardPanelHeaderChips.
 *
 * - When `disabled` is false (default), the select is interactive: full
 *   padding, visible caret, pointer cursor.
 * - When `disabled` is true, the select is inert: collapsed padding, hidden
 *   caret, not-allowed cursor.
 * - `solidBg` overrides the tinted background for special chips (e.g. subtask
 *   uses `var(--bg-blue)` instead of a 22% color-mix).
 */
export function ChipPicker({
  id,
  value,
  options,
  tint,
  ariaLabel,
  onChange,
  disabled = false,
  title,
  solidBg,
  optionLabels,
}: ChipPickerProps) {
  const containerStyle = solidBg
    ? { backgroundColor: solidBg, color: tint, padding: '3px 4px 3px 8px', gap: '2px' }
    : { ...chipTint(tint), padding: '3px 4px 3px 8px', gap: '2px' };

  const selectStyle = {
    ...chipSelectBaseStyle,
    color: tint,
    paddingRight: disabled ? '0' : '14px',
    marginRight: disabled ? '0' : '-12px',
    cursor: disabled ? 'not-allowed' : 'pointer',
  };

  return (
    <div className="chip-pill" style={containerStyle} title={title}>
      <select
        id={id}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="bg-transparent border-none outline-none appearance-none"
        style={selectStyle}
        aria-label={ariaLabel}
      >
        {options.map((o) => (
          <option key={o} value={o} className="bg-[var(--bg2)] text-[var(--fg)]">
            {optionLabels?.[o] ?? o}
          </option>
        ))}
      </select>
      {!disabled && <span className="pointer-events-none">{HeaderCaret}</span>}
    </div>
  );
}
