import type { PlaybookDetail, PlaybookSegment } from '../../types';
import { SegmentedProgress } from './SegmentedProgress';

interface PlaybookDetailHeaderProps {
  detail: PlaybookDetail;
  segments: PlaybookSegment[];
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

/** Breadcrumb, click-to-edit title/description, and the progress + delete row. */
export function PlaybookDetailHeader({
  detail, segments, editingTitle, editingDescription,
  onStartEditTitle, onStartEditDescription, onSaveTitle, onSaveDescription, onDeleteClick,
}: PlaybookDetailHeaderProps) {
  return (
    <>
      <p className="font-mono text-xs" style={{ color: 'var(--grey1)' }}>playbooks / {detail.id}</p>

      {editingTitle ? (
        <input
          autoFocus
          defaultValue={detail.title}
          onBlur={(e) => onSaveTitle(e.target.value)}
          onKeyDown={blurOnEnter}
          className="text-2xl font-medium w-full mt-1 mb-2 bg-transparent rounded px-1"
          style={{ ...editableInputStyle, fontFamily: 'var(--font-display)' }}
        />
      ) : (
        <h1
          onClick={onStartEditTitle}
          className="cursor-text mt-1 mb-2"
          style={{ fontFamily: 'var(--font-display)', fontWeight: 500, fontSize: '24px', color: 'var(--fg)' }}
        >
          {detail.title}
        </h1>
      )}

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

      <div className="flex items-center gap-3 mb-4">
        <SegmentedProgress segments={segments} className="flex-1" />
        <span className="font-mono text-xs" style={{ color: 'var(--grey1)' }}>
          {detail.complete} of {detail.total} complete
        </span>
        <button type="button" onClick={onDeleteClick} className="text-sm" style={{ color: 'var(--red)' }}>
          Delete
        </button>
      </div>
    </>
  );
}
