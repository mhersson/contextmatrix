import { useLayoutEffect, useRef, useState } from 'react';
import type { PlaybookEntry } from '../../types';

interface PlaybookManualStepProps {
  entry: PlaybookEntry;
  onToggleDone: (entryId: string, done: boolean) => void;
  onSaveText: (entryId: string, text: string) => void;
}

// A step is one logical line: Enter saves instead of inserting a newline, and
// pasted line breaks become spaces.
export function PlaybookManualStep({ entry, onToggleDone, onSaveText }: PlaybookManualStepProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(entry.text ?? '');
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
          checked={entry.complete}
          aria-label={entry.text}
          onChange={(e) => onToggleDone(entry.id, e.target.checked)}
          style={{ accentColor: 'var(--green)' }}
        />
      </span>
      {editing ? (
        <textarea
          ref={textareaRef}
          autoFocus
          rows={1}
          value={draft}
          onChange={(e) => setDraft(e.target.value.replace(/[\r\n]+/g, ' '))}
          // The whole row is the drag handle; keep presses here so text can be selected.
          onPointerDown={(e) => e.stopPropagation()}
          onTouchStart={(e) => e.stopPropagation()}
          onBlur={() => { setEditing(false); onSaveText(entry.id, draft.trim()); }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLTextAreaElement).blur(); }
            else if (e.key === 'Escape') { setDraft(entry.text ?? ''); setEditing(false); }
          }}
          className="flex-1 min-w-0 px-1 rounded resize-none overflow-hidden"
          style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
        />
      ) : (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="min-w-0 text-left wrap-anywhere"
          style={{ color: 'var(--fg)', background: 'none', border: 'none', padding: 0, cursor: 'text' }}
        >
          {entry.text}
        </button>
      )}
    </div>
  );
}
