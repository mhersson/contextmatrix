import { describe, expect, it } from 'vitest';
import type { PlaybookDetail, PlaybookEntry, PlaybookSummary } from '../../types';
import {
  arrayMoveLocal,
  describeRun,
  frontierIndex,
  isFullyComplete,
  isRunActive,
  runEntryIndex,
  runStatusChip,
  segmentColor,
} from './playbookUtils';

function entry(complete: boolean): PlaybookEntry {
  return { id: `e-${complete}`, type: 'manual', complete };
}

function summary(complete: number, total: number): PlaybookSummary {
  return {
    id: 'p',
    title: 'Playbook',
    complete,
    total,
    segments: [],
    projects: 0,
    updated_at: '2026-08-20T09:00:00Z',
  };
}

describe('frontierIndex', () => {
  it.each<[string, PlaybookEntry[], number]>([
    ['empty list', [], -1],
    ['all complete', [entry(true), entry(true)], -1],
    ['mixed', [entry(true), entry(false), entry(false)], 1],
  ])('%s', (_label, entries, expected) => {
    expect(frontierIndex(entries)).toBe(expected);
  });
});

describe('isFullyComplete', () => {
  it.each([
    ['0/0', 0, 0, false],
    ['2/3', 2, 3, false],
    ['3/3', 3, 3, true],
  ] as const)('%s', (_label, complete, total, expected) => {
    expect(isFullyComplete(summary(complete, total))).toBe(expected);
  });
});

describe('segmentColor', () => {
  it.each([
    ['complete', 'var(--green)'],
    ['active', 'var(--aqua)'],
    ['missing', 'var(--bg-red)'],
    ['pending', 'var(--bg2)'],
  ])('%s -> %s', (seg, expected) => {
    expect(segmentColor(seg)).toBe(expected);
  });
});

describe('arrayMoveLocal', () => {
  it('moves an item from one index to another without mutating the input', () => {
    const input = ['a', 'b', 'c'];
    const result = arrayMoveLocal(input, 0, 2);
    expect(result).toEqual(['b', 'c', 'a']);
    expect(input).toEqual(['a', 'b', 'c']);
  });

  it('moves an item backward', () => {
    expect(arrayMoveLocal(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
  });
});

function runnableDetail(): PlaybookDetail {
  return {
    id: 'roll', title: 'Roll', created_at: '2026-09-06T10:00:00Z', updated_at: '2026-09-06T10:00:00Z',
    complete: 0, total: 3, runnable: true, branch: 'playbook/roll',
    entries: [
      { id: 'e1', type: 'card', project: 'alpha', card: 'ALPHA-1', card_title: 'First', card_state: 'todo', complete: false },
      { id: 'e2', type: 'manual', text: 'deploy', complete: false },
      { id: 'e3', type: 'card', project: 'alpha', card: 'ALPHA-2', card_title: 'Second', card_state: 'todo', complete: false },
    ],
  };
}

describe('run helpers', () => {
  it('isRunActive is true only for running and waiting', () => {
    expect(isRunActive(undefined)).toBe(false);
    expect(isRunActive({ status: 'running', started_at: 'x', updated_at: 'x' })).toBe(true);
    expect(isRunActive({ status: 'waiting', started_at: 'x', updated_at: 'x' })).toBe(true);
    expect(isRunActive({ status: 'stopped', started_at: 'x', updated_at: 'x' })).toBe(false);
    expect(isRunActive({ status: 'completed', started_at: 'x', updated_at: 'x' })).toBe(false);
  });

  it('runStatusChip maps every status to the house colours', () => {
    expect(runStatusChip('running')).toEqual({ label: 'running', bg: 'var(--bg-aqua)', color: 'var(--aqua)' });
    expect(runStatusChip('waiting')).toEqual({ label: 'waiting for you', bg: 'var(--bg-yellow)', color: 'var(--yellow)' });
    expect(runStatusChip('stopped')).toEqual({ label: 'stopped', bg: 'var(--bg1)', color: 'var(--grey1)' });
    expect(runStatusChip('completed')).toEqual({ label: 'completed', bg: 'var(--bg-green)', color: 'var(--green)' });
  });

  it('runEntryIndex resolves the run entry', () => {
    const d = runnableDetail();
    expect(runEntryIndex(d)).toBe(-1);
    d.run = { status: 'running', started_at: 'x', updated_at: 'x', entry: 'e3' };
    expect(runEntryIndex(d)).toBe(2);
    d.run.entry = 'gone';
    expect(runEntryIndex(d)).toBe(-1);
  });

  it('describeRun names the current card and position, or the reason', () => {
    const d = runnableDetail();
    expect(describeRun(d)).toBe('Not started');
    d.run = { status: 'running', started_at: 'x', updated_at: 'x', entry: 'e3' };
    expect(describeRun(d)).toBe('Running ALPHA-2, 3 of 3');
    d.run = { status: 'waiting', started_at: 'x', updated_at: 'x', entry: 'e2', reason: 'awaiting check-off' };
    expect(describeRun(d)).toBe('Waiting for you: awaiting check-off');
    d.run = { status: 'stopped', started_at: 'x', updated_at: 'x', reason: 'stopped by human:alice' };
    expect(describeRun(d)).toBe('Stopped');
    d.run = { status: 'completed', started_at: 'x', updated_at: 'x' };
    expect(describeRun(d)).toBe('Completed');
  });
});
