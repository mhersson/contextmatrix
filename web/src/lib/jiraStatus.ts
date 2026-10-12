import type { JiraStatus } from '../types';

const AUTH_PROBLEMS: Record<NonNullable<JiraStatus['auth_error']>, string> = {
  unauthorized: 'Jira rejected the configured token; check jira.email and jira.token',
  captcha_required: 'A login CAPTCHA blocks the Jira account; sign in to Jira in a browser once',
  unreachable: 'Jira is unreachable from the server',
};

/** Why a configured Jira cannot be used right now, or null when it can (or is off). */
export function jiraAuthProblem(status: JiraStatus | null): string | null {
  if (!status?.configured || status.authenticated !== false) return null;
  return (status.auth_error && AUTH_PROBLEMS[status.auth_error]) || 'Jira authentication is failing';
}
