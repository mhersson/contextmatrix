import {
  DndContext,
  closestCorners,
  useSensor,
  useSensors,
  PointerSensor,
  TouchSensor,
  type DragEndEvent,
} from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { isTouchDevice } from '../../utils/isTouchDevice';
import type { PlaybookEntry } from '../../types';
import { frontierIndex } from './playbookUtils';
import { PlaybookEntryRow } from './PlaybookEntryRow';

interface PlaybookEntryListProps {
  entries: PlaybookEntry[];
  onDragEnd: (event: DragEndEvent) => void;
  onToggleDone: (entryId: string, done: boolean) => void;
  onSaveNote: (entryId: string, note: string) => void;
  onSaveText: (entryId: string, text: string) => void;
  onRemove: (entryId: string) => void;
}

/** Draggable entry list - sensors copied from Board.tsx per web/AGENTS.md. */
export function PlaybookEntryList({
  entries, onDragEnd, onToggleDone, onSaveNote, onSaveText, onRemove,
}: PlaybookEntryListProps) {
  const pointerSensor = useSensor(PointerSensor, { activationConstraint: { distance: 5 } });
  const touchSensor = useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } });
  const sensors = useSensors(isTouchDevice() ? touchSensor : pointerSensor);

  const frontier = frontierIndex(entries);
  const entryIds = entries.map((e) => e.id);

  return (
    <DndContext sensors={sensors} collisionDetection={closestCorners} onDragEnd={onDragEnd}>
      <SortableContext items={entryIds} strategy={verticalListSortingStrategy}>
        <ul className="mb-4">
          {entries.map((entry, index) => (
            <PlaybookEntryRow
              key={entry.id}
              entry={entry}
              index={index}
              isFrontier={index === frontier}
              prevComplete={index > 0 ? entries[index - 1].complete : undefined}
              isLast={index === entries.length - 1}
              onToggleDone={onToggleDone}
              onSaveNote={onSaveNote}
              onSaveText={onSaveText}
              onRemove={onRemove}
            />
          ))}
        </ul>
      </SortableContext>
    </DndContext>
  );
}
