import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { PlaybookCreateRow, type PlaybookCreateRowProps } from './PlaybookCreateRow';

const SITE = 'https://acme.atlassian.net';

type Overrides = Partial<Omit<PlaybookCreateRowProps, 'title' | 'onTitleChange' | 'description' | 'onDescriptionChange'>>;

// Holds the drafts the way the tab does, so typing reaches the inputs.
function Harness(props: Overrides) {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  return (
    <table>
      <tbody>
        <PlaybookCreateRow
          title={title}
          onTitleChange={setTitle}
          description={description}
          onDescriptionChange={setDescription}
          onCreate={() => {}}
          onCancel={() => {}}
          submitting={false}
          {...props}
        />
      </tbody>
    </table>
  );
}

const titleInput = () => screen.getByLabelText('Playbook title');
const type = (value: string) => fireEvent.change(titleInput(), { target: { value } });
const importButton = () => screen.getByRole('button', { name: 'Import from Jira' });

describe('PlaybookCreateRow', () => {
  it('Enter in either input creates, Escape cancels', () => {
    const onCreate = vi.fn();
    const onCancel = vi.fn();
    render(<Harness onCreate={onCreate} onCancel={onCancel} />);

    fireEvent.keyDown(titleInput(), { key: 'Enter' });
    fireEvent.keyDown(screen.getByLabelText('Description'), { key: 'Enter' });
    expect(onCreate).toHaveBeenCalledTimes(2);

    fireEvent.keyDown(screen.getByLabelText('Description'), { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('Enter during an IME composition does not create', () => {
    const onCreate = vi.fn();
    render(<Harness onCreate={onCreate} />);
    fireEvent.keyDown(titleInput(), { key: 'Enter', isComposing: true });
    expect(onCreate).not.toHaveBeenCalled();
  });

  it('Create playbook and Cancel call their handlers; Create waits while a request is in flight', () => {
    const onCreate = vi.fn();
    const onCancel = vi.fn();
    const { rerender } = render(<Harness onCreate={onCreate} onCancel={onCancel} />);

    fireEvent.click(screen.getByRole('button', { name: 'Create playbook' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);

    rerender(<Harness onCreate={onCreate} submitting />);
    expect(screen.getByRole('button', { name: 'Create playbook' })).toBeDisabled();
  });

  it('offers Import from Jira only when Jira is configured', () => {
    const { rerender } = render(<Harness />);
    expect(screen.queryByRole('button', { name: 'Import from Jira' })).not.toBeInTheDocument();

    rerender(<Harness jiraSiteURL={SITE} />);
    expect(importButton()).toBeInTheDocument();
  });

  it('enables Import only for an epic key or a URL on the configured site, and hands over the key upper-cased', () => {
    const onImportJira = vi.fn();
    render(<Harness jiraSiteURL={SITE} onImportJira={onImportJira} />);
    expect(importButton()).toBeDisabled();

    for (const [value, enabled] of [
      ['Q3 rollout', false],
      ['PROJ-12', true],
      [`${SITE}/browse/PROJ-12`, true],
      ['https://evil.example/browse/PROJ-12', false],
    ] as const) {
      type(value);
      if (enabled) expect(importButton()).toBeEnabled();
      else expect(importButton()).toBeDisabled();
    }

    type('proj-12');
    fireEvent.click(importButton());
    expect(onImportJira).toHaveBeenCalledWith('PROJ-12');
  });

  it('keeps Enter as Create even when the title is a key', () => {
    const onCreate = vi.fn();
    const onImportJira = vi.fn();
    render(<Harness jiraSiteURL={SITE} onCreate={onCreate} onImportJira={onImportJira} />);
    type('PROJ-12');
    fireEvent.keyDown(titleInput(), { key: 'Enter' });
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(onImportJira).not.toHaveBeenCalled();
  });

  it('disables Import while Jira blocks it or a request is in flight', () => {
    const { rerender } = render(<Harness jiraSiteURL={SITE} jiraImportBlocked />);
    type('PROJ-12');
    expect(importButton()).toBeDisabled();

    rerender(<Harness jiraSiteURL={SITE} submitting />);
    expect(importButton()).toBeDisabled();

    rerender(<Harness jiraSiteURL={SITE} />);
    expect(importButton()).toBeEnabled();
  });

  it('shows the Jira auth warning only when there is one', () => {
    const { rerender } = render(<Harness jiraSiteURL={SITE} />);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    rerender(<Harness jiraSiteURL={SITE} jiraAuthProblem="Jira is unreachable from the server" />);
    expect(screen.getByRole('status')).toBeInTheDocument();
  });

  it('offers a boards repo select on a multi-repo instance only', () => {
    const onRepoChange = vi.fn();
    const repos = [{ name: 'team', shared: true }, { name: 'private', shared: false }];
    const { rerender } = render(<Harness repos={[repos[0]]} />);
    expect(screen.queryByRole('combobox', { name: 'Boards repo' })).not.toBeInTheDocument();

    rerender(<Harness repos={repos} repo="team" onRepoChange={onRepoChange} />);
    const select = screen.getByRole('combobox', { name: 'Boards repo' });
    expect(select).toHaveValue('team');
    fireEvent.change(select, { target: { value: 'private' } });
    expect(onRepoChange).toHaveBeenCalledWith('private');
  });
});
