// eslint-disable-next-line react-refresh/only-export-components
export const createButtonStyle = {
  backgroundColor: 'var(--bg-green)',
  color: 'var(--green)',
  border: '1px solid var(--green)',
};

interface CreatePlaybookFormProps {
  title: string;
  onTitleChange: (value: string) => void;
  onCreate: () => void;
  onCancel: () => void;
  submitting: boolean;
}

export function CreatePlaybookForm({ title, onTitleChange, onCreate, onCancel, submitting }: CreatePlaybookFormProps) {
  return (
    <div className="flex items-center justify-center gap-2 flex-wrap">
      <input
        autoFocus
        value={title}
        onChange={(e) => onTitleChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') onCreate();
          if (e.key === 'Escape') onCancel();
        }}
        placeholder="Playbook title"
        aria-label="Playbook title"
        className="px-3 py-1.5 rounded text-sm"
        style={{ backgroundColor: 'var(--bg2)', border: '1px solid var(--bg3)', color: 'var(--fg)' }}
      />
      <button
        onClick={onCreate}
        disabled={submitting}
        className="px-3 py-1.5 rounded text-sm font-medium"
        style={createButtonStyle}
      >
        Create
      </button>
      <button onClick={onCancel} className="px-3 py-1.5 rounded text-sm" style={{ color: 'var(--grey1)' }}>
        Cancel
      </button>
    </div>
  );
}

interface PlaybooksEmptyHeroProps extends CreatePlaybookFormProps {
  creating: boolean;
  onStartCreate: () => void;
}

export function PlaybooksEmptyHero({ creating, onStartCreate, ...form }: PlaybooksEmptyHeroProps) {
  return (
    <div className="empty-hero flex-1">
      <div className="empty-hero-card">
        <div className="empty-hero-icon" aria-hidden="true">▤</div>
        <div className="empty-hero-title">No playbooks yet</div>
        {creating ? (
          <CreatePlaybookForm {...form} />
        ) : (
          <div className="empty-hero-hint">
            Create your first
            <button type="button" className="empty-hero-link" onClick={onStartCreate}>+ New playbook</button>
          </div>
        )}
      </div>
    </div>
  );
}
