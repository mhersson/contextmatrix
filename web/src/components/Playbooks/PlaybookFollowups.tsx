import { useState, type CSSProperties } from 'react';
import {
  DndContext,
  closestCorners,
  useSensor,
  useSensors,
  PointerSensor,
  TouchSensor,
  type DragEndEvent,
} from '@dnd-kit/core';
import { SortableContext, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { isTouchDevice } from '../../utils/isTouchDevice';
import type { PlaybookFollowup } from '../../types';
import { followupMovePosition } from './playbookUtils';
import { PlaybookManualStep } from './PlaybookManualStep';

interface PlaybookFollowupsProps {
  followups: PlaybookFollowup[];
  /** True while a run is active: ticks are disabled, every other edit stays open. */
  locked: boolean;
  onToggle: (followupId: string, done: boolean) => void;
  onSaveText: (followupId: string, text: string) => void;
  onRemove: (followupId: string) => void;
  onAdd: (text: string) => void;
  onMove: (followupId: string, position: number) => void;
}

const cardLabelStyle: CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontSize: '10px',
  padding: '2px 6px',
  borderRadius: '999px',
  backgroundColor: 'var(--bg1)',
  border: '1px solid var(--bg3)',
  color: 'var(--grey1)',
};

// The id's last segment: RECIPEBO-002 reads as 002; the full ref is the title.
function cardNumber(card: string): string {
  return card.slice(card.lastIndexOf('-') + 1);
}

interface FollowupRowProps {
  followup: PlaybookFollowup;
  locked: boolean;
  onToggle: (followupId: string, done: boolean) => void;
  onSaveText: (followupId: string, text: string) => void;
  onRemove: (followupId: string) => void;
}

function FollowupRow({ followup, locked, onToggle, onSaveText, onRemove }: FollowupRowProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: followup.id });
  return (
    <li
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      className="list-none flex items-start gap-2 py-2 pb-hairline"
      style={{
        transform: CSS.Translate.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
    >
      <div className="flex-1 min-w-0 flex items-start gap-2 flex-wrap" style={{ opacity: followup.done ? 0.55 : 1 }}>
        <PlaybookManualStep
          id={followup.id}
          text={followup.text}
          checked={followup.done}
          disabled={locked}
          onToggleDone={onToggle}
          onSaveText={onSaveText}
        />
        {followup.cards.map((c) => (
          <span key={`${c.project}/${c.card}`} title={`${c.project}/${c.card}`} style={cardLabelStyle}>
            {cardNumber(c.card)}
          </span>
        ))}
      </div>
      <button
        type="button"
        onClick={() => onRemove(followup.id)}
        aria-label="Remove check"
        className="text-sm px-1 shrink-0"
        style={{ color: 'var(--grey1)', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        ×
      </button>
    </li>
  );
}

function AddCheck({ onAdd }: { onAdd: (text: string) => void }) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState('');

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => { setDraft(''); setOpen(true); }}
        className="text-xs py-1"
        style={{ color: 'var(--aqua)', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        Add a check
      </button>
    );
  }
  return (
    <textarea
      autoFocus
      rows={1}
      aria-label="New check"
      value={draft}
      onChange={(e) => setDraft(e.target.value.replace(/[\r\n]+/g, ' '))}
      onBlur={() => {
        setOpen(false);
        const text = draft.trim();
        if (text) onAdd(text);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLTextAreaElement).blur(); }
        else if (e.key === 'Escape') { setDraft(''); setOpen(false); }
      }}
      className="w-full px-1 rounded resize-none overflow-hidden"
      style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
    />
  );
}

/** Checks a person makes after the run; the runner never reads them. */
export function PlaybookFollowups({ followups, locked, onToggle, onSaveText, onRemove, onAdd, onMove }: PlaybookFollowupsProps) {
  const pointerSensor = useSensor(PointerSensor, { activationConstraint: { distance: 5 } });
  const touchSensor = useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } });
  const sensors = useSensors(isTouchDevice() ? touchSensor : pointerSensor);

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over) return;
    const to = followupMovePosition(followups, String(active.id), String(over.id));
    if (to !== null) onMove(String(active.id), to);
  };

  return (
    <section className={`pb-followups mb-4${locked ? ' pb-followups-locked' : ''}`}>
      <h2 className="text-sm font-semibold" style={{ color: 'var(--fg)' }}>How to verify what was built</h2>
      <p className="text-xs mb-1" style={{ color: 'var(--grey1)' }}>
        {locked
          ? 'Shown for what comes after the run; ticking opens when it finishes.'
          : 'Ticks are only your record; they never change the run.'}
      </p>
      {followups.length > 0 && (
        <DndContext sensors={sensors} collisionDetection={closestCorners} onDragEnd={handleDragEnd}>
          <SortableContext items={followups.map((f) => f.id)} strategy={verticalListSortingStrategy}>
            <ul>
              {followups.map((f) => (
                <FollowupRow
                  key={f.id}
                  followup={f}
                  locked={locked}
                  onToggle={onToggle}
                  onSaveText={onSaveText}
                  onRemove={onRemove}
                />
              ))}
            </ul>
          </SortableContext>
        </DndContext>
      )}
      <AddCheck onAdd={onAdd} />
    </section>
  );
}
