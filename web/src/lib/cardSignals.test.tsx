import { describe, it, expect } from 'vitest';
import type { Card } from '../types';
import { cardSignals } from './cardSignals';
import { parkedResetLabel } from './subscription';

function baseCard(overrides: Partial<Card> = {}): Card {
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
    ...overrides,
  };
}

const thu14 = new Date(2026, 10, 12, 14, 0).toISOString();

describe('cardSignals', () => {
  it('names the usage-limit reset on a parked card, and says when it has passed', () => {
    const card = baseCard({ worker_status: 'parked', park_resets_at: thu14 });
    const at = (now: number) => cardSignals(card, false, now).find((s) => s.key === 'worker')?.label;
    const before = Date.parse(thu14) - 60_000;
    const after = Date.parse(thu14) + 60_000;
    expect(at(before)).toBe(parkedResetLabel(thu14, before));
    expect(at(after)).toBe(parkedResetLabel(thu14, after));
    expect(at(before)).not.toBe(at(after));
  });

  it('shows no reset on a plain parked card', () => {
    const signals = cardSignals(baseCard({ worker_status: 'parked' }));
    const label = signals.find((s) => s.key === 'worker')?.label;
    expect(parkedResetLabel(undefined)).toBeNull();
    expect(label).toBeTruthy();
    expect(label).not.toBe(parkedResetLabel(thu14, 0));
    expect(label).not.toBe(parkedResetLabel(thu14, Date.parse(thu14) * 2));
  });

  it('replaces the membership signal with a queued-in-playbook signal while the run is active', () => {
    const card = baseCard({ in_playbooks: ['roll'], playbook_lock: { id: 'roll', title: 'Roll', run_status: 'running' } });
    const signals = cardSignals(card);
    const run = signals.find((s) => s.key === 'playbook-run');
    expect(run?.label).toBe('Queued in playbook Roll');
    expect(run?.color).toBe('var(--yellow)');
    expect(signals.find((s) => s.key === 'playbook')).toBeUndefined();
  });

  it.each(['stopped', 'completed'] as const)('keeps the membership signal when the lock run is %s', (runStatus) => {
    const card = baseCard({ in_playbooks: ['roll'], playbook_lock: { id: 'roll', title: 'Roll', run_status: runStatus } });
    const signals = cardSignals(card);
    expect(signals.find((s) => s.key === 'playbook')?.label).toBe('In playbook: roll');
    expect(signals.find((s) => s.key === 'playbook-run')).toBeUndefined();
  });

  it('keeps the membership signal when the lock has no active run', () => {
    const card = baseCard({ in_playbooks: ['roll'], playbook_lock: { id: 'roll', title: 'Roll' } });
    const signals = cardSignals(card);
    expect(signals.find((s) => s.key === 'playbook')?.label).toBe('In playbook: roll');
    expect(signals.find((s) => s.key === 'playbook-run')).toBeUndefined();
  });
});

describe('cardSignals sentinel mark', () => {
  const marked = baseCard({ sentinel: { action: 'healing', label: 'Sentinel is healing this card' } });

  it('adds the purple shield with the mark label', () => {
    const s = cardSignals(marked, true).find((x) => x.key === 'sentinel');
    expect(s?.label).toBe('Sentinel is healing this card');
    expect(s?.color).toBe('var(--purple)');
  });

  it('shows the created mark\'s label', () => {
    const created = baseCard({ sentinel: { action: 'created', label: 'Sentinel created this card from a run\'s friction' } });
    expect(cardSignals(created, true).find((x) => x.key === 'sentinel')?.label).toBe(
      'Sentinel created this card from a run\'s friction',
    );
  });

  it('hides it while Sentinel is off', () => {
    expect(cardSignals(marked, false).some((x) => x.key === 'sentinel')).toBe(false);
  });

  it('hides it without a mark', () => {
    expect(cardSignals(baseCard(), true).some((x) => x.key === 'sentinel')).toBe(false);
  });
});
