import { useCallback, useState } from 'react';
import type { BlueprintView } from '../../types';
import type { HeldDraft } from './draft';
import { currentStep, sliceOf } from './stages';

export type SheetLayout = 'split' | 'overlay' | 'phone';

export interface SheetPin {
  stage: string;
  slice: string;
  version: number | null;
}

interface Signal {
  artifacts: string;
  draft: string;
}

function signalOf(view: BlueprintView, draft: HeldDraft | null): Signal {
  return {
    artifacts: view.artifacts.map((a) => `${a.stage}/${sliceOf(a)}/${a.version}`).sort().join(','),
    draft: draft ? `${draft.stage}/${draft.slice}/${draft.version}` : '',
  };
}

function grew(before: string, after: string): boolean {
  const seen = new Set(before.split(','));
  return after.split(',').some((key) => key !== '' && !seen.has(key));
}

/**
 * Whether the sheet is open, what it is pinned to, and the something-new dot.
 * A new artifact version or the start of a draft opens a closed sheet, except
 * over the transcript while a question or round waits; then only the dot shows. A pin
 * holds until the current step (stage and slice) changes.
 */
export function useSheetState(view: BlueprintView | null, draft: HeldDraft | null, layout: SheetLayout) {
  const [open, setOpen] = useState(layout === 'split');
  const [hasNew, setHasNew] = useState(false);
  const [pin, setPin] = useState<(SheetPin & { at: string; picked: string }) | null>(null);
  const [prevLayout, setPrevLayout] = useState(layout);
  const [seen, setSeen] = useState<Signal | null>(null);

  if (layout !== prevLayout) {
    setPrevLayout(layout);
    setOpen(layout === 'split');
  }

  const step = view ? currentStep(view) : null;
  const at = step ? `${step.stage}/${sliceOf(step)}` : '';
  if (pin && pin.at !== at) setPin(null);

  if (view) {
    const signal = signalOf(view, draft);
    if (!seen || signal.artifacts !== seen.artifacts || signal.draft !== seen.draft) {
      setSeen(signal);
      const fresh =
        seen !== null && (grew(seen.artifacts, signal.artifacts) || (signal.draft !== '' && signal.draft !== seen.draft));
      // Over the transcript a fresh version must not cover a waiting question or the docked round.
      const waits = view.open?.kind === 'question' || view.open?.kind === 'round';
      if (fresh && (layout === 'split' || !waits)) {
        setOpen(true);
        setHasNew(false);
      } else if (fresh && !open) {
        setHasNew(true);
      }
    }
  }

  const openSheet = useCallback(() => {
    setOpen(true);
    setHasNew(false);
  }, []);
  const close = useCallback(() => setOpen(false), []);
  const toggle = useCallback(() => {
    setOpen((o) => !o);
    setHasNew(false);
  }, []);
  const show = useCallback(
    (stage: string, slice: string, version: number | null, remember: string = slice) => {
      // picked is the slice the person chose: a whole-blueprint pick keeps it, a stage tab's fallback never sets it.
      setPin((p) => ({ stage, slice, version, at, picked: remember || (p && p.at === at ? p.picked : '') }));
      setOpen(true);
      setHasNew(false);
    },
    [at],
  );

  return {
    open,
    hasNew,
    pin: pin ? { stage: pin.stage, slice: pin.slice, version: pin.version } : null,
    openSheet,
    close,
    toggle,
    show,
    picked: pin?.picked ?? '',
  };
}
