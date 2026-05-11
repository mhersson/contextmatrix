import type { KnowledgeBaseSummary, RefreshJobStatus } from '../../types';
import { KnowledgeBaseSidebar } from './KnowledgeBaseSidebar';

interface MobileDocSheetProps {
  summary: KnowledgeBaseSummary;
  selected: { repo: string; doc: string } | null;
  onSelect: (sel: { repo: string; doc: string }) => void;
  onRefreshClick?: (repo: string) => void;
  refreshStatusByRepo?: Record<string, RefreshJobStatus>;
  onClose: () => void;
}

export function MobileDocSheet({
  summary,
  selected,
  onSelect,
  onRefreshClick,
  refreshStatusByRepo,
  onClose,
}: MobileDocSheetProps) {
  const handleSelect = (sel: { repo: string; doc: string }) => {
    onSelect(sel);
    onClose();
  };

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} aria-hidden="true" />
      <div
        className="card-panel animate-panel-slide-in"
        style={{ width: '18rem', minWidth: 0, maxWidth: '100vw' }}
        role="dialog"
        aria-modal="true"
        aria-label="Select a document"
      >
        <KnowledgeBaseSidebar
          summary={summary}
          selected={selected}
          onSelect={handleSelect}
          onRefreshClick={onRefreshClick}
          refreshStatusByRepo={refreshStatusByRepo}
        />
      </div>
    </>
  );
}
