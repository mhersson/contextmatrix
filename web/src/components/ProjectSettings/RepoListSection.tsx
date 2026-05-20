import { useId } from 'react';
import type { CSSProperties } from 'react';

// ---------- ListEditor (private helper) ----------

interface ListEditorProps {
  label: string;
  items: string[];
  newValue: string;
  setNewValue: (v: string) => void;
  onAdd: () => void;
  onRemove: (v: string) => void;
  protectedItems?: string[];
  inputStyle: CSSProperties;
}

function ListEditor({
  label,
  items,
  newValue,
  setNewValue,
  onAdd,
  onRemove,
  protectedItems,
  inputStyle,
}: ListEditorProps) {
  const inputId = useId();
  return (
    <div>
      <label htmlFor={inputId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
        {label}
      </label>
      <div className="flex flex-wrap gap-1.5 mb-2">
        {items.map((item) => (
          <span
            key={item}
            className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded"
            style={{ backgroundColor: 'var(--bg2)', color: 'var(--fg)' }}
          >
            {item}
            {!(protectedItems || []).includes(item) && (
              <button
                onClick={() => onRemove(item)}
                className="hover:text-[var(--red)] transition-colors"
                style={{ color: 'var(--grey1)' }}
                aria-label={`Remove ${item}`}
              >
                &times;
              </button>
            )}
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <input
          id={inputId}
          type="text"
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onAdd();
          }}
          placeholder={`Add ${label.toLowerCase().slice(0, -1)}...`}
          className="flex-1 px-3 py-1.5 rounded text-xs border focus:outline-none"
          style={inputStyle}
        />
        <button
          onClick={onAdd}
          disabled={!newValue.trim()}
          className="px-2 py-1.5 rounded text-xs transition-colors"
          style={{
            backgroundColor: newValue.trim() ? 'var(--bg3)' : 'var(--bg2)',
            color: newValue.trim() ? 'var(--fg)' : 'var(--grey1)',
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}

// ---------- RepoListSection ----------

export interface RepoListSectionProps {
  states: string[];
  newState: string;
  setNewState: (v: string) => void;
  onAddState: () => void;
  onRemoveState: (v: string) => void;

  types: string[];
  newType: string;
  setNewType: (v: string) => void;
  onAddType: () => void;
  onRemoveType: (v: string) => void;

  priorities: string[];
  newPriority: string;
  setNewPriority: (v: string) => void;
  onAddPriority: () => void;
  onRemovePriority: (v: string) => void;

  inputStyle: CSSProperties;
}

export function RepoListSection({
  states,
  newState,
  setNewState,
  onAddState,
  onRemoveState,
  types,
  newType,
  setNewType,
  onAddType,
  onRemoveType,
  priorities,
  newPriority,
  setNewPriority,
  onAddPriority,
  onRemovePriority,
  inputStyle,
}: RepoListSectionProps) {
  return (
    <>
      <ListEditor
        label="States"
        items={states}
        newValue={newState}
        setNewValue={setNewState}
        onAdd={onAddState}
        onRemove={onRemoveState}
        protectedItems={['stalled', 'not_planned']}
        inputStyle={inputStyle}
      />

      <ListEditor
        label="Types"
        items={types}
        newValue={newType}
        setNewValue={setNewType}
        onAdd={onAddType}
        onRemove={onRemoveType}
        inputStyle={inputStyle}
      />

      <ListEditor
        label="Priorities"
        items={priorities}
        newValue={newPriority}
        setNewValue={setNewPriority}
        onAdd={onAddPriority}
        onRemove={onRemovePriority}
        inputStyle={inputStyle}
      />
    </>
  );
}
