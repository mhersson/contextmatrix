import { describe, it, expect } from 'vitest';
import { mergeDraft } from './useRailSync';
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
});
