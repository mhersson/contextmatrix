import type { JiraEpicPreview, JiraIssueNode } from '../../types';

function node(key: string, extra: Partial<JiraIssueNode> = {}): JiraIssueNode {
  return {
    key,
    id: key,
    summary: key,
    url: `https://acme.atlassian.net/browse/${key}`,
    issue_type: 'Story',
    status: 'To Do',
    status_category: 'new',
    jira_project: 'PROJ',
    default_project: 'api',
    ...extra,
  };
}

export function samplePreview(extra: Partial<JiraEpicPreview> = {}): JiraEpicPreview {
  return {
    epic: {
      key: 'PROJ-1',
      summary: 'Checkout revamp',
      url: 'https://acme.atlassian.net/browse/PROJ-1',
      description: 'Epic goal',
    },
    children: [
      node('PROJ-2', { subtasks: [node('PROJ-3', { issue_type: 'Sub-task' })] }),
      node('PROJ-4', { status: 'Done', status_category: 'done' }),
      node('PROJ-5', { imported: { project: 'api', card: 'API-007', in_playbook: true } }),
      node('PROJ-6', { imported: { project: 'web', card: 'WEB-002', in_playbook: false } }),
      node('PROJ-7', { default_project: undefined }),
    ],
    ...extra,
  };
}
