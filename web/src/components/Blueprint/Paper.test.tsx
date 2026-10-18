import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { Paper } from './Paper';
import { resetArtifactCache } from './artifactCache';
import { holdDraft } from './draft';
import { DESIGN_V1, DESIGN_V2, findArtifact, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import type { BlueprintCurrentRun, BlueprintView } from '../../types';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));
const get = vi.mocked(api.getBlueprintArtifact);

const run: BlueprintCurrentRun = {
  stage: 'solution_design',
  kind: 'stage',
  persona: 'system_designer',
  model: 'm',
  started_at: '2026-10-17T10:00:00Z',
  turns: 6,
  tool_calls: 4,
  tokens: 11200,
};

/** Design rev 1 reviewed; the designer is writing rev 2. */
function viewWritingRev2(): BlueprintView {
  const view = viewAtDesignGate();
  view.open = undefined;
  view.current = run;
  view.artifacts = view.artifacts.filter((a) => !(a.stage === 'solution_design' && a.version === 2));
  return view;
}

describe('Paper', () => {
  beforeEach(() => {
    resetArtifactCache();
    get.mockReset();
    get.mockImplementation((_chat, stage, slice, version) => Promise.resolve(findArtifact(stage, version, slice)));
  });

  it('clouds what changed on the newest version', async () => {
    const { container } = render(
      <Paper chatId="S1" view={viewAtDesignGate()} slice="" stage="solution_design" version={2} writing={null} draft={null} compare={false} />,
    );
    await screen.findByText('Indent through the existing encoder');
    await waitFor(() => expect(container.querySelector('.bp-cloud .bp-dec')).not.toBeNull());
    expect(screen.queryByRole('note', { name: /Superseded/ })).not.toBeInTheDocument();
  });

  it('stamps a superseded version and draws no clouds', async () => {
    const { container } = render(
      <Paper chatId="S1" view={viewAtDesignGate()} slice="" stage="solution_design" version={1} writing={null} draft={null} compare={false} />,
    );
    await screen.findByText('Indent through the existing encoder');
    expect(screen.getByRole('note', { name: 'Superseded by rev 2' })).toBeInTheDocument();
    expect(container.querySelector('.bp-cloud')).toBeNull();
  });

  it('compares with the previous version', async () => {
    const { container } = render(
      <Paper chatId="S1" view={viewAtDesignGate()} slice="" stage="solution_design" version={2} writing={null} draft={null} compare />,
    );
    await waitFor(() => expect(container.querySelector('.bp-dec del')).toHaveTextContent(/MarshalIndent/));
  });

  it('shows the progress line over the dimmed previous version while a run works', async () => {
    const { container } = render(
      <Paper chatId="S1" view={viewWritingRev2()} slice="" stage="solution_design" version={2} writing={run} draft={null} compare={false} />,
    );
    expect(screen.getByRole('status')).toHaveTextContent(/System designer is working on the design: turn 6, 4 tool calls, 11k tokens/);
    await waitFor(() => expect(container.querySelector('.bp-dimmed')).toHaveTextContent('DEC1'));
    expect(get).toHaveBeenCalledWith('S1', 'solution_design', '', 1);
  });

  it('says the question waits on the operator instead of the run working', async () => {
    const view = { ...viewWritingRev2(), open: { prompt_id: 'Q4', kind: 'question' as const } };
    const { container } = render(
      <Paper chatId="S1" view={view} slice="" stage="solution_design" version={2} writing={run} draft={null} compare={false} />,
    );
    expect(screen.getByRole('status')).toHaveTextContent(/^Q4 is waiting for you$/);
    await waitFor(() => expect(container.querySelector('.bp-dimmed')).toHaveTextContent('DEC1'));
  });

  it('names the responder while it answers the stage question', () => {
    const responder: BlueprintCurrentRun = { ...run, kind: 'responder' };
    const view = { ...viewWritingRev2(), current: responder };
    render(<Paper chatId="S1" view={view} slice="" stage="solution_design" version={2} writing={responder} draft={null} compare={false} />);
    expect(screen.getByRole('status')).toHaveTextContent(/^The responder is answering a question: turn 6, 4 tool calls, 11k tokens$/);
  });

  it('streams a draft with a caret on the block being written', () => {
    const draft = holdDraft({ stage: 'solution_design', version: 2, offset: 0, text: 'Done paragraph.\n\nWriting no' });
    const { container } = render(
      <Paper chatId="S1" view={viewWritingRev2()} slice="" stage="solution_design" version={2} writing={run} draft={draft} compare={false} />,
    );
    expect(screen.getByTestId('md')).toHaveTextContent('Done paragraph.');
    const writing = container.querySelector('.bp-writing')!;
    expect(writing).toHaveTextContent('Writing no');
    expect(writing.querySelector('.bp-caret')).not.toBeNull();
  });

  it('stills the caret and the writing edge while the shown step waits on a question', () => {
    const draft = holdDraft({ stage: 'solution_design', version: 2, offset: 0, text: 'Done paragraph.\n\nWriting no' });
    const view = { ...viewWritingRev2(), open: { prompt_id: 'Q4', kind: 'question' as const } };
    const { container } = render(
      <Paper chatId="S1" view={view} slice="" stage="solution_design" version={2} writing={run} draft={draft} compare={false} />,
    );
    expect(container).toHaveTextContent('Writing no');
    expect(container.querySelector('.bp-caret')).toBeNull();
    expect(container.querySelector('.bp-writing')).toBeNull();
  });

  it('keeps a refused draft visible with the reason', () => {
    const draft = { ...holdDraft({ stage: 'solution_design', version: 2, offset: 0, text: 'Draft.' }), refused: true, reason: 'no decisions' };
    render(<Paper chatId="S1" view={viewWritingRev2()} slice="" stage="solution_design" version={2} writing={run} draft={draft} compare={false} />);
    expect(screen.getByRole('status')).toHaveTextContent('Refused by checks: no decisions');
    expect(screen.getByTestId('md')).toHaveTextContent('Draft.');
  });

  it('offers a retry when the artifact does not load', async () => {
    get.mockRejectedValueOnce({ error: 'down', code: 'BACKEND_UNAVAILABLE' });
    render(<Paper chatId="S1" view={viewAtDesignGate()} slice="" stage="intake" version={1} writing={null} draft={null} compare={false} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('heading', { name: /^Assumptions/ })).toBeInTheDocument();
  });

  it('shows the problems an accepted artifact still has', async () => {
    get.mockResolvedValueOnce({ ...DESIGN_V2, check_problems: ['DEC2 has no rejected option.'] });
    get.mockResolvedValueOnce(DESIGN_V1);
    render(<Paper chatId="S1" view={viewAtDesignGate()} slice="" stage="solution_design" version={2} writing={null} draft={null} compare={false} />);
    expect(await screen.findByText('DEC2 has no rejected option.')).toBeInTheDocument();
  });
  it('says what an open round waits on with no run in flight', () => {
    const view: BlueprintView = {
      ...viewWritingRev2(),
      current: undefined,
      open: { prompt_id: 'round:1', kind: 'round' },
      pipeline: viewWritingRev2().pipeline.map((s) => (s.stage === 'solution_design' ? { ...s, status: 'waiting' as const } : s)),
    };
    render(
      <Paper chatId="S1" view={view} slice="" stage="solution_design" version={2} writing={null} draft={null} compare={false} roundLeft={{ questions: 3, assumptions: 0 }} />,
    );
    expect(screen.getByRole('status')).toHaveTextContent(/^Waiting on 3 questions$/);
  });
});
