import { useEffect, useId, useRef, useState } from 'react';
import { api, describeApiError } from '../../api/client';
import { notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { BLUEPRINT_MODES, type BlueprintMode } from '../../pages/Chat/blueprintForm';
import type { BlueprintView } from '../../types';
import { isBlueprintMode, switchNote } from './mode';

interface ModePopoverProps {
  chatId: string;
  view: BlueprintView;
  onClose: () => void;
}

/** The shown mode is always the view's: a switch posts the command and the architect's republish moves the mode. */
export function ModePopover({ chatId, view, onClose }: ModePopoverProps) {
  const current = isBlueprintMode(view.mode) ? view.mode : null;
  const [picked, setPicked] = useState<BlueprintMode | null>(current);
  const [base, setBase] = useState(current);
  // Follow a republished mode until the user picks something else.
  if (base !== current) {
    setBase(current);
    if (picked === base) setPicked(current);
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const group = useId();

  useEffect(() => {
    const form = formRef.current;
    (form?.querySelector<HTMLInputElement>('input:checked') ?? form?.querySelector<HTMLInputElement>('input'))?.focus();
  }, []);

  const next = picked !== null && picked !== current ? picked : null;
  const yolo = next === 'yolo';
  const note = next ? switchNote(view, next) : null;

  const save = async () => {
    if (next === null || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.setChatMode(chatId, next);
      notifyChatSessionsChanged();
      onClose();
    } catch (err) {
      setError(describeApiError(err, 'Could not switch the mode.'));
      setBusy(false);
    }
  };

  return (
    <form
      ref={formRef}
      className="bp-cap bp-cap--modes"
      role="dialog"
      aria-label="Blueprint mode"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <div className="bp-mode-options" role="radiogroup" aria-label="Mode">
        {BLUEPRINT_MODES.map((m) => {
          const on = m.value === picked;
          const descId = `${group}-${m.value}`;
          return (
            <label
              key={m.value}
              className={`bp-mode-option${on ? ' bp-mode-option--on' : ''}${m.value === 'yolo' ? ' bp-mode-option--yolo' : ''}`}
            >
              <input
                type="radio"
                className="sr-only"
                name={group}
                checked={on}
                aria-label={m.label}
                aria-describedby={descId}
                onChange={() => {
                  setPicked(m.value);
                  setError(null);
                }}
              />
              <b>
                {m.label}
                {m.value === current && <small>Current</small>}
              </b>
              <span id={descId}>{m.description}</span>
            </label>
          );
        })}
      </div>
      {note && (
        <p className="bp-mode-note" role="note">
          {note}
        </p>
      )}
      {error && (
        <p className="bp-cap-error" role="alert">
          {error}
        </p>
      )}
      <div className="bp-cap-actions">
        <button type="button" onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className={yolo ? 'bp-mode-save--yolo' : undefined} disabled={next === null || busy}>
          {busy ? 'Switching...' : yolo ? 'Switch and run' : 'Switch'}
        </button>
      </div>
    </form>
  );
}
