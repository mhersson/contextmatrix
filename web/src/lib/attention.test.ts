import { describe, it, expect } from 'vitest';
import type { AttentionItem } from '../types';
import { attentionHref, projectCounts, tabTitle } from './attention';

const base = { title: 't', reason: 'r', since: '2026-01-01T00:00:00Z' };
const item = (o: Partial<AttentionItem> & Pick<AttentionItem, 'kind' | 'id'>): AttentionItem => ({ ...base, ...o });

describe('attentionHref', () => {
  it.each([
    [item({ kind: 'blueprint', id: 's1' }), '/chat/s1'],
    [item({ kind: 'playbook', id: 'pb' }), '/playbooks/pb'],
    [item({ kind: 'card', id: 'AB-1', project: 'alpha' }), '/projects/alpha?card=AB-1'],
    [item({ kind: 'sentinel', id: 'AB-2', project: 'alpha' }), '/projects/alpha?card=AB-2&tab=sentinel'],
  ])('opens %#', (it, href) => {
    expect(attentionHref(it)).toBe(href);
  });
});

describe('tabTitle', () => {
  it('leads with the count only while something waits', () => {
    expect(tabTitle(2)).toMatch(/^\(2\) /);
    expect(tabTitle(0)).not.toMatch(/\d/);
    expect(tabTitle(2).endsWith(tabTitle(0))).toBe(true);
  });
});

describe('projectCounts', () => {
  it('counts card and sentinel items per project, not blueprints or playbooks', () => {
    const m = projectCounts([
      item({ kind: 'card', id: '1', project: 'a' }),
      item({ kind: 'sentinel', id: '2', project: 'a' }),
      item({ kind: 'card', id: '3', project: 'b' }),
      item({ kind: 'blueprint', id: '4' }),
      item({ kind: 'playbook', id: '5' }),
    ]);
    expect(Object.fromEntries(m)).toEqual({ a: 2, b: 1 });
  });
});
