import { describe, it, expect } from 'vitest';
import type { SyncStatus } from '../../types';
import { describeSync } from './SyncDot';

function status(overrides: Partial<SyncStatus> = {}): SyncStatus {
  return { last_sync_time: '2026-09-03T12:00:00Z', syncing: false, enabled: true, shared: true, remote_reachable: true, ...overrides };
}

describe('describeSync', () => {
  it('is grey and disabled without a remote', () => {
    expect(describeSync(status({ enabled: false, shared: false }))).toEqual({ color: 'var(--grey0)', title: 'sync disabled (no remote)' });
  });

  it('is red when offline and says what is disabled', () => {
    const d = describeSync(status({ remote_reachable: false }));
    expect(d.color).toBe('var(--red)');
    expect(d.title).toContain('offline');
  });

  it('is amber with unpushed commits and lists resolutions', () => {
    const d = describeSync(status({ unpushed_commits: 3, resolutions: [{} as never, {} as never] }));
    expect(d.color).toBe('var(--yellow)');
    expect(d.title).toContain('3 unpushed commits');
    expect(d.title).toContain('resolved 2 conflicts');
  });

  it('is amber with hidden projects and names them', () => {
    const d = describeSync(status({ hidden_projects: ['alpha', 'beta'] }));
    expect(d.color).toBe('var(--yellow)');
    expect(d.title).toContain('hidden: alpha, beta');
  });

  it('is green and healthy otherwise', () => {
    const d = describeSync(status());
    expect(d.color).toBe('var(--green)');
    expect(d.title).toContain('healthy');
  });
});
