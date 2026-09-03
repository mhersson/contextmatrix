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
  return render(<FootStrip version={null} syncStatuses={syncStatus ? [syncStatus] : []} />);
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

describe('FootStrip - several repos', () => {
  const healthy = makeStatus({ repo: 'team', shared: true, remote_reachable: true });
  const offline = makeStatus({ repo: 'lab', shared: true, remote_reachable: false, last_remote_error: 'dial tcp: timeout' });
  const disabled = makeStatus({ repo: 'private', enabled: false, last_sync_time: null });

  it('shows the worst repo and names it', () => {
    const { container } = render(<FootStrip version={null} syncStatuses={[healthy, offline, disabled]} />);
    expect(container.textContent).toContain('lab · sync offline');
    expect(container.textContent).toContain('Sync offline');
  });

  it('never lets a private repo without a remote mask a healthy shared one', () => {
    const { container } = render(<FootStrip version={null} syncStatuses={[healthy, disabled]} />);
    expect(container.textContent).toContain('All systems operational');
    expect(container.textContent).not.toContain('Sync disabled');
  });

  it('reports hidden projects', () => {
    const hidden = makeStatus({ repo: 'lab', shared: true, remote_reachable: true, hidden_projects: ['alpha'] });
    const { container } = render(<FootStrip version={null} syncStatuses={[healthy, hidden]} />);
    expect(container.textContent).toContain('1 hidden project');
  });

  it('keeps the single-repo wording with one status', () => {
    const { container } = render(<FootStrip version={null} syncStatuses={[healthy]} />);
    expect(container.textContent).not.toContain('team ·');
    expect(container.textContent).toContain('All systems operational');
  });
});
