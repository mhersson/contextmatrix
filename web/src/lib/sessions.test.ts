import { describe, it, expect } from 'vitest';
import { sessionKind, isLive, sessionProjects, sessionCostUSD, formatSessionCost } from './sessions';
import type { ChatSession } from '../types';

function sess(o: Partial<ChatSession> = {}): ChatSession {
  return {
    id: 'S1',
    title: 't',
    status: 'cold',
    created_at: '2026-07-01T10:00:00Z',
    last_active: '2026-07-02T10:00:00Z',
    created_by: 'human:alice',
    ...o,
  };
}

describe('sessionCostUSD', () => {
  it('uses the estimate for a chat', () => {
    expect(sessionCostUSD(sess({ estimated_cost_usd: 0.5 }))).toBe(0.5);
    expect(sessionCostUSD(sess())).toBe(0);
  });
  it('uses the recorded spend for a blueprint, never the estimate', () => {
    expect(sessionCostUSD(sess({ kind: 'architect', spent_usd: 2, estimated_cost_usd: 9 }))).toBe(2);
    expect(sessionCostUSD(sess({ kind: 'architect', estimated_cost_usd: 9 }))).toBeUndefined();
    expect(sessionCostUSD(sess({ kind: 'architect', spent_usd: 0, estimated_cost_usd: 9 }))).toBe(0);
  });
});

describe('formatSessionCost', () => {
  it('is a hyphen exactly when the cost is undefined', () => {
    expect(formatSessionCost(sess({ kind: 'architect', estimated_cost_usd: 9 }))).toBe('-');
    expect(formatSessionCost(sess({ kind: 'architect', spent_usd: 2 }))).toBe('$2.00');
    expect(formatSessionCost(sess({ kind: 'architect', spent_usd: 0 }))).not.toBe('-');
  });
});

describe('sessionKind', () => {
  it('treats an absent kind as chat', () => {
    expect(sessionKind(sess())).toBe('chat');
    expect(sessionKind(sess({ kind: 'architect' }))).toBe('architect');
  });
});

describe('isLive', () => {
  it('is true for active and warm-idle only', () => {
    expect(isLive(sess({ status: 'active' }))).toBe(true);
    expect(isLive(sess({ status: 'warm-idle' }))).toBe(true);
    expect(isLive(sess({ status: 'ending' }))).toBe(false);
    expect(isLive(sess({ status: 'cold' }))).toBe(false);
  });
});

describe('sessionProjects', () => {
  it('reads the workspace for a blueprint and the project for a chat', () => {
    expect(sessionProjects(sess({ kind: 'architect', workspace: ['a', 'b'] }))).toEqual(['a', 'b']);
    expect(sessionProjects(sess({ kind: 'architect' }))).toEqual([]);
    expect(sessionProjects(sess({ project: 'p' }))).toEqual(['p']);
    expect(sessionProjects(sess())).toEqual([]);
  });
});
