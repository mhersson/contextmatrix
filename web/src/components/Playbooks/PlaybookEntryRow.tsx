import { useState, type CSSProperties } from 'react';
import { Link } from 'react-router';
import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import type { PlaybookEntry } from '../../types';
import { entryStateChip, projectColor } from './playbookUtils';
import { EntryNode } from './EntryNode';
import { PlaybookEntryNote } from './PlaybookEntryNote';

export interface PlaybookEntryRowViewProps {
  entry: PlaybookEntry;
  index: number;
  isFrontier: boolean;
  onToggleDone: (entryId: string, done: boolean) => void;
  onSaveNote: (entryId: string, note: string) => void;
  onSaveText: (entryId: string, text: string) => void;
  onRemove: (entryId: string) => void;
}

const chipStyle: CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontSize: '10px',
  padding: '2px 8px',
  borderRadius: '999px',
  fontWeight: 500,
};

/** Pure row body - no dnd context required, so it renders directly in tests. */
export function PlaybookEntryRowView({
  entry, index, isFrontier, onToggleDone, onSaveNote, onSaveText, onRemove,
}: PlaybookEntryRowViewProps) {
  const [editingText, setEditingText] = useState(false);
  const [textDraft, setTextDraft] = useState(entry.text ?? '');
  const chip = entryStateChip(entry);

  return (
    <div className="flex items-start gap-3 py-2.5" style={{ opacity: entry.complete ? 0.55 : 1 }}>
      <span className="w-4 flex justify-center pt-0.5 shrink-0">
        {isFrontier && <span aria-label="next up" style={{ color: 'var(--purple)' }}>▶</span>}
      </span>

      <EntryNode entry={entry} index={index} />

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          {entry.type === 'card' ? (
            <Link
              to={`/projects/${entry.project}?card=${entry.card}`}
              className="flex items-center gap-2 min-w-0"
              style={{ textDecoration: 'none' }}
            >
              <span
                className="font-mono text-[10px] px-1.5 py-0.5 rounded"
                style={{ backgroundColor: 'var(--bg1)', border: '1px solid var(--bg3)', color: projectColor(entry.project ?? '') }}
              >
                {entry.project}
              </span>
              <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--grey1)', fontSize: '11px' }}>
                {entry.card}
              </span>
              <span className="truncate" style={{ color: 'var(--fg)' }}>
                {entry.card_title ?? '(unknown card)'}
              </span>
            </Link>
          ) : (
            <div className="flex items-center gap-2 min-w-0">
              <input
                type="checkbox"
                checked={entry.complete}
                aria-label={entry.text}
                onChange={(e) => onToggleDone(entry.id, e.target.checked)}
                style={{ accentColor: 'var(--green)' }}
              />
              {editingText ? (
                <input
                  autoFocus
                  value={textDraft}
                  onChange={(e) => setTextDraft(e.target.value)}
                  onBlur={() => { setEditingText(false); onSaveText(entry.id, textDraft.trim()); }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLInputElement).blur(); }
                    else if (e.key === 'Escape') { setTextDraft(entry.text ?? ''); setEditingText(false); }
                  }}
                  className="px-1 rounded text-sm"
                  style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
                />
              ) : (
                <button
                  type="button"
                  onClick={() => setEditingText(true)}
                  className="truncate text-left"
                  style={{ color: 'var(--fg)', background: 'none', border: 'none', padding: 0, cursor: 'text' }}
                >
                  {entry.text}
                </button>
              )}
            </div>
          )}

          {chip && (
            <span style={{ ...chipStyle, backgroundColor: chip.bg, color: chip.color }}>
              {chip.label}
            </span>
          )}

          {entry.type === 'manual' && entry.complete && (entry.done_by || entry.done_at) && (
            <span className="text-[10px]" style={{ color: 'var(--grey0)' }}>
              {entry.done_by}{entry.done_by && entry.done_at ? ' · ' : ''}{entry.done_at}
            </span>
          )}
        </div>

        <PlaybookEntryNote note={entry.note} onSave={(value) => onSaveNote(entry.id, value)} />
      </div>

      <button
        type="button"
        onClick={() => onRemove(entry.id)}
        aria-label="Remove entry"
        className="text-sm px-1 shrink-0"
        style={{ color: 'var(--grey1)', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        ×
      </button>
    </div>
  );
}

/** Sortable wrapper - drag handle spans the whole row, matching CardItem's idiom. */
export function PlaybookEntryRow(props: PlaybookEntryRowViewProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: props.entry.id });
  return (
    <li
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      className="list-none"
      style={{
        transform: CSS.Translate.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
    >
      <PlaybookEntryRowView {...props} />
    </li>
  );
}
