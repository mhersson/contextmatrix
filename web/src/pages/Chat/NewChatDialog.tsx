import { useEffect, useId, useRef, useState } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useProjects } from '../../hooks/useProjects';
import { useTheme } from '../../hooks/useTheme';
import { BlueprintFields } from './BlueprintFields';
import { ChatModelPicker } from './ChatModelPicker';
import { Field } from './Field';
import { KindTiles } from './KindTiles';
import { NewChatFooter } from './NewChatFooter';
import { ProjectList } from './ProjectList';
import { EMPTY_BLUEPRINT_FORM, createLabel, disabledReason, type BlueprintForm } from './blueprintForm';
import { useChatModelChoice, type ChatModelChoice } from './useChatModelChoice';
import { useCreateSession } from './useCreateSession';

interface NewChatDialogProps {
  open: boolean;
  onClose: () => void;
}

// The model pick lives out here so it persists between opens; the body
// remounts on every open, so everything else starts fresh.
export function NewChatDialog({ open, onClose }: NewChatDialogProps) {
  const models = useChatModelChoice();
  if (!open) return null;
  return <NewChatDialogBody onClose={onClose} models={models} />;
}

function NewChatDialogBody({ onClose, models }: { onClose: () => void; models: ChatModelChoice }) {
  const { projects } = useProjects();
  const { architectEnabled } = useTheme();
  const [blueprint, setBlueprint] = useState(false);
  const [form, setForm] = useState<BlueprintForm>(EMPTY_BLUEPRINT_FORM);
  const [title, setTitle] = useState('');
  const [project, setProject] = useState('');
  const { create, creating, error, clearError } = useCreateSession(onClose);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleInputRef = useRef<HTMLInputElement>(null);
  const headingId = useId();
  const titleFieldId = useId();

  useFocusTrap(dialogRef, true, titleInputRef);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape' && !creating) onClose();
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose, creating]);

  const reason = disabledReason(blueprint, form);
  const canCreate = !creating && reason === null;

  const submit = () => {
    if (!canCreate) return;
    const name = title.trim() || undefined;
    void create(
      blueprint
        ? {
            title: name,
            kind: 'architect',
            projects: form.projects,
            mode: form.mode,
            cost_cap_usd: form.capUSD > 0 ? form.capUSD : undefined,
          }
        : { title: name, project: project || undefined, model: models.model || undefined },
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="nd-scrim absolute inset-0" aria-hidden="true" onClick={creating ? undefined : onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingId}
        className={`nd-dialog${blueprint ? ' nd-dialog--blueprint' : ''}`}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            // The model combobox commits its pick on Enter; submitting now would send the stale model.
            if ((e.target as HTMLElement).closest('[role="combobox"]')) return;
            e.preventDefault();
            submit();
          }
        }}
      >
        <div className="nd-head">
          <h2 id={headingId}>{blueprint ? 'New blueprint' : 'New chat'}</h2>
          {architectEnabled && <KindTiles blueprint={blueprint} onChange={(next) => {
                setBlueprint(next);
                clearError();
              }}
            />}
        </div>
        <div className="nd-body">
          <Field label="Title" hint="Optional" htmlFor={titleFieldId}>
            <input
              id={titleFieldId}
              ref={titleInputRef}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
                  e.preventDefault();
                  submit();
                }
              }}
              className="bf-input"
              placeholder="Named from your first message if blank"
            />
          </Field>
          {blueprint ? (
            <BlueprintFields projects={projects} value={form} onChange={setForm} />
          ) : (
            <>
              <Field label="Project" hint={project ? 'The chat gets a checkout of its repo' : 'No checkout'}>
                <ProjectList
                  projects={projects}
                  multi={false}
                  selected={[project]}
                  onChange={(next) => setProject(next[0] ?? '')}
                />
              </Field>
              <ChatModelPicker {...models} />
            </>
          )}
        </div>
        <NewChatFooter
          reason={reason}
          error={error}
          creating={creating}
          canCreate={canCreate}
          yolo={blueprint && form.mode === 'yolo'}
          label={createLabel(blueprint, form.mode, creating)}
          onCancel={onClose}
          onCreate={submit}
        />
      </div>
    </div>
  );
}
