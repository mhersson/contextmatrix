import { useLayoutEffect, useRef, useState } from 'react';
interface PlaybookManualStepProps {
  id: string;
  text: string;
  checked: boolean;
  disabled?: boolean;
  onToggleDone: (id: string, done: boolean) => void;
  onSaveText: (id: string, text: string) => void;
}

// A step is one logical line: Enter saves instead of inserting a newline, and
// pasted line breaks become spaces.
export function PlaybookManualStep({ id, text, checked, disabled = false, onToggleDone, onSaveText }: PlaybookManualStepProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(text);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    // scrollHeight excludes the border; add it back so border-box sizing fits the text.
    el.style.height = `${el.scrollHeight + el.offsetHeight - el.clientHeight}px`;
  }, [editing, draft]);

  return (
    <div className={`flex items-start gap-2 min-w-0${editing ? ' w-full' : ''}`}>
      <span className="flex items-center h-lh shrink-0">
        <input
          type="checkbox"
          checked={checked}
          disabled={disabled}
          aria-label={text}
          onChange={(e) => onToggleDone(id, e.target.checked)}
          style={{ accentColor: 'var(--green)' }}
        />
      </span>
      {editing ? (
        <textarea
          ref={textareaRef}
          autoFocus
          rows={1}
          aria-label="Step text"
          value={draft}
          onChange={(e) => setDraft(e.target.value.replace(/[\r\n]+/g, ' '))}
          // The whole row is the drag handle; keep presses here so text can be selected.
          onPointerDown={(e) => e.stopPropagation()}
          onTouchStart={(e) => e.stopPropagation()}
          onBlur={() => {
            setEditing(false);
            const next = draft.trim();
            if (next && next !== text) onSaveText(id, next);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLTextAreaElement).blur(); }
            else if (e.key === 'Escape') setEditing(false);
          }}
          className="flex-1 min-w-0 px-1 rounded resize-none overflow-hidden"
          style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
        />
      ) : (
        <button
          type="button"
          // Seed from the live text: an SSE refresh may have changed it since the last edit.
          onClick={() => { setDraft(text); setEditing(true); }}
          className="min-w-0 text-left wrap-anywhere"
          style={{ color: 'var(--fg)', background: 'none', border: 'none', padding: 0, cursor: 'text' }}
        >
          {text}
        </button>
      )}
    </div>
  );
}
