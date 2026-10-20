import { afterEach, beforeEach, describe, it, expect, vi, type Mock } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MOCKUP_V1, MOCKUP_V2, ref, viewAtMockup } from '../../../test-utils/blueprintFixtures';
import type { BlueprintArtifact, BlueprintView, MockupReply } from '../../../types';
import { mockupUseContent, parseMockup } from '../../Mockup/mockupData';
import { RENDER_MESSAGE, SETTINGS_MESSAGE } from '../../Mockup/frameProtocol';
import { MOCKUP_LABELS } from '../../Mockup/labels';
import { MockupPaneContext, type MockupPane } from '../../Mockup/mockupPane';
import { resetMockupStore } from '../../Mockup/mockupStore';
import { api } from '../../../api/client';
import { resetArtifactCache, useBlueprintArtifact } from '../artifactCache';
import { ArtifactRenderer } from './ArtifactRenderer';

vi.mock('../../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
const get = vi.mocked(api.getBlueprintArtifact);

vi.mock('../../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

const data = MOCKUP_V2.data as {
  suggestions: { id: string; title: string; why: string }[];
  settings: Record<string, string>;
  source: string;
};
const opening = { layout: 'rows', group: 'off', tick: 'strike' };
const byId = (id: string) => data.suggestions.find((s) => s.id === id)!;

const QUIET: MockupPane = { chatId: 'S1', live: true, promptId: null };
let posts: unknown[] = [];

beforeEach(() => {
  resetMockupStore();
  resetArtifactCache();
  get.mockReset();
  posts = [];
});
afterEach(() => {
  cleanup();
});

function show(artifact: BlueprintArtifact, view: BlueprintView = viewAtMockup(), pane: MockupPane = QUIET) {
  const out = render(
    <MockupPaneContext.Provider value={pane}>
      <ArtifactRenderer artifact={artifact} previous={null} mode="plain" view={view} />
    </MockupPaneContext.Provider>,
  );
  const frame = out.container.querySelector('iframe');
  if (frame) {
    // The frame's window is jsdom's own; capture what the page posts to it.
    const win = frame.contentWindow!;
    vi.spyOn(win, 'postMessage').mockImplementation((message: unknown) => {
      posts.push(message);
    });
    fireEvent.load(frame);
  }
  return out;
}

const groupName = (title: string) => screen.getByRole('group', { name: title });

describe('MockupSheet', () => {
  it('runs the revision with its opening settings and shows each suggestion with its why', () => {
    show(MOCKUP_V2);
    expect(posts).toEqual([{ type: RENDER_MESSAGE, source: data.source, settings: opening }]);
    expect(screen.getByRole('switch', { name: byId('group').title })).toHaveAttribute('aria-checked', 'false');
    const style = within(groupName(byId('layout').title)).getAllByRole('button');
    expect(style[0]).toHaveAttribute('aria-pressed', 'true');
    expect(style[1]).toHaveAttribute('aria-pressed', 'false');
    for (const s of data.suggestions) expect(screen.getByText(s.why)).toBeInTheDocument();
    expect(screen.getByRole('region', { name: MOCKUP_LABELS.suggestions })).toBeInTheDocument();
  });

  it('flipping a suggestion redraws the mockup at once', () => {
    show(MOCKUP_V2);
    const sw = screen.getByRole('switch', { name: byId('group').title });
    fireEvent.click(sw);
    expect(sw).toHaveAttribute('aria-checked', 'true');
    expect(posts.at(-1)).toEqual({ type: SETTINGS_MESSAGE, settings: { ...opening, group: 'on' } });
    const cards = within(groupName(byId('layout').title)).getAllByRole('button')[1];
    fireEvent.click(cards);
    expect(cards).toHaveAttribute('aria-pressed', 'true');
    expect(posts.at(-1)).toEqual({ type: SETTINGS_MESSAGE, settings: { ...opening, group: 'on', layout: 'cards' } });
  });

  it('keeps your flips for that revision while the sheet is closed', () => {
    const first = show(MOCKUP_V2);
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    first.unmount();
    posts = [];
    show(MOCKUP_V2);
    expect(screen.getByRole('switch', { name: byId('group').title })).toHaveAttribute('aria-checked', 'true');
    expect(posts[0]).toMatchObject({ type: RENDER_MESSAGE, settings: { ...opening, group: 'on' } });
    cleanup();
    posts = [];
    show(MOCKUP_V1);
    expect(posts[0]).toMatchObject({ type: RENDER_MESSAGE, settings: opening });
  });

  it('fixes every control once a design is chosen and shows the chosen settings', () => {
    const chosen = { layout: 'cards', group: 'on', tick: 'hide' };
    const view = viewAtMockup();
    view.open = undefined;
    view.artifacts = [ref('intake', 1, 'current'), ref('mockup', 1, 'superseded'), ref('mockup', 2, 'approved', { decided_by: 'you' })];
    show({ ...MOCKUP_V2, data: { ...data, chosen } }, view);
    expect(posts[0]).toMatchObject({ type: RENDER_MESSAGE, settings: chosen });
    const controls = [...screen.getAllByRole('switch'), ...screen.getAllByRole('button')];
    expect(controls.length).toBeGreaterThan(3);
    for (const c of controls) expect(c).toBeDisabled();
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    expect(posts).toHaveLength(1);
  });

  it('draws a revision without a mockup as its notes', () => {
    const { container } = show({ ...MOCKUP_V2, data: {} });
    expect(container.querySelector('iframe')).toBeNull();
    expect(screen.getByTestId('md')).toHaveTextContent(MOCKUP_V2.body);
  });

  it('draws the same artifact again without posting to the frame', () => {
    const view = viewAtMockup();
    const { rerender } = show(MOCKUP_V2, view);
    const again = (
      <MockupPaneContext.Provider value={QUIET}>
        <ArtifactRenderer artifact={MOCKUP_V2} previous={null} mode="plain" view={{ ...view }} />
      </MockupPaneContext.Provider>
    );
    rerender(again);
    rerender(again);
    expect(posts).toHaveLength(1);
  });
});

describe('MockupSheet when a design is chosen live', () => {
  const approved = () => {
    const view = viewAtMockup();
    view.open = undefined;
    view.artifacts = [ref('intake', 1, 'current'), ref('mockup', 1, 'superseded'), ref('mockup', 2, 'approved', { decided_by: 'you' })];
    return view;
  };
  const chosen = { layout: 'cards', group: 'on', tick: 'hide' };

  // Reads rev 2 through the artifact cache, as the sheet's paper does.
  function Live({ view }: { view: BlueprintView }) {
    const entry = useBlueprintArtifact('S1', 'mockup', '', 2);
    if (entry?.status !== 'ready') return null;
    return (
      <MockupPaneContext.Provider value={QUIET}>
        <ArtifactRenderer artifact={entry.artifact} previous={null} mode="plain" view={view} />
      </MockupPaneContext.Provider>
    );
  }

  async function open() {
    get.mockResolvedValueOnce(MOCKUP_V2);
    const out = render(<Live view={viewAtMockup()} />);
    const frame = await waitFor(() => {
      const f = out.container.querySelector('iframe');
      expect(f).not.toBeNull();
      return f!;
    });
    vi.spyOn(frame.contentWindow!, 'postMessage').mockImplementation((message: unknown) => {
      posts.push(message);
    });
    fireEvent.load(frame);
    return out;
  }

  it('fetches the chosen revision once and then shows what was chosen', async () => {
    const out = await open();
    expect(get).toHaveBeenCalledTimes(1);
    get.mockResolvedValueOnce({ ...MOCKUP_V2, data: { ...data, chosen } });
    out.rerender(<Live view={approved()} />);
    await waitFor(() => expect(posts.at(-1)).toEqual({ type: SETTINGS_MESSAGE, settings: chosen }));
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('keeps the person\'s flips on screen until the chosen settings arrive', async () => {
    const out = await open();
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    const posted = posts.length;
    let finish: (a: BlueprintArtifact) => void = () => {};
    get.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    out.rerender(<Live view={approved()} />);
    expect(screen.getByRole('switch', { name: byId('group').title })).toHaveAttribute('aria-checked', 'true');
    expect(posts).toHaveLength(posted);
    await act(async () => finish({ ...MOCKUP_V2, data: { ...data, chosen } }));
    await waitFor(() => expect(posts.at(-1)).toEqual({ type: SETTINGS_MESSAGE, settings: chosen }));
  });

  it('does not refetch again when the refetched data still lacks chosen', async () => {
    const out = await open();
    get.mockResolvedValue({ ...MOCKUP_V2, data: { ...data } });
    out.rerender(<Live view={approved()} />);
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    out.rerender(<Live view={{ ...approved() }} />);
    await act(async () => {});
    expect(get).toHaveBeenCalledTimes(2);
  });
});

describe('MockupSheet choice', () => {
  let send: Mock<(content: string, reply: MockupReply) => Promise<void>>;
  let pane: MockupPane;
  beforeEach(() => {
    send = vi.fn().mockResolvedValue(undefined);
    pane = { chatId: 'S1', live: true, promptId: 'mockup:1', send };
  });
  const use = () => screen.getByRole('button', { name: MOCKUP_LABELS.use });
  const skip = () => screen.getByRole('button', { name: MOCKUP_LABELS.skip });
  const noChoice = () => {
    expect(screen.queryByRole('button', { name: MOCKUP_LABELS.use })).toBeNull();
    expect(screen.queryByRole('button', { name: MOCKUP_LABELS.skip })).toBeNull();
  };

  it('Use this design sends every setting you see as your answers', async () => {
    show(MOCKUP_V2, viewAtMockup(), pane);
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    fireEvent.click(use());
    const settings = { layout: 'rows', group: 'on', tick: 'strike' };
    const mockup = parseMockup(MOCKUP_V2.data)!;
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith(mockupUseContent(2, mockup.suggestions, settings), {
        prompt_id: 'mockup:1',
        revision: 2,
        action: 'use',
        settings,
      }),
    );
  });

  it('Skip mockups sends a skip without settings', async () => {
    show(MOCKUP_V2, viewAtMockup(), pane);
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    fireEvent.click(skip());
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith(MOCKUP_LABELS.skip, { prompt_id: 'mockup:1', revision: 2, action: 'skip' }),
    );
  });

  it('flipping sends nothing', () => {
    show(MOCKUP_V2, viewAtMockup(), pane);
    fireEvent.click(screen.getByRole('switch', { name: byId('group').title }));
    expect(send).not.toHaveBeenCalled();
  });

  it('offers the choice only on the newest revision while it waits for you', () => {
    show(MOCKUP_V1, viewAtMockup(), pane);
    noChoice();
    cleanup();
    show(MOCKUP_V2, viewAtMockup(), { ...pane, promptId: null });
    noChoice();
    cleanup();
    const decided = viewAtMockup();
    decided.open = undefined;
    decided.artifacts = [ref('intake', 1, 'current'), ref('mockup', 1, 'superseded'), ref('mockup', 2, 'approved')];
    show(MOCKUP_V2, decided, pane);
    noChoice();
  });

  it('moves the choice to a revision that lands while you look at the older one', () => {
    const view = viewAtMockup();
    const { rerender } = show(MOCKUP_V2, view, pane);
    expect(use()).toBeEnabled();
    const later = viewAtMockup();
    later.artifacts = [
      ref('intake', 1, 'current'),
      ref('mockup', 1, 'superseded'),
      ref('mockup', 2, 'superseded'),
      ref('mockup', 3, 'current'),
    ];
    rerender(
      <MockupPaneContext.Provider value={pane}>
        <ArtifactRenderer artifact={MOCKUP_V2} previous={null} mode="plain" view={later} />
      </MockupPaneContext.Provider>,
    );
    noChoice();
    expect(send).not.toHaveBeenCalled();
  });

  it('keeps both buttons disabled after a choice was sent, until a new revision lands', async () => {
    const { rerender } = show(MOCKUP_V2, viewAtMockup(), pane);
    fireEvent.click(use());
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    await act(async () => {});
    expect(use()).toBeDisabled();
    expect(skip()).toBeDisabled();
    fireEvent.click(use());
    expect(send).toHaveBeenCalledTimes(1);
    const later = viewAtMockup();
    later.artifacts = [ref('intake', 1, 'current'), ref('mockup', 2, 'superseded'), ref('mockup', 3, 'current')];
    rerender(
      <MockupPaneContext.Provider value={pane}>
        <ArtifactRenderer artifact={{ ...MOCKUP_V2, version: 3 }} previous={null} mode="plain" view={later} />
      </MockupPaneContext.Provider>,
    );
    expect(use()).toBeEnabled();
    expect(skip()).toBeEnabled();
  });

  it('pauses the choice while the session is not live', () => {
    show(MOCKUP_V2, viewAtMockup(), { ...pane, live: false });
    expect(use()).toBeDisabled();
    expect(skip()).toBeDisabled();
    const sw = screen.getByRole('switch', { name: byId('group').title });
    fireEvent.click(sw);
    expect(sw).toHaveAttribute('aria-checked', 'true');
  });

  it('holds both buttons while a choice is sent and shows a failed send', async () => {
    let fail: (e: Error) => void = () => {};
    send.mockReturnValueOnce(new Promise((_, reject) => { fail = reject; }));
    show(MOCKUP_V2, viewAtMockup(), pane);
    fireEvent.click(use());
    expect(use()).toBeDisabled();
    expect(skip()).toBeDisabled();
    await act(async () => fail(new Error('architect is unreachable')));
    expect(await screen.findByRole('alert')).toHaveTextContent('architect is unreachable');
    expect(use()).toBeEnabled();
    expect(skip()).toBeEnabled();
  });
});
