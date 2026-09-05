import { useId } from 'react';

// ---------- ListEditor (private helper) ----------

interface ListEditorProps {
  label: string;
  /** Singular noun for the add-row placeholder. */
  itemName: string;
  items: string[];
  newValue: string;
  setNewValue: (v: string) => void;
  onAdd: () => void;
  onRemove: (v: string) => void;
  protectedItems?: string[];
}

function ListEditor({
  label,
  itemName,
  items,
  newValue,
  setNewValue,
  onAdd,
  onRemove,
  protectedItems,
}: ListEditorProps) {
  const inputId = useId();
  return (
    <div className="ps-field">
      <label htmlFor={inputId} className="ps-label">
        {label}
      </label>
      <div className="ps-chips">
        {items.map((item) =>
          (protectedItems || []).includes(item) ? (
            <span key={item} className="ps-chip ps-chip--locked" title="Built-in state">
              {item}
            </span>
          ) : (
            <span key={item} className="ps-chip">
              {item}
              <button type="button" onClick={() => onRemove(item)} aria-label={`Remove ${item}`}>
                &times;
              </button>
            </span>
          ),
        )}
      </div>
      <div className="ps-addrow">
        <input
          id={inputId}
          type="text"
          value={newValue}
          onChange={(e) => setNewValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onAdd();
          }}
          placeholder={`Add ${itemName}...`}
          className="bf-input"
        />
        <button type="button" onClick={onAdd} disabled={!newValue.trim()} className="bf-btn-ghost bf-btn-sm">
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
}: RepoListSectionProps) {
  return (
    <>
      <ListEditor
        label="States"
        itemName="state"
        items={states}
        newValue={newState}
        setNewValue={setNewState}
        onAdd={onAddState}
        onRemove={onRemoveState}
        protectedItems={['stalled', 'not_planned']}
      />

      <div className="ps-two mt-3.5">
        <ListEditor
          label="Types"
          itemName="type"
          items={types}
          newValue={newType}
          setNewValue={setNewType}
          onAdd={onAddType}
          onRemove={onRemoveType}
        />

        <ListEditor
          label="Priorities"
          itemName="priority"
          items={priorities}
          newValue={newPriority}
          setNewValue={setNewPriority}
          onAdd={onAddPriority}
          onRemove={onRemovePriority}
        />
      </div>
    </>
  );
}
