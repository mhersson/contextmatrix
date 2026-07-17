import { useCallback, useRef, useState } from 'react';
import type { CreateCardInput, ProjectConfig } from '../../types';

export interface PendingTemplate {
  type: string;
  body: string;
}

export interface CreateCardForm {
  // Field values
  title: string;
  type: string;
  priority: string;
  labels: string[];
  parent: string;
  body: string;
  bodyDirty: boolean;
  autonomous: boolean;
  modelOrchestrator: string;
  modelCoder: string;
  modelReviewer: string;
  featureBranch: boolean;
  createPR: boolean;
  baseBranch: string;
  // Best-of-N and mob session - surfaced at create time when the agent backend is
  // active (see AutomationCheckboxes). 0/[] = off / unset.
  bestOfN: number;
  mobParticipants: number;
  mobPhases: string[];
  mobGuests: string[];
  skills: string[] | null;
  isSubmitting: boolean;
  pendingTemplate: PendingTemplate | null;

  // Field setters
  setTitle: (v: string) => void;
  setPriority: (v: string) => void;
  setLabels: (v: string[]) => void;
  setAutonomous: (v: boolean) => void;
  setModelOrchestrator: (v: string) => void;
  setModelCoder: (v: string) => void;
  setModelReviewer: (v: string) => void;
  setFeatureBranch: (v: boolean) => void;
  setCreatePR: (v: boolean) => void;
  setBaseBranch: (v: string) => void;
  setBestOfN: (v: number) => void;
  setMobParticipants: (v: number) => void;
  setMobPhases: (v: string[]) => void;
  setMobGuests: (v: string[]) => void;
  setSkills: (v: string[] | null) => void;
  setBody: (v: string) => void;
  setBodyDirty: (v: boolean) => void;
  setPendingTemplate: (v: PendingTemplate | null) => void;

  // Compound setters / handlers
  handleSetParent: (newParent: string) => void;
  handleTypeChange: (newType: string) => void;

  // Submit handlers - return Promise<void> and catch internally; parent
  // (component) should not swallow errors - it shows toast; form stays open.
  handleJustCreate: () => Promise<void>;
  handleCreateAndRun: () => Promise<void>;
}

export interface UseCreateCardForm {
  form: CreateCardForm;
  // Ref for the title <input>, returned separately so the `form` object
  // contains only plain values/setters - keeps the `react-hooks/refs`
  // lint rule from flagging every `form.X` access on the caller side.
  titleInputRef: React.RefObject<HTMLInputElement | null>;
}

export function useCreateCardForm(
  config: ProjectConfig,
  onCreate: (input: CreateCardInput, opts?: { run?: boolean; interactive?: boolean }) => Promise<void>,
): UseCreateCardForm {
  const [title, setTitle] = useState('');
  const [type, setType] = useState(config.types[0] || 'task');
  const [priority, setPriority] = useState(config.priorities[1] || config.priorities[0] || '');
  const [labels, setLabels] = useState<string[]>([]);
  const [parent, setParent] = useState('');
  const [body, setBody] = useState(() => config.templates?.[config.types[0]] ?? '');
  const [bodyDirty, setBodyDirty] = useState(false);
  const [autonomous, setAutonomous] = useState(false);
  const [modelOrchestrator, setModelOrchestrator] = useState('');
  const [modelCoder, setModelCoder] = useState('');
  const [modelReviewer, setModelReviewer] = useState('');
  const [featureBranch, setFeatureBranch] = useState(true);
  const [createPR, setCreatePR] = useState(true);
  const [baseBranch, setBaseBranch] = useState('');
  // 0 = off / unset; the AutomationCheckboxes selector (create mode, agent
  // backend only) writes here, and buildInput forwards non-zero values.
  const [bestOfN, setBestOfN] = useState(0);
  const [mobParticipants, setMobParticipants] = useState(0);
  const [mobPhases, setMobPhases] = useState<string[]>([]);
  const [mobGuests, setMobGuests] = useState<string[]>([]);
  // null = inherit project default, [] = mount none, [...] = specific list.
  const [skills, setSkills] = useState<string[] | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [pendingTemplate, setPendingTemplate] = useState<PendingTemplate | null>(null);

  // Tracks the type the user had selected before a parent was set, so we
  // can restore it on clear. Updated by handleSetParent synchronously
  // alongside the parent change - avoids the cascading-render lint and the
  // race where `type` could briefly read 'subtask' before the parent state
  // change settled.
  const prevTypeRef = useRef<string>(type);

  const titleInputRef = useRef<HTMLInputElement | null>(null);

  // Wrap setParent so the type-lock and parent change happen in one
  // commit, no effect required.
  const handleSetParent = useCallback(
    (newParent: string) => {
      setParent(newParent);
      if (newParent) {
        if (type !== 'subtask') prevTypeRef.current = type;
        setType('subtask');
      } else {
        const restored =
          prevTypeRef.current === 'subtask' ? (config.types[0] ?? 'task') : prevTypeRef.current;
        setType(restored);
      }
    },
    [type, config.types],
  );

  const handleTypeChange = useCallback(
    (newType: string) => {
      const template = config.templates?.[newType];
      if (template) {
        if (bodyDirty) {
          setPendingTemplate({ type: newType, body: template });
        } else {
          setBody(template);
        }
      } else if (!bodyDirty) {
        setBody('');
      }
      setType(newType);
    },
    [config.templates, bodyDirty],
  );

  const buildInput = useCallback(
    (forRun: boolean): CreateCardInput => ({
      title: title.trim(),
      type,
      priority,
      labels: labels.length > 0 ? labels : undefined,
      parent: parent || undefined,
      body: body || undefined,
      autonomous: autonomous || undefined,
      // Per-role model pins for the agent backend. Empty = "selector decides";
      // only forward a non-empty override.
      model_orchestrator: modelOrchestrator || undefined,
      model_coder: modelCoder || undefined,
      model_reviewer: modelReviewer || undefined,
      // Best-of-N and mob session - only forwarded when the user picked a value
      // (0 / [] mean "off / unset" and are omitted to keep the input clean).
      best_of_n: bestOfN || undefined,
      mob_participants: mobParticipants || undefined,
      mob_phases: mobPhases.length ? mobPhases : undefined,
      mob_guests: mobGuests.length ? mobGuests : undefined,
      // Server force-enables both on Run; mirror that here so the persisted
      // record matches what the user sees in the form.
      feature_branch: forRun ? true : featureBranch || undefined,
      create_pr: forRun ? true : createPR || undefined,
      base_branch: baseBranch || undefined,
      // null = inherit project default; only forward an explicit override.
      skills: skills === null ? undefined : skills,
    }),
    [title, type, priority, labels, parent, body, autonomous, modelOrchestrator, modelCoder, modelReviewer, bestOfN, mobParticipants, mobPhases, mobGuests, featureBranch, createPR, baseBranch, skills],
  );

  const ensureTitle = useCallback((): boolean => {
    if (title.trim()) return true;
    titleInputRef.current?.focus();
    return false;
  }, [title]);

  const handleJustCreate = useCallback(async () => {
    if (isSubmitting) return;
    if (!ensureTitle()) return;
    setIsSubmitting(true);
    try {
      await onCreate(buildInput(false), { run: false });
    } catch {
      // Parent shows error toast; keep form open.
    } finally {
      setIsSubmitting(false);
    }
  }, [isSubmitting, ensureTitle, buildInput, onCreate]);

  const handleCreateAndRun = useCallback(async () => {
    if (isSubmitting) return;
    if (!ensureTitle()) return;
    setIsSubmitting(true);
    try {
      await onCreate(buildInput(true), { run: true, interactive: !autonomous });
    } catch {
      // Parent shows error toast; keep form open.
    } finally {
      setIsSubmitting(false);
    }
  }, [isSubmitting, ensureTitle, buildInput, onCreate, autonomous]);

  return {
    form: {
      title,
      type,
      priority,
      labels,
      parent,
      body,
      bodyDirty,
      autonomous,
      modelOrchestrator,
      modelCoder,
      modelReviewer,
      featureBranch,
      createPR,
      baseBranch,
      bestOfN,
      mobParticipants,
      mobPhases,
      mobGuests,
      skills,
      isSubmitting,
      pendingTemplate,
      setTitle,
      setPriority,
      setLabels,
      setAutonomous,
      setModelOrchestrator,
      setModelCoder,
      setModelReviewer,
      setFeatureBranch,
      setCreatePR,
      setBaseBranch,
      setBestOfN,
      setMobParticipants,
      setMobPhases,
      setMobGuests,
      setSkills,
      setBody,
      setBodyDirty,
      setPendingTemplate,
      handleSetParent,
      handleTypeChange,
      handleJustCreate,
      handleCreateAndRun,
    },
    titleInputRef,
  };
}
