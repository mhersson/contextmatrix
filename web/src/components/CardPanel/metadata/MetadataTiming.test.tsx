import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ActivityEntry, Card } from '../../../types';
import { duration, formatStamp } from '../../../lib/cardTiming';
import { displayState } from '../../../lib/stateLabels';
import { MetadataTiming } from './MetadataTiming';

const T0 = Date.parse('2026-01-01T12:00:00Z');
const iso = (offsetMin: number) => new Date(T0 + offsetMin * 60_000).toISOString();
// Stamps render in the viewer's locale and time zone.
const at = (offsetMin: number) => formatStamp(iso(offsetMin));
const entry = (agent: string, min: number, action: string, message = ''): ActivityEntry => ({ agent, ts: iso(min), action, message });

// In progress since 11:00; worker-3 held it 11:10-11:30, worker-7 since 11:46.
const worked: Card = {
  id: 'TEST-001',
  title: 'Sort the spice rack',
  project: 'test',
  type: 'task',
  state: 'in_progress',
  priority: 'medium',
  created: iso(-120),
  updated: iso(0),
  body: '',
  assigned_agent: 'worker-7',
  activity_log: [
    entry('human:alice', -60, 'state_changed', 'todo -> in_progress'),
    entry('worker-3', -50, 'claimed'),
    entry('worker-3', -30, 'released'),
    entry('worker-7', -14, 'claimed'),
  ],
};

const done: Card = {
  ...worked,
  state: 'done',
  assigned_agent: undefined,
  activity_log: [...worked.activity_log!, entry('worker-7', -5, 'released'), entry('worker-7', -4, 'state_changed', 'review -> done')],
};

const claims = () => within(screen.getByTestId('timing-claims')).getAllByRole('listitem');

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(T0);
});
afterEach(() => {
  vi.useRealTimers();
});

describe('MetadataTiming', () => {
  it('shows the state and how long, then every claim with the total, ticking', () => {
    render(<MetadataTiming card={worked} />);
    const state = screen.getByTestId('timing-state');
    expect(state).toHaveTextContent(displayState('in_progress'));
    expect(state).toHaveTextContent(duration(60 * 60));
    expect(state).toHaveTextContent(at(-60));

    expect(screen.getByTestId('timing-claims')).toHaveTextContent(duration(34 * 60));
    const [first, held] = claims();
    expect(claims()).toHaveLength(2);
    for (const text of ['worker-3', at(-50), at(-30), duration(20 * 60)]) expect(first).toHaveTextContent(text);
    for (const text of ['worker-7', at(-14), duration(14 * 60)]) expect(held).toHaveTextContent(text);

    act(() => { vi.advanceTimersByTime(10 * 60_000); });
    expect(state).toHaveTextContent(duration(70 * 60));
    expect(screen.getByTestId('timing-claims')).toHaveTextContent(duration(44 * 60));
    expect(claims()[1]).toHaveTextContent(duration(24 * 60));
  });

  it('shows when a finished card got there, not how long', () => {
    render(<MetadataTiming card={done} />);
    const state = screen.getByTestId('timing-state');
    expect(state).toHaveTextContent(displayState('done'));
    expect(state).toHaveTextContent(at(-4));
    expect(state).not.toHaveTextContent(duration(4 * 60));
    expect(claims()).toHaveLength(2);
    expect(claims()[1]).toHaveTextContent(duration(9 * 60));
  });

  it('keeps a held claim on a finished card ticking', () => {
    render(<MetadataTiming card={{ ...done, assigned_agent: 'worker-7', activity_log: worked.activity_log }} />);
    expect(claims()[1]).toHaveTextContent(duration(14 * 60));
    act(() => { vi.advanceTimersByTime(10 * 60_000); });
    expect(claims()[1]).toHaveTextContent(duration(24 * 60));
  });

  it('leaves the claims row out when no claim is recorded', () => {
    render(<MetadataTiming card={{ ...worked, assigned_agent: undefined, activity_log: [] }} />);
    expect(screen.getByTestId('timing-state')).toHaveTextContent(duration(120 * 60));
    expect(screen.queryByTestId('timing-claims')).not.toBeInTheDocument();
  });
});
