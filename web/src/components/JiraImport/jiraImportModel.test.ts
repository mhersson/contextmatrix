import { describe, it, expect } from 'vitest';
import type { ProjectConfig } from '../../types';
import {
  buildRequest,
  canSubmit,
  initialForm,
  isSelectable,
  prefixClashes,
  setAllProjects,
  setProject,
  splitWarnings,
  toggle,
} from './jiraImportModel';
import { samplePreview } from './previewFixture';

describe('jiraImportModel', () => {
  it('selects open, never-imported issues by default', () => {
    const form = initialForm(samplePreview());
    expect([...form.selected].sort()).toEqual(['PROJ-2', 'PROJ-3', 'PROJ-7']);
    expect(form.vetted).toBe(false);
    expect(form.includeEpic).toBe(true);
    expect(form.title).toBe('PROJ-1 Checkout revamp');
    expect(form.projects['PROJ-6']).toBe('web');
  });

  it('uses the existing playbook title', () => {
    const form = initialForm(samplePreview({ playbook: { id: 'p', title: 'Existing' } }));
    expect(form.title).toBe('Existing');
  });

  it('allows re-selecting an imported card missing from the playbook only', () => {
    const p = samplePreview();
    expect(isSelectable(p.children[2])).toBe(false);
    expect(isSelectable(p.children[3])).toBe(true);
  });

  it('carries sub-tasks that followed their parent to its new project', () => {
    const p = samplePreview();
    const moved = setProject(initialForm(p), p, 'PROJ-2', 'web');
    expect(moved.projects['PROJ-3']).toBe('web');
    expect(splitWarnings(p, moved).size).toBe(0);

    const split = setProject(moved, p, 'PROJ-3', 'api');
    expect(splitWarnings(p, split).get('PROJ-3')).toBe('PROJ-2');
  });

  it('warns only when both ends of the dependency are in play', () => {
    const p = samplePreview();
    const split = setProject(initialForm(p), p, 'PROJ-3', 'web');
    expect(splitWarnings(p, split).has('PROJ-3')).toBe(true);
    expect(splitWarnings(p, toggle(split, 'PROJ-2')).has('PROJ-3')).toBe(false);
  });

  it('sets every non-imported issue at once', () => {
    const p = samplePreview();
    const all = setAllProjects(initialForm(p), p, 'web');
    expect(all.projects['PROJ-7']).toBe('web');
    expect(all.projects['PROJ-5']).toBe('api');
  });

  it('needs a project for every selected issue before submitting', () => {
    const p = samplePreview();
    const form = initialForm(p);
    expect(canSubmit(p, form)).toBe(false); // PROJ-7 has no default
    expect(canSubmit(p, setProject(form, p, 'PROJ-7', 'api'))).toBe(true);
    expect(canSubmit(p, { ...form, selected: new Set() })).toBe(false);
  });

  it('blocks submitting while the playbook has an active run', () => {
    const ready = (runActive: boolean) => {
      const p = samplePreview({
        playbook: { id: 'p', title: 'Existing', run_active: runActive },
      });
      return canSubmit(p, setProject(initialForm(p), p, 'PROJ-7', 'api'));
    };
    expect(ready(false)).toBe(true);
    expect(ready(true)).toBe(false);
  });

  it('flags selected issues landing in a project whose card prefix is a Jira key here', () => {
    const p = samplePreview();
    const projects = [
      { name: 'api', prefix: 'API' },
      { name: 'legacy', prefix: 'proj' },
    ] as ProjectConfig[];
    const form = setProject(initialForm(p), p, 'PROJ-7', 'legacy');

    expect([...prefixClashes(p, form, projects)]).toEqual(['PROJ-7']);
    expect(prefixClashes(p, toggle(form, 'PROJ-7'), projects).size).toBe(0);
    expect(prefixClashes(p, setProject(form, p, 'PROJ-7', 'api'), projects).size).toBe(0);
  });

  it('builds the request, falling back to the epic summary for the description', () => {
    const p = samplePreview();
    const form = setProject(initialForm(p), p, 'PROJ-7', 'api');
    const req = buildRequest(p, form, '', 'team');
    expect(req.selections).toEqual(
      expect.arrayContaining([
        { key: 'PROJ-2', project: 'api' },
        { key: 'PROJ-3', project: 'api' },
        { key: 'PROJ-7', project: 'api' },
      ]),
    );
    expect(req.selections).toHaveLength(3);
    expect(req.playbook).toEqual({
      title: 'PROJ-1 Checkout revamp',
      description: 'Checkout revamp',
      boards_repo: 'team',
    });
    expect(buildRequest(p, form, 'Typed', undefined).playbook).toEqual({
      title: 'PROJ-1 Checkout revamp',
      description: 'Typed',
    });
  });
});
