import { useCallback, useState } from 'react';
import { EMPTY_DRAFT, type RoundDraft } from './roundDraft';

/** Unsent picks and decisions plus each question's own-answer text; they live as long as the docked block. */
export function useRoundDraft() {
  const [draft, setDraft] = useState<RoundDraft>(EMPTY_DRAFT);
  const [own, setOwn] = useState<Readonly<Record<number, string>>>({});

  const pickOption = useCallback((number: number, option: number, title: string) => {
    setDraft((d) => ({ ...d, picks: { ...d.picks, [number]: { kind: 'option', option, title } } }));
  }, []);

  const typeOwn = useCallback((number: number, text: string) => {
    setOwn((o) => ({ ...o, [number]: text }));
    setDraft((d) => {
      const picks = { ...d.picks };
      if (text.trim() === '') delete picks[number];
      else picks[number] = { kind: 'own', text };
      return { ...d, picks };
    });
  }, []);

  const decide = useCallback((id: string, accept: boolean) => {
    setDraft((d) => ({ ...d, decisions: { ...d.decisions, [id]: accept } }));
  }, []);

  return { draft, own, pickOption, typeOwn, decide };
}
