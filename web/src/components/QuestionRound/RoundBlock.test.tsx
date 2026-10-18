import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { RoundReply } from '../../types';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound } from './parseRound';
import { RoundBlock } from './RoundBlock';
import type { RoundMode } from './roundView';
import { DRAFT_STORAGE_KEY } from './roundDraftStore';

// Node 25 ships a localStorage without .clear(); back it with a plain map.
const store = new Map<string, string>();
const storageMock = {
  getItem: vi.fn((k: string) => store.get(k) ?? null),
  setItem: vi.fn((k: string, v: string) => void store.set(k, v)),
  removeItem: vi.fn((k: string) => void store.delete(k)),
  clear: vi.fn(() => store.clear()),
};
Object.defineProperty(globalThis, 'localStorage', { value: storageMock, configurable: true });
beforeEach(() => store.clear());

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

  it('keeps an option picked after a typed answer when the answer box only gains focus', () => {
    renderBlock();
    const own = screen.getByRole('textbox', { name: /Your own answer to Q1/ });
    fireEvent.change(own, { target: { value: 'Plain YAML' } });
    fireEvent.click(option(/Helm/));
    fireEvent.focus(own);
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(tab(/Submit/));
    const rows = screen.getAllByRole('row').map((r) => Array.from((r as HTMLTableRowElement).cells).map((c) => c.textContent));
    expect(rows[0]).toEqual(['Q1 Deploy tooling', 'Helm']);
  });

  it('shows code sources beside model knowledge when research is off and counts only those', () => {
    renderBlock({
      research_off: true,
      questions: [
        {
          number: 1,
          title: 'T',
          text: 'Q?',
          options: [
            {
              title: 'Alpha',
              pros: ['p'],
              sources: [
                { tag: 'C1', title: 'internal/api/chats.go' },
                { tag: 'S1', title: 'a web page', url: 'https://a.example/' },
                { tag: 'S2', title: 'evil', url: 'javascript:alert(1)' },
              ],
            },
          ],
        },
      ],
    });
    expect(option(/Alpha/)).toHaveTextContent(/pros · 2 sources$/);
    fireEvent.click(option(/Alpha/));
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText('internal/api/chats.go')).toBeInTheDocument();
    expect(screen.getByText('evil')).toBeInTheDocument();
    expect(screen.queryByText('a web page')).not.toBeInTheDocument();
    expect(screen.getByText(/model knowledge/)).toBeInTheDocument();
  });

  it('hints the option digits only on a question tab', () => {
    const digits = '← → switch questions · 1-4 pick an option · Enter next';
    renderBlock({ assumptions: [{ id: 'A1', text: 'One cluster per environment.' }] });
    expect(screen.getByText(digits)).toBeInTheDocument();
    for (const name of [/Assumptions/, /Submit/]) {
      fireEvent.click(tab(name));
      expect(screen.queryByText(/pick an option/)).not.toBeInTheDocument();
    }
    fireEvent.click(tab(/Postgres/));
    expect(screen.getByText(digits)).toBeInTheDocument();
  });

  it('hints no option digits on a round of assumptions only', () => {
    renderBlock({ questions: [], assumptions: [{ id: 'A1', text: 'One cluster per environment.' }] });
    expect(screen.queryByText(/pick an option/)).not.toBeInTheDocument();
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

  it('keeps the chat box enabled while a send is in flight, without double sending', async () => {
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

  it('keeps text typed and an option focused during a chat send', async () => {
    let finish!: () => void;
    const onSendChat = vi.fn(() => new Promise<void>((r) => (finish = r)));
    renderSending({ chat: [{ from: 'system_designer', text: 'Hi.' }] }, undefined, onSendChat);
    const box = screen.getByRole('textbox', { name: 'Message about these questions' });
    box.focus();
    fireEvent.change(box, { target: { value: 'hello' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    fireEvent.change(box, { target: { value: 'hello, and one more thing' } });
    option(/Kustomize/).focus();
    finish();
    await waitFor(() => expect(onSendChat).toHaveBeenCalledWith('hello'));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled());
    expect(box).toHaveValue('hello, and one more thing');
    expect(option(/Kustomize/)).toHaveFocus();
  });

  it('returns focus to the chat box after a click on Send', async () => {
    const { onSendChat } = renderSending({ chat: [{ from: 'system_designer', text: 'Hi.' }] });
    const box = screen.getByRole('textbox', { name: 'Message about these questions' });
    fireEvent.change(box, { target: { value: 'hello' } });
    const send = screen.getByRole('button', { name: 'Send' });
    send.focus();
    fireEvent.click(send);
    await waitFor(() => expect(onSendChat).toHaveBeenCalledWith('hello'));
    await waitFor(() => expect(box).toHaveValue(''));
    expect(box).toHaveFocus();
  });

  it('scrolls the thread on its own, apart from the chat box, and follows the newest message', () => {
    const round = parseRound(rawRound({ chat: [{ from: 'system_designer', text: 'Ask me anything.' }] }))!;
    const props = { promptId: 'round:1', round, mode: 'open' as const, onSendChat: vi.fn(async () => {}) };
    const { rerender } = render(<RoundBlock {...props} sentChat={[]} />);
    const thread = screen.getByRole('log', { name: 'Chat thread' });
    expect(within(thread).getByText('Ask me anything.')).toBeInTheDocument();
    expect(within(thread).queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Message about these questions' })).toBeInTheDocument();

    Object.defineProperty(thread, 'scrollHeight', { configurable: true, value: 480 });
    rerender(<RoundBlock {...props} sentChat={['Does Q3 change Q1?']} />);
    expect(within(thread).getByText('Does Q3 change Q1?')).toBeInTheDocument();
    expect(thread.scrollTop).toBe(480);
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

describe('RoundBlock kept drafts', () => {
  const withA = { assumptions: [{ id: 'A1', text: 'One cluster per environment.' }] };
  function mount(extra: Record<string, unknown> = {}, onSendRound = vi.fn(async () => {})) {
    const round = parseRound(rawRound({ ...withA, ...extra }))!;
    return {
      onSendRound,
      ...render(<RoundBlock key="k" promptId="round:1" round={round} mode="open" draftScope="s1" onSendRound={onSendRound} />),
    };
  }
  const answered = () => screen.getByText(/of 4 answered/).textContent;

  function makeDraft() {
    fireEvent.click(tab(/Deploy tooling/));
    fireEvent.click(option(/Helm/));
    fireEvent.click(tab(/Logging/));
    fireEvent.change(screen.getByRole('textbox', { name: /Your own answer to Q2/ }), { target: { value: 'Loki only' } });
    fireEvent.click(tab(/Assumptions/));
    fireEvent.click(screen.getByRole('button', { name: 'Accept' }));
  }

  it('brings unsent picks, own answers and decisions back after the block is closed and reopened', () => {
    const first = mount();
    makeDraft();
    first.unmount();
    mount();
    expect(answered()).toMatch(/^0 of 4 answered/);
    fireEvent.click(tab(/Deploy tooling/));
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(tab(/Logging/));
    expect(screen.getByRole('textbox', { name: /Your own answer to Q2/ })).toHaveValue('Loki only');
    fireEvent.click(tab(/Assumptions/));
    expect(screen.getByRole('button', { name: 'Accept' })).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(tab(/Submit/));
    const rows = screen.getAllByRole('row').map((r) => Array.from((r as HTMLTableRowElement).cells).map((c) => c.textContent));
    expect(rows).toContainEqual(['Q1 Deploy tooling', 'Helm']);
    expect(rows).toContainEqual(['Q2 Logging', 'Loki only']);
  });

  it('drops a restored pick once the newest frame stores that question', () => {
    const first = mount();
    makeDraft();
    first.unmount();
    mount({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Deploy tooling/));
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'false');
    expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(tab(/Logging/));
    expect(screen.getByRole('textbox', { name: /Your own answer to Q2/ })).toHaveValue('Loki only');
  });

  it('keeps a re-pick over a stored answer across a reopen', () => {
    const first = mount({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Deploy tooling/));
    fireEvent.click(option(/Helm/));
    first.unmount();
    mount({ answers: [{ number: 1, option: 1 }] });
    fireEvent.click(tab(/Deploy tooling/));
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
  });

  it('removes what was sent from storage once the send succeeds', async () => {
    const { onSendRound } = mount();
    makeDraft();
    expect(store.has(DRAFT_STORAGE_KEY)).toBe(true);
    fireEvent.click(tab(/Submit/));
    fireEvent.click(screen.getByRole('button', { name: /Send 3 now/ }));
    await waitFor(() => expect(onSendRound).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(store.get(DRAFT_STORAGE_KEY) ?? '{}').toBe('{}'));
  });

  it('works in memory when storage throws or holds junk', () => {
    store.set(DRAFT_STORAGE_KEY, '{junk');
    storageMock.setItem.mockImplementationOnce(() => {
      throw new Error('blocked');
    });
    mount();
    expect(() => makeDraft()).not.toThrow();
    fireEvent.click(tab(/Deploy tooling/));
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
  });
});
