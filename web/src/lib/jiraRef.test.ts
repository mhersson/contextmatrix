import { describe, it, expect } from 'vitest';
import { parseJiraRef } from './jiraRef';

const SITE = 'https://acme.atlassian.net';

describe('parseJiraRef', () => {
  it.each([
    ['PROJ-12', 'PROJ-12'],
    ['  PROJ-12  ', 'PROJ-12'],
    ['AB_C2-7', 'AB_C2-7'],
    ['proj-12', 'PROJ-12'],
    [`${SITE}/browse/PROJ-12`, 'PROJ-12'],
    [`${SITE}/browse/proj-12`, 'PROJ-12'],
    ['https://ACME.atlassian.net/browse/PROJ-12', 'PROJ-12'],
    [`${SITE}/browse/PROJ-12/`, 'PROJ-12'],
    [`${SITE}/jira/software/projects/PROJ/boards/1?selectedIssue=PROJ-12`, 'PROJ-12'],
  ])('parses %s', (input, want) => {
    expect(parseJiraRef(input, SITE)).toBe(want);
  });

  it.each([
    'Q3 rollout',
    'PROJ-0',
    'P-12',
    'https://evil.example/browse/PROJ-12',
    'http://acme.atlassian.net/browse/PROJ-12',
    `${SITE}/browse/not-a-key`,
    `${SITE}/browse/%E0%A4%A`,
    `${SITE}/projects/PROJ`,
    '',
  ])('rejects %s', (input) => {
    expect(parseJiraRef(input, SITE)).toBeNull();
  });

  it('rejects everything when the site URL is unusable', () => {
    expect(parseJiraRef(`${SITE}/browse/PROJ-12`, 'not a url')).toBeNull();
  });
});
