import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { BlueprintPane } from './BlueprintPane';
import { resetArtifactCache } from './artifactCache';
import { toggleSheet, useSheetButton } from './sheetButtonStore';
import type { BlueprintState } from './state';
import { findArtifact, viewAtDesignGate } from '../../test-utils/blueprintFixtures';
import type { LogEntry } from '../../types';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

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

function renderPane(state: BlueprintState, over: { phone?: boolean; logs?: LogEntry[] } = {}) {
  const onRetry = vi.fn();
  const onReply = vi.fn().mockResolvedValue(undefined);
  const pane = (o: { phone?: boolean; logs?: LogEntry[] }) => (
    <BlueprintPane
      chatId="S1"
      state={state}
      onRetry={onRetry}
      logs={o.logs ?? []}
      promptsLive
      onReply={onReply}
      phone={o.phone ?? false}
    >
      <div>transcript</div>
    </BlueprintPane>
  );
  const view = render(pane(over));
  return { onRetry, onReply, rerender: (next: { phone?: boolean; logs?: LogEntry[] }) => view.rerender(pane(next)) };
}

describe('BlueprintPane', () => {
  beforeEach(() => {
    paneWidth = 1000;
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    resetArtifactCache();
    vi.mocked(api.getBlueprintArtifact).mockImplementation((_c, stage, version) => Promise.resolve(findArtifact(stage, version)));
  });
  afterEach(() => vi.unstubAllGlobals());

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
    vi.mocked(api.getBlueprintArtifact).mockImplementation((_c, stage, version) => Promise.resolve(findArtifact(stage, version)));
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
});
