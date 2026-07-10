import { useEffect, useId, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, isAPIError } from '../../api/client';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useProjects } from '../../hooks/useProjects';
import { notifyChatSessionsChanged } from '../../hooks/useChatSessions';
import { ChatModelPicker } from './ChatModelPicker';
import type { ChatModel } from '../../types';

interface NewChatDialogProps {
  open: boolean;
  onClose: () => void;
}

export function NewChatDialog({ open, onClose }: NewChatDialogProps) {
  const navigate = useNavigate();
  const { projects } = useProjects();
  const [title, setTitle] = useState('');
  const [project, setProject] = useState('');
  const [model, setModel] = useState('');
  const [models, setModels] = useState<ChatModel[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [modelSource, setModelSource] = useState<'openrouter' | 'endpoint'>('endpoint');
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const dialogRef = useRef<HTMLDivElement>(null);
  const titleInputRef = useRef<HTMLInputElement>(null);
  const titleId = useId();

  useFocusTrap(dialogRef, open, titleInputRef);

  // Escape key closes dialog
  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, onClose]);

  // Fetch the available models once on mount. Failures are non-fatal —
  // we fall back to an empty list, the picker disappears, and the create
  // POST omits the model field (the server applies its default).
  //
  // On success, seed `model` to the server default IF the user hasn't
  // already picked something. Without this seed the state lags the
  // visually-selected first option in the dropdown and submitting without
  // touching the picker would send `model: undefined` (i.e. the user's
  // "selection" silently degraded to "server default").
  useEffect(() => {
    let cancelled = false;
    void api
      .listChatModels()
      .then((resp) => {
        if (cancelled) return;
        setModels(resp.models);
        setDefaultModel(resp.default);
        setModelSource(resp.source ?? 'endpoint');
        setModel((cur) => cur || resp.default);
      })
      .catch((e) => {
        console.warn('listChatModels failed; picker disabled', e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Reset form state when the dialog opens. Uses the in-render reset
  // pattern (web/CLAUDE.md § CardPanel) so the reset is synchronous with
  // the prop change and the React 19 lint rule (no setState in effects)
  // stays happy. We intentionally DO NOT reset `model` here — the user's
  // last selection persists across dialog open/close cycles, and the
  // initial value comes from the listChatModels useEffect above.
  const [prevOpen, setPrevOpen] = useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setTitle('');
      setProject('');
      setError(null);
    }
  }

  if (!open) return null;

  const handleCreate = async () => {
    setCreating(true);
    setError(null);
    try {
      const sess = await api.createChat({
        title: title.trim() || undefined,
        project: project || undefined,
        model: model || undefined,
      });
      // Tell any mounted sidebar to refresh its list before we navigate —
      // otherwise the user lands on the new chat but the sidebar still
      // shows the old set until a manual refresh.
      notifyChatSessionsChanged();
      // Kick the worker so the container is spawning by the time the user
      // lands on the chat thread. Failure is non-fatal: the session row
      // exists, and the user can retry via the Reopen button on the cold
      // status header.
      try {
        await api.openChat(sess.id);
      } catch (openErr) {
        console.warn('openChat failed; user can Reopen manually', openErr);
      }
      // Second notify so the sidebar status flips from cold → active once
      // the worker is warm (the first notify above fired before openChat
      // returned, so its result reflected the still-cold session).
      notifyChatSessionsChanged();
      onClose();
      navigate(`/chat/${sess.id}`);
    } catch (e) {
      setError(isAPIError(e) ? e.error : 'Failed to create chat');
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        aria-hidden="true"
        onClick={onClose}
      />

      {/* Dialog panel */}
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative z-10 w-full max-w-md rounded-lg shadow-lg mx-4 p-5 border"
        style={{ backgroundColor: 'var(--bg1)', borderColor: 'var(--bg3)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2
          id={titleId}
          className="text-sm font-semibold mb-4"
          style={{ color: 'var(--fg)' }}
        >
          New chat
        </h2>

        <label
          className="block text-xs mb-1"
          style={{ color: 'var(--grey2)' }}
        >
          Title <span style={{ color: 'var(--grey1)' }}>(optional)</span>
        </label>
        <input
          ref={titleInputRef}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !creating) void handleCreate();
          }}
          className="bf-input w-full mb-3"
          placeholder="Auto-generated from your first message if blank"
        />

        <label
          className="block text-xs mb-1"
          style={{ color: 'var(--grey2)' }}
        >
          Project <span style={{ color: 'var(--grey1)' }}>(optional)</span>
        </label>
        <select
          value={project}
          onChange={(e) => setProject(e.target.value)}
          className="bf-input w-full mb-3"
        >
          <option value="">— Cross-project (no clone) —</option>
          {projects.map((p) => (
            <option key={p.name} value={p.name}>
              {p.display_name ?? p.name}
            </option>
          ))}
        </select>

        <ChatModelPicker
          source={modelSource}
          model={model}
          defaultModel={defaultModel}
          models={models}
          onChange={setModel}
        />

        {error && (
          <div className="text-xs mb-3" style={{ color: 'var(--red)' }}>
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={creating}
            className="bf-btn-ghost bf-btn-sm"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void handleCreate()}
            disabled={creating}
            className="bf-btn-primary"
          >
            {creating ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  );
}
