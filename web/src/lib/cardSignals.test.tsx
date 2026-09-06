import { describe, it, expect } from 'vitest';
import type { Card } from '../types';
import { cardSignals } from './cardSignals';

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

describe('cardSignals', () => {
  it('replaces the membership signal with a queued-in-playbook signal while the run is active', () => {
    const card = baseCard({ in_playbooks: ['roll'], playbook_lock: { id: 'roll', title: 'Roll', run_status: 'running' } });
    const signals = cardSignals(card);
    const run = signals.find((s) => s.key === 'playbook-run');
    expect(run?.label).toBe('Queued in playbook Roll');
    expect(run?.color).toBe('var(--yellow)');
    expect(signals.find((s) => s.key === 'playbook')).toBeUndefined();
  });

  it('keeps the membership signal when the lock has no active run', () => {
    const card = baseCard({ in_playbooks: ['roll'], playbook_lock: { id: 'roll', title: 'Roll' } });
    const signals = cardSignals(card);
    expect(signals.find((s) => s.key === 'playbook')?.label).toBe('In playbook: roll');
    expect(signals.find((s) => s.key === 'playbook-run')).toBeUndefined();
  });
});
