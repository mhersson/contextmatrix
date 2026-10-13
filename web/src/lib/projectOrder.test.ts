import { describe, it, expect } from 'vitest';
import { sortProjectsForSidebar } from './projectOrder';
import type { ProjectConfig } from '../types';

function p(name: string, boards_repo?: string): ProjectConfig {
  return { name, prefix: name.toUpperCase(), next_id: 1, states: [], types: [], priorities: [], transitions: {}, boards_repo };
}

describe('sortProjectsForSidebar', () => {
  it('sorts by name case-insensitively with a single repo', () => {
    const out = sortProjectsForSidebar([p('beta'), p('Alpha'), p('gamma')], [{ name: 'a', shared: false }]);
    expect(out.map((x) => x.name)).toEqual(['Alpha', 'beta', 'gamma']);
  });

  it('groups by repo in config order, then name, when several repos exist', () => {
    const repos = [{ name: 'work', shared: false }, { name: 'home', shared: false }];
    const out = sortProjectsForSidebar(
      [p('a', 'home'), p('z', 'work'), p('m', 'work'), p('orphan', 'gone')],
      repos,
    );
    // unknown repo falls back to the first configured repo
    expect(out.map((x) => x.name)).toEqual(['m', 'orphan', 'z', 'a']);
  });
});
