import { describe, it, expect } from 'vitest';
import { sourceToastMessage } from './sourceToast';

describe('sourceToastMessage', () => {
  it.each(['github', 'jira'])('announces a %s import with the card title', (system) => {
    expect(sourceToastMessage({ source_system: system, title: 'PROJ-2 Cart API' }, 'API-001')).toContain('PROJ-2 Cart API');
  });

  it('falls back to the card id without a title', () => {
    expect(sourceToastMessage({ source_system: 'jira' }, 'API-001')).toContain('API-001');
  });

  it('stays quiet for cards that were not imported or come from elsewhere', () => {
    expect(sourceToastMessage(undefined, 'API-001')).toBeNull();
    expect(sourceToastMessage({ source_system: 'linear', title: 'x' }, 'API-001')).toBeNull();
  });

  it('distinguishes the two sources', () => {
    expect(sourceToastMessage({ source_system: 'jira', title: 't' }, 'A-1')).not.toBe(
      sourceToastMessage({ source_system: 'github', title: 't' }, 'A-1'),
    );
  });
});
