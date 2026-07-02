import { useId, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';

interface ModelComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Catalog slugs. Empty = catalog unavailable → plain free-text input. */
  options: string[];
  placeholder?: string;
  disabled?: boolean;
  ariaLabel?: string;
  id?: string;
  className?: string;
  inputStyle?: CSSProperties;
}

/**
 * Strict searchable combobox for model slugs: typing filters the option list,
 * but only a listed option (or the empty string) can be committed — free text
 * never reaches onChange. With an empty option list it degrades to a plain
 * input, mirroring the server's fail-open validation. A committed value that
 * is missing from the catalog (legacy pin, delisted model) is flagged but
 * preserved.
 */
export function ModelCombobox({
  value,
  onChange,
  options,
  placeholder,
  disabled = false,
  ariaLabel,
  id,
  className = 'bf-input font-mono',
  inputStyle,
}: ModelComboboxProps) {
  const listboxId = useId();
  const [draft, setDraft] = useState(value);
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  // In-render sync marker (project convention, see useRailSync): external
  // value changes (favorites chip click) reset the draft synchronously.
  const [prevValue, setPrevValue] = useState(value);
  if (prevValue !== value) {
    setPrevValue(value);
    setDraft(value);
    setHighlight(-1);
  }

  if (options.length === 0) {
    return (
      <input
        id={id}
        type="text"
        aria-label={ariaLabel}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className={className}
        style={inputStyle}
      />
    );
  }

  const q = draft.trim().toLowerCase();
  const filtered = q === '' ? options : options.filter((o) => o.toLowerCase().includes(q));
  const unknown = value !== '' && !options.includes(value);

  function commit(slug: string) {
    setDraft(slug);
    setOpen(false);
    setHighlight(-1);
    if (slug !== value) onChange(slug);
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setOpen(true);
      setHighlight((h) => Math.min(h + 1, filtered.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlight((h) => Math.max(h - 1, 0));
    } else if (e.key === 'Enter') {
      if (open && highlight >= 0 && filtered[highlight]) {
        e.preventDefault();
        commit(filtered[highlight]);
      }
    } else if (e.key === 'Escape') {
      setOpen(false);
      setDraft(value);
      setHighlight(-1);
    }
  }

  function handleBlur() {
    setOpen(false);
    setHighlight(-1);
    const trimmed = draft.trim();
    if (trimmed === '') {
      setDraft('');
      if (value !== '') onChange('');
      return;
    }
    if (options.includes(trimmed)) {
      commit(trimmed);
      return;
    }
    setDraft(value); // strict: never commit free text
  }

  return (
    <div className="relative" style={{ minWidth: 0 }}>
      <input
        id={id}
        type="text"
        role="combobox"
        aria-label={ariaLabel}
        aria-expanded={open}
        aria-controls={open && filtered.length > 0 ? listboxId : undefined}
        aria-autocomplete="list"
        aria-activedescendant={highlight >= 0 ? `${listboxId}-${highlight}` : undefined}
        value={draft}
        disabled={disabled}
        placeholder={placeholder}
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          setDraft(e.target.value);
          setOpen(true);
          setHighlight(-1);
        }}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        className={className}
        style={inputStyle}
      />
      {unknown && (
        <span
          title="Not in the model catalog"
          className="absolute right-1 top-1/2 -translate-y-1/2 text-[10px]"
          style={{ color: 'var(--yellow)' }}
        >
          ⚠
        </span>
      )}
      {open && filtered.length > 0 && (
        <ul
          id={listboxId}
          role="listbox"
          className="absolute z-50 mt-1 w-full overflow-y-auto rounded border font-mono text-[11.5px]"
          style={{
            maxHeight: '240px',
            background: 'var(--bg2)',
            borderColor: 'var(--bg3)',
            color: 'var(--fg)',
          }}
        >
          {filtered.map((slug, i) => (
            <li
              key={slug}
              id={`${listboxId}-${i}`}
              role="option"
              aria-selected={slug === value}
              onMouseDown={(e) => {
                e.preventDefault(); // keep focus so blur does not revert first
                commit(slug);
              }}
              className="cursor-pointer px-2 py-1"
              style={{ background: i === highlight ? 'var(--bg-visual)' : undefined }}
            >
              {slug}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
