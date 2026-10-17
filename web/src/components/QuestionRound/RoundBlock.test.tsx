import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { RoundReply } from '../../types';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound } from './parseRound';
import { RoundBlock } from './RoundBlock';
import type { RoundMode } from './roundView';

function renderBlock(extra: Record<string, unknown> = {}, mode: RoundMode = 'open') {
  const round = parseRound(rawRound(extra))!;
  return render(<RoundBlock promptId="round:1" round={round} mode={mode} />);
}

const tab = (name: RegExp) => screen.getByRole('tab', { name });
const option = (name: RegExp) => screen.getByRole('button', { name });

describe('RoundBlock questions', () => {
  it('shows one tab per question, ticks answered ones and opens on the first open one', () => {
    renderBlock({ answers: [{ number: 1, option: 1 }] });
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'Deploy tooling, answered',
      'Logging, open',
      'Postgres, open',
      'Submit',
    ]);
    expect(tab(/Deploy tooling/)).toHaveClass('done');
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('region', { name: /3 questions from the system designer/ })).toBeInTheDocument();
  });

  it('selects an option on click, expands its pros, cons and sources, and collapses it on a second click', () => {
    renderBlock();
    const kustomize = option(/Kustomize/);
    expect(kustomize).toHaveAccessibleName(/Recommended/);
    fireEvent.click(kustomize);
    expect(kustomize).toHaveAttribute('aria-pressed', 'true');
    expect(kustomize).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Built into kubectl')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'kubectl.docs.kubernetes.io/guides' })).toHaveAttribute(
      'href',
      'https://kubectl.docs.kubernetes.io/guides/',
    );
    fireEvent.click(kustomize);
    expect(kustomize).toHaveAttribute('aria-expanded', 'false');
    expect(kustomize).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByText('Built into kubectl')).not.toBeInTheDocument();
  });

  it('labels a collapsed option by what it carries and shows only those sections when expanded', () => {
    renderBlock({
      questions: [
        {
          number: 1,
          title: 'Mixed',
          text: 'Pick one?',
          options: [
            { title: 'Full', summary: 's', pros: ['p1'], cons: ['c1'], sources: [{ tag: 'S1', title: 'one', url: 'https://a.example/' }] },
            { title: 'ProsOnly', summary: 's', pros: ['p2'] },
            { title: 'Bare', summary: 's' },
          ],
        },
      ],
    });
    expect(option(/Full/)).toHaveTextContent('pros · cons · 1 source');
    expect(option(/ProsOnly/)).toHaveTextContent(/pros$/);
    expect(option(/Bare/).textContent).toBe('3Bares');
    fireEvent.click(option(/ProsOnly/));
    expect(screen.getByRole('heading', { name: 'Pros' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Cons' })).not.toBeInTheDocument();
    expect(screen.queryByText(/Sources/)).not.toBeInTheDocument();
    fireEvent.click(option(/Bare/));
    expect(option(/Bare/)).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByRole('heading', { name: 'Pros' })).not.toBeInTheDocument();
    expect(screen.queryByText(/Sources/)).not.toBeInTheDocument();
  });

  it('moves the selection, never stacking two picks on one question', () => {
    renderBlock();
    fireEvent.click(option(/Kustomize/));
    fireEvent.click(option(/Helm/));
    expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'false');
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
  });

  it('says model knowledge instead of links when research is off', () => {
    renderBlock({ research_off: true });
    fireEvent.click(option(/Kustomize/));
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText(/model knowledge/)).toBeInTheDocument();
  });

  it('shows a code source and an unsafe url as text, never as a link', () => {
    renderBlock({
      questions: [
        {
          number: 1,
          title: 'T',
          text: 'Q?',
          options: [{ title: 'Alpha', sources: [{ tag: 'C1', title: 'internal/api/chats.go' }, { tag: 'S1', title: 'evil', url: 'javascript:alert(1)' }] }],
        },
      ],
    });
    fireEvent.click(option(/Alpha/));
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText('internal/api/chats.go')).toBeInTheDocument();
    expect(screen.getByText('evil')).toBeInTheDocument();
  });

  it('offers a typed answer as the last option on every question, and typing it replaces an option pick', () => {
    renderBlock();
    for (const name of [/Deploy tooling/, /Logging/, /Postgres/]) {
      fireEvent.click(tab(name));
      expect(screen.getByRole('textbox', { name: /Your own answer/ })).toBeInTheDocument();
    }
    fireEvent.click(tab(/Deploy tooling/));
    fireEvent.click(option(/Helm/));
    fireEvent.change(screen.getByRole('textbox', { name: /Your own answer to Q1/ }), { target: { value: 'Neither, plain YAML' } });
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'false');
  });

  it('switches tabs with the arrow keys, picks with digits and jumps to the own answer with the next digit', () => {
    renderBlock();
    const first = tab(/Deploy tooling/);
    first.focus();
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(tab(/Logging/));
    fireEvent.keyDown(tab(/Logging/), { key: '2' });
    expect(option(/No stack/)).toHaveAttribute('aria-pressed', 'true');
    fireEvent.keyDown(tab(/Logging/), { key: '3' });
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: /Your own answer to Q2/ }));
    fireEvent.keyDown(tab(/Logging/), { key: 'ArrowLeft' });
    expect(tab(/Deploy tooling/)).toHaveAttribute('aria-selected', 'true');
  });

  it('leaves typing alone: digits and arrows in the own answer type, Enter moves to the next question', () => {
    renderBlock();
    const input = screen.getByRole('textbox', { name: /Your own answer to Q1/ });
    fireEvent.keyDown(input, { key: '2' });
    fireEvent.keyDown(input, { key: 'ArrowRight' });
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'false');
    expect(tab(/Deploy tooling/)).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
  });

  it('lets a paused or responder round be read but never answered', () => {
    for (const mode of ['paused', 'readonly'] as const) {
      const { unmount } = renderBlock({ answers: [{ number: 1, option: 2 }] }, mode);
      fireEvent.click(tab(/Deploy tooling/));
      expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
      fireEvent.click(option(/Kustomize/));
      expect(option(/Kustomize/)).toHaveAttribute('aria-expanded', 'true');
      expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'false');
      expect(screen.getByRole('textbox', { name: /Your own answer to Q1/ })).toBeDisabled();
      unmount();
    }
  });

  it('shows the stored own answer in the input of an answered question', () => {
    renderBlock({ answers: [{ number: 2, text: 'Loki only' }] });
    fireEvent.click(tab(/Logging/));
    const pane = screen.getByRole('tabpanel');
    expect(within(pane).getByRole('textbox', { name: /Your own answer to Q2/ })).toHaveValue('Loki only');
  });
});

function renderSending(extra: Record<string, unknown> = {}, onSendRound = vi.fn(async () => {}), onSendChat = vi.fn(async () => {})) {
  const round = parseRound(rawRound(extra))!;
  render(<RoundBlock promptId="round:1" round={round} mode="open" onSendRound={onSendRound} onSendChat={onSendChat} />);
  return { onSendRound, onSendChat };
}

function answerAll() {
  for (const [tabName, opt] of [[/Deploy tooling/, /Kustomize/], [/Logging/, /Grafana/], [/Postgres/, /CloudNativePG/]] as const) {
    fireEvent.click(tab(tabName));
    fireEvent.click(option(opt));
  }
}

describe('RoundBlock sending', () => {
  it('reviews every answer on the Submit tab and marks the missing ones', () => {
    renderSending({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Postgres/));
    fireEvent.click(option(/CloudNativePG/));
    fireEvent.click(tab(/Submit/));
    const rows = screen.getAllByRole('row').map((r) => Array.from((r as HTMLTableRowElement).cells).map((c) => c.textContent));
    expect(rows).toEqual([
      ['Q1 Deploy tooling', 'Kustomize'],
      ['Q2 Logging', 'not answered'],
      ['Q3 Postgres', 'CloudNativePG operator'],
    ]);
  });

  it('submits all only once every question is answered, as the final reply', async () => {
    const { onSendRound } = renderSending();
    fireEvent.click(tab(/Submit/));
    expect(screen.getByRole('button', { name: 'Submit all 3' })).toBeDisabled();
    answerAll();
    fireEvent.click(tab(/Submit/));
    fireEvent.click(screen.getByRole('button', { name: 'Submit all 3' }));
    await waitFor(() => expect(onSendRound).toHaveBeenCalledTimes(1));
    const [content, reply] = onSendRound.mock.calls[0] as unknown as [string, RoundReply];
    expect(content.length).toBeGreaterThan(0);
    expect(reply).toEqual({
      prompt_id: 'round:1',
      answers: [{ number: 1, option: 1 }, { number: 2, option: 1 }, { number: 3, option: 1 }],
      decisions: [],
      final: true,
    });
    expect(screen.getByRole('button', { name: 'Submit all 3' })).toBeDisabled();
  });

  it('sends some now without final, carrying the stored answers too', async () => {
    const { onSendRound } = renderSending({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Logging/));
    fireEvent.click(option(/No stack/));
    fireEvent.click(tab(/Submit/));
    fireEvent.click(screen.getByRole('button', { name: 'Send 2 now, answer the rest later' }));
    await waitFor(() => expect(onSendRound).toHaveBeenCalledTimes(1));
    const [, reply] = onSendRound.mock.calls[0] as unknown as [string, RoundReply];
    expect(reply).toEqual({ prompt_id: 'round:1', answers: [{ number: 1, option: 1 }, { number: 2, option: 2 }], decisions: [] });
  });

  it('has nothing to send now when every answer is already stored', () => {
    renderSending({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Submit/));
    expect(screen.getByRole('button', { name: 'Send 1 now, answer the rest later' })).toBeDisabled();
  });

  it('shows a failed send and allows another try', async () => {
    const onSendRound = vi.fn(async () => {
      throw new Error('backend unavailable');
    });
    renderSending({}, onSendRound);
    answerAll();
    fireEvent.click(tab(/Submit/));
    fireEvent.click(screen.getByRole('button', { name: 'Submit all 3' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('backend unavailable');
    expect(screen.getByRole('button', { name: 'Submit all 3' })).toBeEnabled();
  });

  it('decides assumptions on their own tab and needs every decision before submit', async () => {
    const { onSendRound } = renderSending({
      assumptions: [{ id: 'A1', text: 'One cluster per environment.' }, { id: 'A2', text: 'Nightly backups are enough.' }],
    });
    answerAll();
    fireEvent.click(tab(/Submit/));
    expect(screen.getByRole('button', { name: 'Submit all 5' })).toBeDisabled();
    fireEvent.click(tab(/Assumptions/));
    const a1 = screen.getByRole('group', { name: 'Assumption 1' });
    const a2 = screen.getByRole('group', { name: 'Assumption 2' });
    fireEvent.click(within(a1).getByRole('button', { name: 'Accept' }));
    fireEvent.click(within(a2).getByRole('button', { name: 'Turn into a question' }));
    expect(within(a2).getByRole('button', { name: 'Turn into a question' })).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(tab(/Submit/));
    const rows = screen.getAllByRole('row').map((r) => Array.from((r as HTMLTableRowElement).cells).map((c) => c.textContent));
    expect(rows.slice(-2)).toEqual([
      ['Assumption 1', 'Accepted'],
      ['Assumption 2', 'Turn into a question'],
    ]);
    fireEvent.click(screen.getByRole('button', { name: 'Submit all 5' }));
    await waitFor(() => expect(onSendRound).toHaveBeenCalledTimes(1));
    const [, reply] = onSendRound.mock.calls[0] as unknown as [string, RoundReply];
    expect(reply.decisions).toEqual([{ id: 'A1', accept: true }, { id: 'A2', accept: false }]);
    expect(screen.queryByText(/\bA[12]\b/)).not.toBeInTheDocument();
  });

  it('chats about the round through the plain message path, on every tab', async () => {
    const { onSendChat } = renderSending({ chat: [{ from: 'system_designer', text: 'Ask me anything.' }] });
    for (const name of [/Deploy tooling/, /Submit/]) {
      fireEvent.click(tab(name));
      expect(screen.getByRole('button', { name: /Chat about these questions/ })).toBeInTheDocument();
    }
    expect(screen.getByText(/Ask me anything\./)).toBeInTheDocument();
    fireEvent.click(tab(/Deploy tooling/));
    const box = screen.getByRole('textbox', { name: 'Message about these questions' });
    fireEvent.change(box, { target: { value: ' Does Q3 change Q1? ' } });
    fireEvent.keyDown(box, { key: '1' });
    expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'false');
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(onSendChat).toHaveBeenCalledWith('Does Q3 change Q1?'));
    await waitFor(() => expect(box).toHaveValue(''));
  });

  it('keeps the chat box enabled and focused while a send is in flight, without double sending', async () => {
    let finish!: () => void;
    const onSendChat = vi.fn(() => new Promise<void>((r) => (finish = r)));
    renderSending({ chat: [{ from: 'system_designer', text: 'Hi.' }] }, undefined, onSendChat);
    const box = screen.getByRole('textbox', { name: 'Message about these questions' });
    box.focus();
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSendChat).toHaveBeenCalledTimes(1);
    expect(box).not.toBeDisabled();
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSendChat).toHaveBeenCalledTimes(1);
    finish();
    await waitFor(() => expect(box).toHaveValue(''));
    expect(box).toHaveFocus();
  });

  it('opens the chat on a click when the round has no thread yet', () => {
    renderSending();
    expect(screen.queryByRole('textbox', { name: 'Message about these questions' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Chat about these questions/ }));
    expect(screen.getByRole('textbox', { name: 'Message about these questions' })).toBeInTheDocument();
  });

  it('a responder round has no Submit tab and no chat', () => {
    renderBlock({ responder: true }, 'readonly');
    expect(screen.queryByRole('tab', { name: /Submit/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Chat about these questions/ })).not.toBeInTheDocument();
  });
});
