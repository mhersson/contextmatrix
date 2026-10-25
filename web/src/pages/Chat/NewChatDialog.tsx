import { useEffect, useId, useRef, useState } from 'react';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useIdentity } from '../../hooks/useIdentity';
import { useProjects } from '../../hooks/useProjects';
import { useTheme } from '../../hooks/useTheme';
import type { HandoffPrefill } from '../../components/ChatPanel/handoff';
import type { SessionKind } from '../../types';
import { BlueprintFields } from './BlueprintFields';
import { ChatModelPicker } from './ChatModelPicker';
import { Field } from './Field';
import { HandoffBanner, NeedField } from './HandoffFields';
import { KindTiles } from './KindTiles';
import { NewChatFooter } from './NewChatFooter';
import { ProjectList } from './ProjectList';
import {
  EMPTY_BLUEPRINT_FORM,
  createLabel,
  disabledReason,
  needReason,
  projectListReason,
  readPlainLanguage,
  rememberPlainLanguage,
  type BlueprintForm,
} from './blueprintForm';
import { useChatModelChoice, type ChatModelChoices } from './useChatModelChoice';
import { useCreateSession } from './useCreateSession';

interface NewChatDialogProps {
  open: boolean;
  onClose: () => void;
  /** Opens a New blueprint dialog pre-filled from a chat's hand-off. */
  handoff?: HandoffPrefill;
  /** Kind the dialog opens on; Blueprint needs an architect backend. */
  initialKind?: SessionKind;
}

// The model pick lives out here so it persists between opens; the body
// remounts on every open, so everything else starts fresh.
export function NewChatDialog({ open, onClose, handoff, initialKind }: NewChatDialogProps) {
  const choices = useChatModelChoice();
  if (!open) return null;
  return <NewChatDialogBody onClose={onClose} choices={choices} handoff={handoff} initialKind={initialKind} />;
}

interface NewChatDialogBodyProps {
  onClose: () => void;
  choices: ChatModelChoices;
  handoff?: HandoffPrefill;
  initialKind?: SessionKind;
}

function NewChatDialogBody({ onClose, choices, handoff, initialKind }: NewChatDialogBodyProps) {
  const { projects, loading, error: projectsError } = useProjects();
  const { architectEnabled } = useTheme();
  const { identity } = useIdentity();
  const [blueprint, setBlueprint] = useState(handoff !== undefined || (initialKind === 'architect' && architectEnabled));
  const [kindPicked, setKindPicked] = useState(false);
  // architectEnabled is false until the app config loads, so a reload of ?new=blueprint renders Chat first.
  if (initialKind === 'architect' && architectEnabled && !blueprint && !kindPicked) setBlueprint(true);
  const [form, setForm] = useState<BlueprintForm>(() => ({
    ...EMPTY_BLUEPRINT_FORM,
    projects: handoff?.projects ?? [],
    plainLanguage: readPlainLanguage(identity),
  }));
  const [title, setTitle] = useState(handoff?.title ?? '');
  const [need, setNeed] = useState(handoff?.need ?? '');
  // Null until the project list has loaded; a name missing from a list still loading or failed may exist.
  const [leftOut, setLeftOut] = useState<string[] | null>(null);
  if (handoff && !loading && !projectsError && leftOut === null) {
    const known = new Set(projects.map((p) => p.name));
    setLeftOut(handoff.projects.filter((name) => !known.has(name)));
    setForm((f) => ({ ...f, projects: f.projects.filter((name) => known.has(name)) }));
  }
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

  const picked = choices.blueprint;
  const modelUnpriced = picked.models.some((m) => m.id === picked.model && m.unpriced === true);
  const reason =
    disabledReason(blueprint, form, modelUnpriced) ??
    (handoff ? (leftOut === null ? projectListReason(Boolean(projectsError)) : needReason(need)) : null);
  const canCreate = !creating && reason === null;

  const submit = () => {
    if (!canCreate) return;
    const name = title.trim() || undefined;
    if (blueprint) rememberPlainLanguage(identity, form.plainLanguage);
    void create(
      blueprint
        ? {
            title: name,
            kind: 'architect',
            projects: form.projects,
            model: picked.model || undefined,
            mode: form.mode,
            cost_cap_usd: form.capUSD > 0 ? form.capUSD : undefined,
            ...(form.plainLanguage ? { plain_language: true } : {}),
            ...(form.unattended ? { unattended: true } : {}),
            ...(handoff ? { origin_chat: handoff.chatId, origin_seq: handoff.seq } : {}),
          }
        : { title: name, project: project || undefined, model: choices.chat.model || undefined },
      handoff ? need : undefined,
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
          {handoff ? (
            <HandoffBanner chatTitle={handoff.chatTitle} />
          ) : (
            architectEnabled && (
              <KindTiles
                blueprint={blueprint}
                onChange={(next) => {
                  setBlueprint(next);
                  setKindPicked(true);
                  clearError();
                }}
              />
            )
          )}
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
          {handoff && <NeedField value={need} onChange={setNeed} />}
          {blueprint ? (
            <BlueprintFields
              projects={projects}
              value={form}
              onChange={setForm}
              models={picked}
              leftOut={leftOut ?? undefined}
            />
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
              <ChatModelPicker {...choices.chat} />
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
