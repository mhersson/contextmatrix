import { Link } from 'react-router';
import type { PlaybookDetail } from '../../types';

interface PlaybookDetailHeaderProps {
  detail: PlaybookDetail;
  editingTitle: boolean;
  editingDescription: boolean;
  onStartEditTitle: () => void;
  onStartEditDescription: () => void;
  onSaveTitle: (value: string) => void;
  onSaveDescription: (value: string) => void;
  onDeleteClick: () => void;
}

const editableInputStyle = { color: 'var(--fg)', border: '1px solid var(--bg3)' };

function blurOnEnter(e: React.KeyboardEvent<HTMLInputElement>) {
  if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
}

/** Breadcrumb plus click-to-edit title/description; progress lives in the
 * side panel, so the title row carries the Delete action. */
export function PlaybookDetailHeader({
  detail, editingTitle, editingDescription,
  onStartEditTitle, onStartEditDescription, onSaveTitle, onSaveDescription, onDeleteClick,
}: PlaybookDetailHeaderProps) {
  return (
    <>
      <p className="font-mono text-xs" style={{ color: 'var(--grey1)' }}>
        <Link to="/playbooks" style={{ color: 'inherit' }}>playbooks</Link> / {detail.id}
      </p>

      <div className="flex items-baseline gap-4 mt-1 mb-2">
        {editingTitle ? (
          <input
            autoFocus
            defaultValue={detail.title}
            onBlur={(e) => onSaveTitle(e.target.value)}
            onKeyDown={blurOnEnter}
            className="text-2xl font-medium flex-1 min-w-0 bg-transparent rounded px-1"
            style={{ ...editableInputStyle, fontFamily: 'var(--font-display)' }}
          />
        ) : (
          <h1
            onClick={onStartEditTitle}
            className="cursor-text flex-1 min-w-0"
            style={{ fontFamily: 'var(--font-display)', fontWeight: 500, fontSize: '24px', color: 'var(--fg)' }}
          >
            {detail.title}
          </h1>
        )}
        <button type="button" onClick={onDeleteClick} className="text-sm shrink-0" style={{ color: 'var(--red)' }}>
          Delete
        </button>
      </div>

      {editingDescription ? (
        <input
          autoFocus
          defaultValue={detail.description ?? ''}
          placeholder="Add a description"
          onBlur={(e) => onSaveDescription(e.target.value)}
          onKeyDown={blurOnEnter}
          className="w-full mb-4 bg-transparent text-sm rounded px-1"
          style={editableInputStyle}
        />
      ) : (
        <p
          onClick={onStartEditDescription}
          className="cursor-text mb-4 text-sm"
          style={{ color: 'var(--grey2)' }}
        >
          {detail.description || 'Add a description'}
        </p>
      )}
    </>
  );
}
