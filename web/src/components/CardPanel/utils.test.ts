import { describe, it, expect } from 'vitest';
import type { Card, ProjectConfig } from '../../types';
import {
  buildCardPatch,
  isCardDirty,
  isWorkerAttached,
  isSafeHttpUrl,
  primaryAction,
} from './utils';

function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: 'TEST-001',
    title: 'Test',
    project: 'test',
    type: 'task',
    state: 'todo',
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    autonomous: false,
    feature_branch: false,
    create_pr: false,
    ...overrides,
  };
}

function makeConfig(overrides: Partial<ProjectConfig> = {}): ProjectConfig {
  return {
    name: 'Test',
    prefix: 'TEST',
    next_id: 2,
    states: ['todo', 'in_progress', 'review', 'done', 'blocked', 'stalled', 'not_planned'],
    types: ['task'],
    priorities: ['low', 'medium', 'high'],
    transitions: {
      todo: ['in_progress', 'blocked'],
      in_progress: ['review'],
      review: ['done', 'in_progress'],
      done: ['todo'],
      blocked: ['todo'],
      stalled: ['todo'],
    },
    ...overrides,
  };
}

describe('isSafeHttpUrl', () => {
  it.each([
    ['http://example.com', true],
    ['https://example.com/pr/1', true],
    ['javascript:alert(1)', false],
    ['data:text/html,<script>alert(1)</script>', false],
    ['vbscript:msgbox(1)', false],
    ['ftp://example.com', false],
    ['not a url', false],
    ['', false],
  ])('isSafeHttpUrl(%j) === %s', (input, expected) => {
    expect(isSafeHttpUrl(input)).toBe(expected);
  });
});

describe('isWorkerAttached', () => {
  it('returns true when worker_status is queued', () => {
    expect(isWorkerAttached(makeCard({ worker_status: 'queued' }), null)).toBe(true);
  });

  it('returns true when worker_status is running', () => {
    expect(isWorkerAttached(makeCard({ worker_status: 'running' }), null)).toBe(true);
  });

  it('returns true when another agent holds the claim', () => {
    expect(
      isWorkerAttached(makeCard({ assigned_agent: 'agent:other' }), 'human:me'),
    ).toBe(true);
  });

  it('returns false when the current human holds the claim (self-claim)', () => {
    expect(
      isWorkerAttached(makeCard({ assigned_agent: 'human:me' }), 'human:me'),
    ).toBe(false);
  });

  it('returns true when a non-human current agent matches (only humans self-own)', () => {
    expect(
      isWorkerAttached(makeCard({ assigned_agent: 'agent:me' }), 'agent:me'),
    ).toBe(true);
  });

  it('returns false when no worker and no claim', () => {
    expect(isWorkerAttached(makeCard(), null)).toBe(false);
  });
});

describe('primaryAction', () => {
  it('returns stop when worker is running', () => {
    expect(primaryAction(makeCard({ worker_status: 'running' }), false, makeConfig(), false))
      .toEqual({ kind: 'stop' });
  });

  it('returns stop when worker is queued', () => {
    expect(primaryAction(makeCard({ worker_status: 'queued' }), false, makeConfig(), false))
      .toEqual({ kind: 'stop' });
  });

  it('returns "Mark done" for a review card whose config allows review→done', () => {
    expect(primaryAction(makeCard({ state: 'review' }), false, makeConfig(), false))
      .toEqual({ kind: 'transition', label: 'Mark done', targetState: 'done' });
  });

  it('returns "Unblock" for a blocked card', () => {
    expect(primaryAction(makeCard({ state: 'blocked' }), false, makeConfig(), false))
      .toEqual({ kind: 'transition', label: 'Unblock', targetState: 'todo' });
  });

  it('returns "Resume" for a stalled card', () => {
    expect(primaryAction(makeCard({ state: 'stalled' }), false, makeConfig(), false))
      .toEqual({ kind: 'transition', label: 'Resume', targetState: 'todo' });
  });

  it('returns "Re-open" for a done card', () => {
    expect(primaryAction(makeCard({ state: 'done' }), false, makeConfig(), false))
      .toEqual({ kind: 'transition', label: 'Re-open', targetState: 'todo' });
  });

  it('returns run HITL when canRun=true and autonomous=false', () => {
    expect(primaryAction(makeCard(), false, makeConfig(), true))
      .toEqual({ kind: 'run', autonomous: false });
  });

  it('returns run Auto when canRun=true and autonomous=true', () => {
    expect(primaryAction(makeCard(), true, makeConfig(), true))
      .toEqual({ kind: 'run', autonomous: true });
  });

  it('returns null when no curated action matches', () => {
    // in_progress, no worker, canRun=false → no primary action
    expect(primaryAction(makeCard({ state: 'in_progress' }), false, makeConfig(), false))
      .toBeNull();
  });

  it('does NOT return "Mark done" when review→done is not in the transitions map', () => {
    const config = makeConfig({ transitions: { review: ['in_progress'] } });
    expect(primaryAction(makeCard({ state: 'review' }), false, config, false)).toBeNull();
  });
});

describe('isCardDirty', () => {
  it('returns false when edited equals original', () => {
    const c = makeCard();
    expect(isCardDirty(c, c)).toBe(false);
  });

  it.each([
    ['title', { title: 'different' }],
    ['type', { type: 'feature' }],
    ['state', { state: 'in_progress' }],
    ['priority', { priority: 'high' }],
    ['body', { body: 'new content' }],
    ['labels', { labels: ['bug'] }],
    ['autonomous', { autonomous: true }],
    ['best_of_n', { best_of_n: 3 }],
    ['feature_branch', { feature_branch: true }],
    ['create_pr', { create_pr: true }],
    ['vetted', { vetted: true }],
    ['base_branch', { base_branch: 'main' }],
  ])('returns true when %s changed', (_field, patch) => {
    const original = makeCard();
    const edited = { ...original, ...patch };
    expect(isCardDirty(edited, original)).toBe(true);
  });

  it('treats undefined and false as equal for boolean fields', () => {
    const a = makeCard({ autonomous: undefined });
    const b = makeCard({ autonomous: false });
    expect(isCardDirty(a, b)).toBe(false);
  });

  it('treats undefined and empty string as equal for base_branch', () => {
    const a = makeCard({ base_branch: undefined });
    const b = makeCard({ base_branch: '' });
    expect(isCardDirty(a, b)).toBe(false);
  });

  it('treats undefined and 0 as equal for best_of_n', () => {
    const a = makeCard({ best_of_n: undefined });
    const b = makeCard({ best_of_n: 0 });
    expect(isCardDirty(a, b)).toBe(false);
  });

  it('returns false when labels are equal in different array references', () => {
    const a = makeCard({ labels: ['bug', 'p1'] });
    const b = makeCard({ labels: ['bug', 'p1'] });
    expect(isCardDirty(a, b)).toBe(false);
  });
});

describe('buildCardPatch', () => {
  it('returns an empty object when nothing changed', () => {
    const c = makeCard();
    expect(buildCardPatch(c, c)).toEqual({});
  });

  it('returns only the changed fields', () => {
    const original = makeCard();
    const edited = { ...original, title: 'new title', priority: 'high' };
    const patch = buildCardPatch(edited, original);
    expect(patch).toEqual({ title: 'new title', priority: 'high' });
  });

  it('includes type when it changed', () => {
    const original = makeCard({ type: 'task' });
    const edited = { ...original, type: 'feature' };
    expect(buildCardPatch(edited, original)).toEqual({ type: 'feature' });
  });

  it('returns labels when they differ using deep equality (not JSON.stringify drift)', () => {
    const original = makeCard({ labels: ['a', 'b'] });
    const edited = { ...original, labels: ['a', 'b', 'c'] };
    expect(buildCardPatch(edited, original)).toEqual({ labels: ['a', 'b', 'c'] });
  });

  it('does NOT include labels when they are equal (different array references)', () => {
    const original = makeCard({ labels: ['a', 'b'] });
    const edited = { ...original, labels: ['a', 'b'] };
    expect(buildCardPatch(edited, original)).toEqual({});
  });

  it('coerces base_branch undefined → empty string in the diff', () => {
    const original = makeCard({ base_branch: 'main' });
    const edited = { ...original, base_branch: undefined };
    expect(buildCardPatch(edited, original)).toEqual({ base_branch: '' });
  });

  it('includes best_of_n when it changed', () => {
    const original = makeCard();
    const edited = { ...original, best_of_n: 3 };
    expect(buildCardPatch(edited, original)).toEqual({ best_of_n: 3 });
  });

  it('coerces best_of_n undefined → 0 in the diff (disabling)', () => {
    const original = makeCard({ best_of_n: 3 });
    const edited = { ...original, best_of_n: undefined };
    expect(buildCardPatch(edited, original)).toEqual({ best_of_n: 0 });
  });
});
