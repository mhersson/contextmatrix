import { useCallback, useRef, useState } from 'react';
import { resolveCardDefaults } from '../../lib/cardDefaults';
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
  createPR: boolean;
  awaitCI: boolean;
  awaitCopilotReview: boolean;
  mergePR: boolean;
  baseBranch: string;
  // Best-of-N and mob session - surfaced at create time when the agent backend is
  // active (see AutomationCheckboxes). 0/[] = off / unset.
  bestOfN: number;
  maxCapability: boolean;
  mobParticipants: number;
  mobPhases: string[];
  mobGuests: string[];
  skills: string[] | null;
  assignee: string;
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
  setCreatePR: (v: boolean) => void;
  setAwaitCI: (v: boolean) => void;
  setAwaitCopilotReview: (v: boolean) => void;
  setMergePR: (v: boolean) => void;
  setBaseBranch: (v: string) => void;
  setBestOfN: (v: number) => void;
  setMaxCapability: (v: boolean) => void;
  setMobParticipants: (v: number) => void;
  setMobPhases: (v: string[]) => void;
  setMobGuests: (v: string[]) => void;
  setSkills: (v: string[] | null) => void;
  setAssignee: (v: string) => void;
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
  // Seed the Automation rail from the project's card_defaults (built-ins when
  // it has none). Every value is still sent explicitly on create, so what the
  // user sees is exactly what the server stores - the server-side fallback
  // only matters for callers that omit fields (MCP, GitHub import).
  const seed = resolveCardDefaults(config.card_defaults);
  const [autonomous, setAutonomous] = useState(seed.autonomous);
  const [modelOrchestrator, setModelOrchestrator] = useState('');
  const [modelCoder, setModelCoder] = useState('');
  const [modelReviewer, setModelReviewer] = useState('');
  const [createPR, setCreatePR] = useState(seed.create_pr);
  const [awaitCI, setAwaitCI] = useState(seed.await_ci);
  const [awaitCopilotReview, setAwaitCopilotReview] = useState(seed.await_copilot_review);
  // No card_defaults seed: merge_pr is human-only and always starts off at create.
  const [mergePR, setMergePR] = useState(false);
  const [baseBranch, setBaseBranch] = useState('');
  // 0 = off / unset; the AutomationCheckboxes selector (create mode, agent
  // backend only) writes here, and buildInput forwards non-zero values.
  const [bestOfN, setBestOfN] = useState(0);
  const [maxCapability, setMaxCapability] = useState(seed.max_capability);
  const [mobParticipants, setMobParticipants] = useState(seed.mob_participants);
  const [mobPhases, setMobPhases] = useState<string[]>(seed.mob_phases);
  const [mobGuests, setMobGuests] = useState<string[]>([]);
  // null = inherit project default, [] = mount none, [...] = specific list.
  const [skills, setSkills] = useState<string[] | null>(null);
  const [assignee, setAssignee] = useState('');
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
    (): CreateCardInput => ({
      title: title.trim(),
      type,
      priority,
      labels: labels.length > 0 ? labels : undefined,
      parent: parent || undefined,
      body: body || undefined,
      autonomous,
      // Per-role model pins for the agent backend. Empty = "selector decides";
      // only forward a non-empty override.
      model_orchestrator: modelOrchestrator || undefined,
      model_coder: modelCoder || undefined,
      model_reviewer: modelReviewer || undefined,
      // Best-of-N - only forwarded when the user picked a value (0 means
      // "off / unset" and is omitted to keep the input clean).
      best_of_n: bestOfN || undefined,
      max_capability: maxCapability,
      mob_participants: mobParticipants,
      mob_phases: mobParticipants >= 2 && mobPhases.length ? mobPhases : undefined,
      mob_guests: mobGuests.length ? mobGuests : undefined,
      // Always explicit booleans/counts: the server fills absent fields from
      // the project's card_defaults, so an unticked box must arrive as false,
      // never as an omission.
      create_pr: createPR,
      await_ci: awaitCI,
      await_copilot_review: awaitCopilotReview,
      merge_pr: mergePR,
      base_branch: baseBranch || undefined,
      // null = inherit project default; only forward an explicit override.
      skills: skills === null ? undefined : skills,
      assignee: assignee || undefined,
    }),
    [title, type, priority, labels, parent, body, autonomous, modelOrchestrator, modelCoder, modelReviewer, bestOfN, maxCapability, mobParticipants, mobPhases, mobGuests, createPR, awaitCI, awaitCopilotReview, mergePR, baseBranch, skills, assignee],
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
      await onCreate(buildInput(), { run: false });
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
      await onCreate(buildInput(), { run: true, interactive: !autonomous });
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
      createPR,
      awaitCI,
      awaitCopilotReview,
      mergePR,
      baseBranch,
      bestOfN,
      maxCapability,
      mobParticipants,
      mobPhases,
      mobGuests,
      skills,
      assignee,
      isSubmitting,
      pendingTemplate,
      setTitle,
      setPriority,
      setLabels,
      setAutonomous,
      setModelOrchestrator,
      setModelCoder,
      setModelReviewer,
      setCreatePR,
      setAwaitCI,
      setAwaitCopilotReview,
      setMergePR,
      setBaseBranch,
      setBestOfN,
      setMaxCapability,
      setMobParticipants,
      setMobPhases,
      setMobGuests,
      setSkills,
      setAssignee,
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
