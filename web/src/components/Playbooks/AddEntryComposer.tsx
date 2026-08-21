import { useEffect, useMemo, useState } from 'react';
import { api } from '../../api/client';
import { useProjects } from '../../hooks/useProjects';
import type { Card, NewPlaybookEntry, PlaybookEntry } from '../../types';
import { CardPicker } from './CardPicker';

interface AddEntryComposerProps {
  onAdd: (entry: NewPlaybookEntry) => Promise<void>;
  /** Current playbook entries; their cards are hidden from the picker. */
  entries: PlaybookEntry[];
}

const inputStyle = {
  backgroundColor: 'var(--bg2)',
  border: '1px solid var(--bg3)',
  color: 'var(--fg)',
};

/** Appends a new card or manual entry to the end of the playbook - order is
 * adjusted afterward by dragging, so this never takes a position. */
export function AddEntryComposer({ onAdd, entries }: AddEntryComposerProps) {
  const { projects } = useProjects();
  const [mode, setMode] = useState<'card' | 'manual'>('card');
  const [project, setProject] = useState('');
  const [cards, setCards] = useState<Card[]>([]);
  const [cardFilter, setCardFilter] = useState('');
  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [manualText, setManualText] = useState('');
  const [note, setNote] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // Derived in render rather than synced via an effect - `project` starts
  // empty and only needs a fallback until the user (or the projects list)
  // supplies a real value.
  const effectiveProject = project || projects[0]?.name || '';

  const existingCardIds = useMemo(
    () => new Set(entries.flatMap((e) => (e.type === 'card' && e.project === effectiveProject && e.card ? [e.card] : []))),
    [entries, effectiveProject],
  );

  useEffect(() => {
    if (!effectiveProject) return;
    let cancelled = false;
    api.getCards(effectiveProject).then((list) => { if (!cancelled) setCards(list); }).catch(() => {});
    return () => { cancelled = true; };
  }, [effectiveProject]);

  const reset = () => {
    setSelectedCard(null);
    setCardFilter('');
    setManualText('');
    setNote('');
  };

  const handleAdd = async () => {
    if (submitting) return;
    let entry: NewPlaybookEntry;
    if (mode === 'card') {
      if (!selectedCard) return;
      entry = { type: 'card', project: effectiveProject, card: selectedCard.id, note: note.trim() || undefined };
    } else {
      const text = manualText.trim();
      if (!text) return;
      entry = { type: 'manual', text, note: note.trim() || undefined };
    }
    setSubmitting(true);
    try {
      await onAdd(entry);
      reset();
    } finally {
      setSubmitting(false);
    }
  };

  const canAdd = mode === 'card' ? !!selectedCard : manualText.trim().length > 0;

  return (
    <div className="rounded-[10px] border p-3" style={{ borderColor: 'var(--bg2)' }}>
      <div className="flex gap-2 mb-2">
        {(['card', 'manual'] as const).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => setMode(m)}
            className="px-2 py-1 rounded text-xs capitalize"
            style={{
              background: mode === m ? 'var(--bg-aqua)' : 'var(--bg2)',
              color: mode === m ? 'var(--aqua)' : 'var(--grey1)',
            }}
          >
            {m}
          </button>
        ))}
      </div>

      {mode === 'card' ? (
        <CardPicker
          projects={projects}
          project={effectiveProject}
          onProjectChange={(p) => { setProject(p); setSelectedCard(null); }}
          cards={cards}
          filter={cardFilter}
          onFilterChange={(f) => { setCardFilter(f); setSelectedCard(null); }}
          selectedCard={selectedCard}
          onSelectCard={(c) => { setSelectedCard(c); setCardFilter(`${c.id} - ${c.title}`); }}
          excludeIds={existingCardIds}
        />
      ) : (
        <input
          value={manualText}
          onChange={(e) => setManualText(e.target.value)}
          placeholder="Manual step"
          className="w-full px-2 py-1 rounded text-sm"
          style={inputStyle}
        />
      )}

      <input
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="note - humans only"
        className="w-full px-2 py-1 rounded text-sm mt-2"
        style={inputStyle}
      />

      <button
        type="button"
        onClick={handleAdd}
        disabled={!canAdd || submitting}
        className="mt-2 px-3 py-1.5 rounded text-sm font-medium"
        style={{ backgroundColor: 'var(--bg-green)', color: 'var(--green)', border: '1px solid var(--green)' }}
      >
        Add entry
      </button>
    </div>
  );
}
