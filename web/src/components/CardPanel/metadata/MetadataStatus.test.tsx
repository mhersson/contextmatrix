import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { Card, ProjectConfig } from '../../../types';
import { MetadataStatus } from './MetadataStatus';
import { parkedResetLabel } from '../../../lib/subscription';

const config = {
  name: 'demo',
  prefix: 'CMX',
  next_id: 2,
  states: ['todo', 'in_progress', 'done'],
  types: ['task'],
  priorities: ['medium'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
} as ProjectConfig;

function renderStatus(partial: Partial<Card>) {
  const card = {
    id: 'CMX-001',
    title: 'Demo',
    project: 'demo',
    type: 'task',
    state: 'in_progress',
    priority: 'medium',
    created: '2026-11-01T00:00:00Z',
    updated: '2026-11-01T00:00:00Z',
    body: '',
    ...partial,
  } as Card;
  render(<MetadataStatus card={card} editedCard={card} config={config} workerAttached={false} onStateChange={() => {}} />);
}

describe('MetadataStatus worker badge', () => {
  it('names a future usage-limit reset on a parked card', () => {
    const resets = new Date(2099, 0, 1, 14, 0).toISOString();
    renderStatus({ worker_status: 'parked', park_resets_at: resets });
    expect(screen.getByText(parkedResetLabel(resets) as string)).toBeInTheDocument();
  });

  it('says the reset has passed once it has', () => {
    const resets = new Date(2026, 10, 5, 14, 0).toISOString();
    renderStatus({ worker_status: 'parked', park_resets_at: resets });
    expect(screen.getByText(parkedResetLabel(resets) as string)).toBeInTheDocument();
    expect(parkedResetLabel(resets)).not.toBe(parkedResetLabel(resets, Date.parse(resets) - 60_000));
  });

  it('shows no reset on a plain parked badge', () => {
    renderStatus({ worker_status: 'parked' });
    expect(parkedResetLabel(undefined)).toBeNull();
    expect(screen.getByText('Parked for a human')).toBeInTheDocument();
  });
});
