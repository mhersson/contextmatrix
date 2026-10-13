import { describe, it, expect } from 'vitest';
import { jiraAuthProblem } from './jiraStatus';

const base = { configured: true, site_url: 'https://acme.atlassian.net' };

describe('jiraAuthProblem', () => {
  it('is null while Jira works, is off, or has not answered', () => {
    expect(jiraAuthProblem(null)).toBeNull();
    expect(jiraAuthProblem({ configured: false })).toBeNull();
    expect(jiraAuthProblem({ ...base, authenticated: true })).toBeNull();
  });

  it('gives each auth_error its own message', () => {
    const messages = (['unauthorized', 'captcha_required', 'unreachable'] as const).map((auth_error) =>
      jiraAuthProblem({ ...base, authenticated: false, auth_error }),
    );
    messages.forEach((m) => expect(m).toBeTruthy());
    expect(new Set(messages).size).toBe(3);
  });

  it('still reports a failure without an auth_error', () => {
    expect(jiraAuthProblem({ ...base, authenticated: false })).toBeTruthy();
  });
});
