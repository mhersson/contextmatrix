import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import type { SyncStatus } from '../../types';
import { FootStrip } from './FootStrip';

function makeStatus(overrides: Partial<SyncStatus> = {}): SyncStatus {
  return {
    last_sync_time: '2026-09-03T12:00:00Z',
    syncing: false,
    enabled: true,
    ...overrides,
  };
}

function renderStrip(syncStatus: SyncStatus | null) {
  return render(<FootStrip version={null} syncStatus={syncStatus} />);
}

describe('FootStrip - claims at risk', () => {
  it('warns on a shared board whose pushes are failing', () => {
    const { container } = renderStrip(
      makeStatus({ shared: true, remote_reachable: true, claims_at_risk: true }),
    );
    expect(container.textContent).toContain('pushes failing · claims at risk');
    expect(container.textContent).toContain('Claims at risk');
  });

  it('stays silent on a private board even when the field is set', () => {
    const { container } = renderStrip(
      makeStatus({ shared: false, claims_at_risk: true }),
    );
    expect(container.textContent).not.toMatch(/claims at risk/i);
  });

  it('stays silent on a shared board that is pushing cleanly', () => {
    const { container } = renderStrip(
      makeStatus({ shared: true, remote_reachable: true, claims_at_risk: false }),
    );
    expect(container.textContent).not.toMatch(/claims at risk/i);
  });
});
