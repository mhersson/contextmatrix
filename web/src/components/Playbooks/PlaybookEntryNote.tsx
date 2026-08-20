import { useState } from 'react';

interface PlaybookEntryNoteProps {
  note?: string;
  onSave: (value: string) => void;
}

// Inline-editable note in the display serif voice - italic, muted, prefixed
// with "›". Click swaps to a textarea; Enter (without shift) or blur saves,
// Escape cancels without writing.
export function PlaybookEntryNote({ note, onSave }: PlaybookEntryNoteProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(note ?? '');

  if (editing) {
    return (
      <textarea
        autoFocus
        rows={2}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => { setEditing(false); onSave(draft.trim()); }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            (e.target as HTMLTextAreaElement).blur();
          } else if (e.key === 'Escape') {
            setDraft(note ?? '');
            setEditing(false);
          }
        }}
        className="block w-full mt-1 rounded px-2 py-1 text-sm"
        style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
      />
    );
  }

  return (
    <button
      type="button"
      onClick={() => setEditing(true)}
      className="block mt-1 text-left"
      style={{
        fontFamily: 'var(--font-display)',
        fontStyle: 'italic',
        color: 'var(--grey2)',
        background: 'none',
        border: 'none',
        padding: 0,
        cursor: 'pointer',
      }}
    >
      {note ? `› ${note}` : '+ add note'}
    </button>
  );
}
