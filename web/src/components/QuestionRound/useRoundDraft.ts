import { useCallback, useEffect, useState } from 'react';
import type { Round } from './parseRound';
import { EMPTY_DRAFT, storedAnswer, storedDecision, type RoundDraft } from './roundDraft';
import { clearDraft, loadDraft, saveDraft, type DraftBases, type DraftScope } from './roundDraftStore';

const NO_BASES: DraftBases = { picks: {}, decisions: {} };

/**
 * Unsent picks and decisions plus each question's own-answer text. With a scope they are kept in this browser and
 * restored when the block mounts again, under the validity rules of roundDraftStore.
 */
export function useRoundDraft(round: Round, scope?: DraftScope) {
  const [restored] = useState(() => (scope ? loadDraft(scope, round) : null));
  const [draft, setDraft] = useState<RoundDraft>(restored?.draft ?? EMPTY_DRAFT);
  const [own, setOwn] = useState<Readonly<Record<number, string>>>(restored?.own ?? {});
  const [bases, setBases] = useState<DraftBases>(restored?.bases ?? NO_BASES);
  const sessionID = scope?.sessionID;
  const promptId = scope?.promptId;

  useEffect(() => {
    if (sessionID !== undefined && promptId !== undefined) saveDraft({ sessionID, promptId }, round, { draft, own, bases });
  }, [sessionID, promptId, round, draft, own, bases]);

  const pickBase = useCallback(
    (number: number) => {
      const q = round.questions.find((x) => x.number === number);
      setBases((b) => ({ ...b, picks: { ...b.picks, [number]: q ? storedAnswer(round, q) : null } }));
    },
    [round],
  );

  const pickOption = useCallback(
    (number: number, option: number, title: string) => {
      setDraft((d) => ({ ...d, picks: { ...d.picks, [number]: { kind: 'option', option, title } } }));
      pickBase(number);
    },
    [pickBase],
  );

  const typeOwn = useCallback(
    (number: number, text: string) => {
      setOwn((o) => ({ ...o, [number]: text }));
      setDraft((d) => {
        const picks = { ...d.picks };
        if (text.trim() === '') delete picks[number];
        else picks[number] = { kind: 'own', text };
        return { ...d, picks };
      });
      pickBase(number);
    },
    [pickBase],
  );

  const decide = useCallback(
    (id: string, accept: boolean) => {
      setDraft((d) => ({ ...d, decisions: { ...d.decisions, [id]: accept } }));
      setBases((b) => ({ ...b, decisions: { ...b.decisions, [id]: storedDecision(round, id) } }));
    },
    [round],
  );

  const clearSaved = useCallback(() => {
    if (sessionID !== undefined && promptId !== undefined) clearDraft({ sessionID, promptId });
  }, [sessionID, promptId]);

  return { draft, own, pickOption, typeOwn, decide, clearSaved };
}
