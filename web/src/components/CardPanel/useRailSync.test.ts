import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useState } from 'react';
import { mergeDraft, useRailSync } from './useRailSync';
import type { Card } from '../../types';

const base = { id: 'T-1', title: 'old', body: 'text', depends_on: [] as string[] } as Card;

describe('mergeDraft', () => {
  it('keeps the fields the user edited and takes everything else from the server', () => {
    const draft = { ...base, title: 'typed but unsaved' };
    const next = { ...base, depends_on: ['T-2'], updated: '2026-09-02T10:00:00Z' } as Card;

    const merged = mergeDraft(draft, base, next);

    expect(merged.title).toBe('typed but unsaved');
    expect(merged.depends_on).toEqual(['T-2']);
    expect(merged.updated).toBe('2026-09-02T10:00:00Z');
  });

  it('is the server card when nothing was edited', () => {
    const next = { ...base, title: 'renamed by the agent' } as Card;
    expect(mergeDraft(base, base, next)).toEqual(next);
  });

  it('follows the server for an array field the draft only re-created with the same content', () => {
    // depends_on and labels both get a fresh array reference here even though
    // their content matches prev - this must not read as a user edit.
    const draft = {
      ...base,
      depends_on: [...(base.depends_on ?? [])],
      labels: ['a'],
    };
    const prev = { ...base, labels: ['a'] } as Card;
    const next = { ...base, depends_on: ['T-3'], labels: ['a', 'b'] } as Card;

    const merged = mergeDraft(draft, prev, next);

    expect(merged.depends_on).toEqual(['T-3']);
    expect(merged.labels).toEqual(['a', 'b']);
  });

  it('keeps a genuinely edited array (different content) from the draft', () => {
    const draft = { ...base, labels: ['a', 'x'] };
    const prev = { ...base, labels: ['a'] } as Card;
    const next = { ...base, labels: ['a', 'b'] } as Card;

    expect(mergeDraft(draft, prev, next).labels).toEqual(['a', 'x']);
  });
});

describe('useRailSync - server refresh against an open draft', () => {
  // Drives the hook the way CardPanel does: the panel owns the editedCard
  // state and hands its setter in, so this pins the merge at the call site
  // rather than only in mergeDraft.
  const useHarness = (card: Card) => {
    const [editedCard, setEditedCard] = useState<Card>(card);
    useRailSync(card, false, 'automation', setEditedCard);
    return { editedCard, setEditedCard };
  };

  it('keeps the typed title and takes the server depends_on', () => {
    const c1 = { ...base } as Card;
    const { result, rerender } = renderHook(({ card }: { card: Card }) => useHarness(card), {
      initialProps: { card: c1 },
    });

    act(() => {
      result.current.setEditedCard((draft) => ({ ...draft, title: 'typed but unsaved' }));
    });

    // Same card id, new object reference from the server (an SSE refresh).
    const c2 = { ...c1, depends_on: ['T-2'] } as Card;
    rerender({ card: c2 });

    expect(result.current.editedCard.title).toBe('typed but unsaved');
    expect(result.current.editedCard.depends_on).toEqual(['T-2']);
  });
});
