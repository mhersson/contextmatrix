import { useState, useEffect, useCallback, useRef } from 'react';
import type { Card, ProjectConfig, CreateCardInput } from '../../types';
import { CreateCardForm } from './CreateCardForm';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { api } from '../../api/client';

interface CreateCardPanelProps {
  config: ProjectConfig;
  cards: Card[];
  onClose: () => void;
  onCreate: (input: CreateCardInput) => Promise<void>;
}

export function CreateCardPanel({ config, cards, onClose, onCreate }: CreateCardPanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [title, setTitle] = useState('');
  const [type, setType] = useState(config.types[0] || '');
  const [priority, setPriority] = useState(config.priorities[1] || config.priorities[0] || '');
  const [labels, setLabels] = useState<string[]>([]);
  const [parent, setParent] = useState('');
  const [body, setBody] = useState('');
  const [bodyDirty, setBodyDirty] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);

  useFocusTrap(panelRef, true);

  const [autonomous, setAutonomous] = useState(false);
  const [featureBranch, setFeatureBranch] = useState(false);
  const [createPR, setCreatePR] = useState(false);
  const [baseBranch, setBaseBranch] = useState('');
  const [branches, setBranches] = useState<string[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(false);
  const [branchesError, setBranchesError] = useState(false);

  // Load initial template for default type
  useEffect(() => {
    const template = config.templates?.[config.types[0]];
    if (template) setBody(template);
  }, [config.templates, config.types]);

  // Fetch branches when autonomous mode is enabled and remote execution is configured
  useEffect(() => {
    if (!autonomous || !config.remote_execution?.enabled) return;
    let cancelled = false;
    setBranchesLoading(true);
    setBranchesError(false);
    api.fetchBranches(config.name).then((data) => {
      if (!cancelled) {
        setBranches(data);
        setBranchesLoading(false);
      }
    }).catch(() => {
      if (!cancelled) {
        setBranchesError(true);
        setBranchesLoading(false);
      }
    });
    return () => { cancelled = true; };
  }, [autonomous, config.name, config.remote_execution?.enabled]);

  // Escape key handler
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleCreate = useCallback(async () => {
    if (!title.trim() || isSubmitting) return;
    setIsSubmitting(true);
    try {
      await onCreate({
        title: title.trim(),
        type,
        priority,
        labels: labels.length > 0 ? labels : undefined,
        parent: parent || undefined,
        body: body || undefined,
        autonomous: autonomous || undefined,
        feature_branch: featureBranch || undefined,
        create_pr: createPR || undefined,
        base_branch: baseBranch || undefined,
      });
    } catch {
      // Parent shows error toast; keep form open
    } finally {
      setIsSubmitting(false);
    }
  }, [title, type, priority, labels, parent, body, autonomous, featureBranch, createPR, baseBranch, isSubmitting, onCreate]);

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />

      {/* Panel */}
      <div ref={panelRef} className="card-panel animate-panel-slide-in" role="dialog" aria-modal="true" aria-label="Create card">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-[var(--bg3)]">
          <div className="flex items-center gap-3">
            <button
              onClick={onClose}
              className="text-[var(--grey1)] hover:text-[var(--fg)] transition-colors"
              title="Close (Esc)"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
            <span className="text-sm font-medium text-[var(--fg)]">New Card</span>
          </div>
          <button
            onClick={handleCreate}
            disabled={!title.trim() || isSubmitting}
            className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
              title.trim()
                ? 'bg-[var(--green)] text-[var(--bg-dim)] hover:opacity-90'
                : 'bg-[var(--bg3)] text-[var(--grey1)] cursor-not-allowed'
            }`}
          >
            {isSubmitting ? 'Creating...' : 'Create'}
          </button>
        </div>

        {/* Content */}
        <div className="p-4 overflow-y-auto" style={{ maxHeight: 'calc(100vh - 60px)' }}>
          <CreateCardForm
            title={title} setTitle={setTitle}
            type={type} setType={setType}
            priority={priority} setPriority={setPriority}
            labels={labels} setLabels={setLabels}
            parent={parent} setParent={setParent}
            body={body} setBody={setBody}
            config={config}
            cards={cards}
            bodyDirty={bodyDirty} setBodyDirty={setBodyDirty}
            autonomous={autonomous} setAutonomous={setAutonomous}
            featureBranch={featureBranch} setFeatureBranch={setFeatureBranch}
            createPR={createPR} setCreatePR={setCreatePR}
            baseBranch={baseBranch} onBaseBranchChange={setBaseBranch}
            branches={branches} branchesLoading={branchesLoading} branchesError={branchesError}
          />
        </div>
      </div>
    </>
  );
}
