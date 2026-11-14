import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import type { AttentionItem, ChatSession, PlaybookSummary } from '../../types';
import { PlaybookTableRow } from '../Playbooks/PlaybookTableRow';
import { PlaybooksTable } from '../Playbooks/PlaybooksTable';
import { SessionRow } from './SessionRow';
import { SessionsTable } from './SessionsTable';

const LONG_TITLE = 'A very long title that would otherwise push every right-hand column out of view';
const PROJECTS = ['alpha-service', 'beta-gateway', 'gamma-frontend', 'delta-infra'];
const REASON = 'Waiting on a long answer to a rather long question';

const noop = vi.fn();
const waiting: AttentionItem = { kind: 'blueprint', id: 's', title: 's', reason: REASON, since: '2026-01-01T00:00:00Z' };

const session: ChatSession = {
  id: 's',
  title: LONG_TITLE,
  kind: 'architect',
  status: 'cold',
  mode: 'auto',
  created_at: '2026-10-01T10:00:00Z',
  last_active: '2026-10-01T10:00:00Z',
  created_by: 'human:alice',
  workspace: PROJECTS,
};

const playbook: PlaybookSummary = {
  id: 'pb',
  title: LONG_TITLE,
  complete: 0,
  total: 1,
  segments: ['pending'],
  projects: PROJECTS.length,
  project_names: PROJECTS,
  next: { type: 'card', project: 'alpha', card: 'ALPHA-7', title: 'Wire the flag' },
  updated_at: '2026-10-01T10:00:00Z',
};

const handlers = { onToggle: noop, onPin: noop, onOpen: noop };

describe('Sessions table truncation', () => {
  it('a session row truncates title and projects, keeping the full text in tooltips', () => {
    render(
      <table><tbody>
        <SessionRow session={session} showMode ticked={false} {...handlers} waiting={waiting} />
      </tbody></table>,
    );
    const row = screen.getByRole('row');
    expect(within(row).getByText(LONG_TITLE)).toHaveAttribute('title', LONG_TITLE);
    expect(within(row).getByText(LONG_TITLE).closest('td')).toHaveClass('ssn-col-title');
    expect(within(row).getByText(REASON)).toHaveAttribute('title', REASON);
    const cell = within(row).getByText(PROJECTS.join(', '));
    expect(cell).toHaveClass('ssn-col-projects');
    expect(cell).toHaveAttribute('title', PROJECTS.join(', '));
  });

  it('a session row without projects has no projects tooltip', () => {
    render(
      <table><tbody>
        <SessionRow session={{ ...session, workspace: undefined }} showMode ticked={false} {...handlers} />
      </tbody></table>,
    );
    const cell = within(screen.getByRole('row')).getByText('-', { selector: 'td.ssn-col-projects' });
    expect(cell).not.toHaveAttribute('title');
  });

  it('a playbook row truncates title, projects and wait reason', () => {
    render(
      <table><tbody>
        <PlaybookTableRow playbook={playbook} ticked={false} {...handlers} waiting={{ ...waiting, kind: 'playbook' }} />
      </tbody></table>,
    );
    const row = screen.getByRole('row');
    expect(within(row).getByText(LONG_TITLE)).toHaveAttribute('title', LONG_TITLE);
    expect(within(row).getByText(LONG_TITLE).closest('td')).toHaveClass('ssn-col-title');
    expect(within(row).getByText(REASON)).toHaveAttribute('title', REASON);
    const cell = within(row).getByText(PROJECTS.join(', '));
    expect(cell).toHaveClass('ssn-col-projects');
    expect(cell).toHaveAttribute('title', PROJECTS.join(', '));
  });

  it.each([
    ['Chats', false, ['Title', 'Project', 'Cost', 'Last active']],
    ['Blueprints', true, ['Title', 'Projects', 'Mode', 'Cost', 'Last active']],
  ])('%s keeps every column header', (_n, showMode, headers) => {
    render(
      <SessionsTable rows={[session]} showMode={showMode} selected={new Set()} onToggleAll={noop}
        {...handlers} waiting={new Map()} />,
    );
    for (const h of headers) expect(screen.getByRole('columnheader', { name: h })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /^(Projects?)$/ })).toHaveClass('ssn-col-projects');
  });

  it('Playbooks keeps every column header', () => {
    render(
      <PlaybooksTable rows={[playbook]} selected={new Set()} onToggleAll={noop} {...handlers} waiting={new Map()} />,
    );
    for (const h of ['Title', 'Projects', 'Run', 'Progress', 'Last active']) {
      expect(screen.getByRole('columnheader', { name: h })).toBeInTheDocument();
    }
    expect(screen.getByRole('columnheader', { name: 'Projects' })).toHaveClass('ssn-col-projects');
  });
});
