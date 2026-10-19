import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, renderHook, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { api } from '../../api/client';
import { clearChatLiveData, useChatLiveData } from '../../hooks/useChatLiveData';
import { BlueprintPane } from './BlueprintPane';
import { useBlueprintArtifacts } from './sheetContext';
import { resetArtifactCache } from './artifactCache';
import { toggleSheet, useSheetButton } from './sheetButtonStore';
import type { BlueprintState } from './state';
import { findArtifact, step, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import { rawRound, roundEntry } from '../../test-utils/roundFixtures';
import type { BlueprintView, LogEntry } from '../../types';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));
function ArtifactsProbe() {
  return <span>{useBlueprintArtifacts().length} refs</span>;
}

// Test switches: the stage whose renderer throws, and a throwing title block.
const broken = vi.hoisted(() => ({ stage: null as string | null, titleBlock: false }));
vi.mock('./renderers/ArtifactRenderer', async (importOriginal) => {
  const real = await importOriginal<typeof import('./renderers/ArtifactRenderer')>();
  return {
    ArtifactRenderer: (props: Parameters<typeof real.ArtifactRenderer>[0]) => {
      if (props.artifact.stage === broken.stage) throw new Error('renderer bug');
      return <real.ArtifactRenderer {...props} />;
    },
  };
});
vi.mock('./TitleBlock', async (importOriginal) => {
  const real = await importOriginal<typeof import('./TitleBlock')>();
  return {
    TitleBlock: (props: Parameters<typeof real.TitleBlock>[0]) => {
      if (broken.titleBlock) throw new Error('title block bug');
      return <real.TitleBlock {...props} />;
    },
  };
});

let paneWidth = 1000;

class FakeResizeObserver {
  callback: ResizeObserverCallback;
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
  }
  observe() {
    this.callback([{ contentRect: { width: paneWidth } } as ResizeObserverEntry], this as unknown as ResizeObserver);
  }
  unobserve() {}
  disconnect() {}
}

const gateRow: LogEntry = {
  ts: '2026-10-17T10:00:00Z',
  card_id: '',
  type: 'text',
  kind: 'prompt',
  seq: 9,
  content: JSON.stringify({
    text: 'Design rev 2 is at your gate.',
    prompt: { id: 'gate:solution_design:v2', kind: 'gate', actions: [{ label: 'Approve', reply: 'approve' }] },
  }),
};

const ready = (): BlueprintState => ({ status: 'ready', view: viewAtDesignGate(), draft: null });

interface PaneOptions {
  phone?: boolean;
  logs?: LogEntry[];
  live?: boolean;
}

function renderPane(state: BlueprintState, over: PaneOptions = {}) {
  const onRetry = vi.fn();
  const onReply = vi.fn().mockResolvedValue(undefined);
  const pane = (o: PaneOptions) => (
    <BlueprintPane
      chatId="S1"
      state={state}
      onRetry={onRetry}
      logs={o.logs ?? []}
      promptsLive={o.live ?? true}
      onReply={onReply}
      phone={o.phone ?? false}
    >
      <div>transcript</div>
    </BlueprintPane>
  );
  const view = render(pane(over));
  return { onRetry, onReply, rerender: (next: PaneOptions) => view.rerender(pane(next)) };
}

describe('BlueprintPane', () => {
  it('publishes nothing for the pane header while the view is loading', () => {
    clearChatLiveData('S1');
    const { result } = renderHook(() => useChatLiveData('S1'));
    renderPane({ status: 'loading' });
    expect(result.current).toBeUndefined();
  });

  it("publishes the view's spend for the pane header and follows a new snapshot", () => {
    clearChatLiveData('S1');
    const pane = (view: BlueprintView) => (
      <BlueprintPane chatId="S1" state={{ status: 'ready', view, draft: null }} onRetry={vi.fn()} logs={[]} promptsLive onReply={vi.fn()} phone={false}>
        <div>transcript</div>
      </BlueprintPane>
    );
    const { result } = renderHook(() => useChatLiveData('S1'));
    const view = viewAtDesignGate();
    const { rerender } = render(pane(view));
    expect(result.current?.blueprintSpentUsd).toBe(view.cost.spent_usd);

    const later = { ...view, seq: view.seq + 1, cost: { ...view.cost, spent_usd: view.cost.spent_usd + 0.5 } };
    rerender(pane(later));
    expect(result.current?.blueprintSpentUsd).toBe(later.cost.spent_usd);
    clearChatLiveData('S1');
  });

  it("publishes the view's mode for the pane header and follows a switch", () => {
    clearChatLiveData('S1');
    const pane = (view: BlueprintView) => (
      <BlueprintPane chatId="S1" state={{ status: 'ready', view, draft: null }} onRetry={vi.fn()} logs={[]} promptsLive onReply={vi.fn()} phone={false}>
        <div>transcript</div>
      </BlueprintPane>
    );
    const { result } = renderHook(() => useChatLiveData('S1'));
    const view = viewAtDesignGate();
    const { rerender } = render(pane(view));
    expect(result.current?.blueprintMode).toBe(view.mode);

    rerender(pane({ ...view, seq: view.seq + 1, mode: 'auto' }));
    expect(result.current?.blueprintMode).toBe('auto');
    clearChatLiveData('S1');
  });

  beforeEach(() => {
    paneWidth = 1000;
    broken.stage = null;
    broken.titleBlock = false;
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    resetArtifactCache();
    vi.mocked(api.getBlueprintArtifact).mockImplementation((_c, stage, slice, version) => Promise.resolve(findArtifact(stage, version, slice)));
  });
  afterEach(() => vi.unstubAllGlobals());

  it("hands the view's artifact refs to transcript rows, and none while the architect is unreachable", () => {
    const pane = (state: BlueprintState) => (
      <BlueprintPane chatId="S1" state={state} onRetry={vi.fn()} logs={[]} promptsLive onReply={vi.fn()} phone={false}>
        <ArtifactsProbe />
      </BlueprintPane>
    );
    const view = render(pane(ready()));
    expect(screen.getByText(`${viewAtDesignGate().artifacts.length} refs`)).toBeInTheDocument();
    view.rerender(pane({ status: 'unreachable' }));
    expect(screen.getByText('0 refs')).toBeInTheDocument();
  });

  it('keeps the transcript and the scale bar when a sheet fails to draw', async () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
    broken.stage = 'solution_design';
    renderPane(ready());
    expect(await screen.findByText('This sheet could not be drawn.')).toBeInTheDocument();
    expect(screen.getByText('transcript')).toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'Blueprint stages' })).toBeInTheDocument();
    quiet.mockRestore();
  });

  it('draws the next sheet after one fails to draw, keeping the picked tab focused', async () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
    broken.stage = 'solution_design';
    renderPane(ready());
    await screen.findByText('This sheet could not be drawn.');
    const need = screen.getByRole('tab', { name: 'Need' });
    need.focus();
    fireEvent.click(need);
    expect(await screen.findByRole('heading', { name: /^Assumptions/ })).toBeInTheDocument();
    expect(screen.queryByText('This sheet could not be drawn.')).not.toBeInTheDocument();
    expect(need).toHaveFocus();
    fireEvent.click(screen.getByRole('tab', { name: 'Design, current stage' }));
    expect(await screen.findByText('This sheet could not be drawn.')).toBeInTheDocument();
    quiet.mockRestore();
  });

  it('keeps focus on a picked tab', async () => {
    renderPane(ready());
    const need = screen.getByRole('tab', { name: 'Need' });
    need.focus();
    fireEvent.click(need);
    expect(await screen.findByRole('article', { name: 'Need rev 1' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Need' })).toHaveFocus();
  });

  it('keeps the transcript when the sheet around the paper fails to draw', () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
    broken.titleBlock = true;
    renderPane(ready());
    expect(screen.getByText('This sheet could not be drawn.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument();
    expect(screen.getByText('transcript')).toBeInTheDocument();
    quiet.mockRestore();
  });

  it('puts the sheet beside the transcript on a wide pane', () => {
    renderPane(ready());
    expect(screen.getByText('transcript')).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Blueprint sheet' })).toBeInTheDocument();
    expect(screen.getByText('Design rev 2 is at your gate')).toBeInTheDocument();
    const divider = screen.getByRole('separator', { name: 'Resize the sheet' });
    expect(divider).toHaveAttribute('aria-valuenow', '46');
    fireEvent.keyDown(divider, { key: 'ArrowLeft' });
    expect(divider).toHaveAttribute('aria-valuenow', '48');
  });

  it('collapses to an edge strip under 760px and opens the sheet over the transcript', () => {
    paneWidth = 700;
    renderPane(ready());
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Open the Design sheet' }));
    expect(screen.getByRole('region', { name: 'Blueprint sheet' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
  });

  it('closes the overlay on Escape and returns focus to the edge strip', () => {
    paneWidth = 700;
    renderPane(ready());
    fireEvent.click(screen.getByRole('button', { name: 'Open the Design sheet' }));
    const region = screen.getByRole('region', { name: 'Blueprint sheet' });
    expect(region.parentElement).toContainElement(document.activeElement as HTMLElement);
    expect(screen.queryByRole('button', { name: 'Open the Design sheet' })).not.toBeInTheDocument();
    fireEvent.keyDown(document.activeElement as Element, { key: 'Escape' });
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open the Design sheet' })).toHaveFocus();
  });

  it('returns focus to the edge strip when the split sheet closes', () => {
    renderPane(ready());
    const close = screen.getByRole('button', { name: 'Close' });
    close.focus();
    fireEvent.click(close);
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open the Design sheet' })).toHaveFocus();
  });

  it('keeps the overlay open when an inner control handled Escape', () => {
    paneWidth = 700;
    const feedbackRow: LogEntry = {
      ...gateRow,
      content: JSON.stringify({
        text: 'Design rev 2 is at your gate.',
        prompt: {
          id: 'gate:solution_design:v2',
          kind: 'gate',
          actions: [{ label: 'Reject with feedback', reply: 'reject', feedback: true }],
        },
      }),
    };
    renderPane(ready(), { logs: [feedbackRow] });
    fireEvent.click(screen.getByRole('button', { name: 'Open the Design sheet' }));
    fireEvent.click(screen.getByRole('button', { name: 'Reject with feedback' }));
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' });
    expect(screen.getByRole('region', { name: 'Blueprint sheet' })).toBeInTheDocument();
  });

  it('moves the stage label into the tooltip under 560px', () => {
    paneWidth = 500;
    renderPane(ready());
    expect(screen.queryByText('Design rev 2 is at your gate')).not.toBeInTheDocument();
    expect(screen.getByText('$0.16 of $2.00')).toBeInTheDocument();
  });

  it('shows no edge strip and no sheet on a phone', () => {
    renderPane(ready(), { phone: true });
    expect(screen.queryByRole('button', { name: /Open the .* sheet/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
  });

  it('says the architect is unreachable on the sheet and hides the scale bar', () => {
    const { onRetry } = renderPane({ status: 'unreachable' });
    expect(screen.queryByRole('list', { name: 'Blueprint stages' })).not.toBeInTheDocument();
    expect(screen.getByText("Can't reach the architect.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalled();
    expect(screen.getByText('transcript')).toBeInTheDocument();
  });

  it('renders a plain transcript when there is no blueprint view', () => {
    renderPane({ status: 'absent' });
    expect(screen.getByText('transcript')).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Blueprint sheet' })).not.toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Blueprint stages' })).not.toBeInTheDocument();
  });

  it('answers the open gate from the sheet through the transcript reply path', async () => {
    const { onReply } = renderPane(ready(), { logs: [gateRow] });
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(onReply).toHaveBeenCalledWith('approve'));
  });

  it('keeps the gate bar with its buttons disabled while the session is not live', () => {
    renderPane(ready(), { logs: [gateRow], live: false });
    const bar = screen.getByRole('region', { name: 'Gate decision' });
    expect(within(bar).getByRole('button', { name: 'Approve' })).toBeDisabled();
  });

  it('drops the gate bar once the gate is answered', () => {
    const answer: LogEntry = { ts: '2026-10-17T10:01:00Z', card_id: '', type: 'user', content: 'approve', seq: 10 };
    renderPane(ready(), { logs: [gateRow, answer] });
    expect(screen.queryByRole('region', { name: 'Gate decision' })).not.toBeInTheDocument();
  });

  it('opens the artifact a finished scale-bar segment names', async () => {
    renderPane(ready());
    fireEvent.click(screen.getByRole('button', { name: /^Open Need/ }));
    expect(await screen.findByRole('article', { name: 'Need rev 1' })).toBeInTheDocument();
  });
});

describe('BlueprintPane on a phone', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    resetArtifactCache();
    vi.mocked(api.getBlueprintArtifact).mockImplementation((_c, stage, slice, version) => Promise.resolve(findArtifact(stage, version, slice)));
  });
  afterEach(() => vi.unstubAllGlobals());

  function HeaderProbe() {
    const button = useSheetButton('S1');
    return <span data-testid="probe">{button ? `open:${button.open}` : 'none'}</span>;
  }

  it('publishes the header button and opens a full-pane sheet from it', () => {
    render(<HeaderProbe />);
    renderPane(ready(), { phone: true });
    expect(screen.getByTestId('probe')).toHaveTextContent('open:false');
    act(() => toggleSheet('S1'));
    expect(screen.getByRole('region', { name: 'Blueprint sheet' })).toBeInTheDocument();
    expect(screen.getByTestId('probe')).toHaveTextContent('open:true');
  });

  it('withdraws the button off the phone layout', () => {
    render(<HeaderProbe />);
    const { rerender } = renderPane(ready(), { phone: true });
    expect(screen.getByTestId('probe')).toHaveTextContent('open:false');
    rerender({ phone: false });
    expect(screen.getByTestId('probe')).toHaveTextContent('none');
  });
  it('shows on the sheet how many questions the open round still waits on', () => {
    const view: BlueprintView = {
      ...viewAtDesignGate(),
      pipeline: [step('intake', 'product_owner', 'waiting'), step('solution_design', 'system_designer', 'pending')],
      current: undefined,
      open: { prompt_id: 'round:1', kind: 'round' },
      artifacts: [],
    };
    renderPane(
      { status: 'ready', view, draft: null },
      { logs: [roundEntry(1, 'round:1', rawRound({ answers: [{ number: 1, option: 1 }] }))] },
    );
    expect(screen.getAllByText('Waiting on 2 questions').length).toBeGreaterThan(0);
    const title = within(screen.getByRole('group', { name: 'Title block' }));
    expect(title.getByText('Waiting on 2 questions')).toHaveAttribute('title', 'Waiting on 2 questions');
  });
});
