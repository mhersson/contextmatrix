import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { api } from '../../api/client';
import { Sheet } from './Sheet';
import { useSheetState } from './useSheetState';
import { resolveShown } from './sheetModel';
import { resetArtifactCache } from './artifactCache';
import { holdDraft } from './draft';
import type { GatePrompt } from './sheetModel';
import { findArtifact, ref, viewAtDesignGate, viewAtLedgerGate, viewOneSlice, viewSliced } from '../../test-utils/blueprintFixtures';
import type { BlueprintView } from '../../types';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

const get = vi.mocked(api.getBlueprintArtifact);

const gatePrompt: GatePrompt = {
  prompt: {
    id: 'gate:solution_design:v2',
    kind: 'gate',
    freeText: false,
    actions: [{ label: 'Approve', reply: 'approve', feedback: false, recommended: false }],
  },
  status: 'open',
};

function writingView(): BlueprintView {
  const view = viewAtDesignGate();
  view.open = undefined;
  view.current = { stage: 'solution_design', kind: 'stage', persona: 'system_designer', model: 'm', started_at: '', turns: 2, tool_calls: 1, tokens: 900 };
  return view;
}

type Props = Parameters<typeof Sheet>[0];

function props(over: Partial<Props> = {}): Props {
  return {
    chatId: 'S1',
    view: viewAtDesignGate(),
    draft: null,
    shown: { stage: 'solution_design', slice: '', version: 2 },
    onShow: vi.fn(),
    gatePrompt: null,
    onReply: vi.fn().mockResolvedValue(undefined),
    picked: '',
    ...over,
  };
}

describe('Sheet', () => {
  beforeEach(() => {
    resetArtifactCache();
    get.mockImplementation((_c, stage, slice, version) => Promise.resolve(findArtifact(stage, version, slice)));
  });

  it('dims tabs without an artifact and marks the current stage', () => {
    render(<Sheet {...props()} />);
    expect(screen.getByRole('tab', { name: 'Need' })).toBeEnabled();
    expect(screen.getByRole('tab', { name: 'Grounding' })).toBeDisabled();
    expect(screen.getByRole('tab', { name: 'Design, current stage' })).toHaveAttribute('aria-selected', 'true');
  });

  it('pins a tab the operator picks', () => {
    const p = props();
    render(<Sheet {...p} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Need' }));
    expect(p.onShow).toHaveBeenCalledWith('intake', '', null, '');
  });

  it('picks a revision from the title block', () => {
    const p = props();
    render(<Sheet {...p} />);
    fireEvent.change(screen.getByRole('combobox', { name: 'Rev' }), { target: { value: '1' } });
    expect(p.onShow).toHaveBeenCalledWith('solution_design', '', 1, '');
  });

  it('repeats the open gate only under the version it decides', () => {
    const { rerender } = render(<Sheet {...props({ gatePrompt })} />);
    expect(screen.getByRole('region', { name: 'Gate decision' })).toBeInTheDocument();
    expect(screen.getByText('At your gate')).toBeInTheDocument();
    rerender(<Sheet {...props({ gatePrompt, shown: { stage: 'solution_design', slice: '', version: 1 } })} />);
    expect(screen.queryByRole('region', { name: 'Gate decision' })).not.toBeInTheDocument();
  });

  it('keeps compare on across snapshots and drops it on another version', async () => {
    const { container, rerender } = render(<Sheet {...props()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compare' }));
    await waitFor(() => expect(container.querySelector('.bp-dec del')).not.toBeNull());
    rerender(<Sheet {...props({ view: viewAtDesignGate() })} />);
    expect(screen.getByRole('checkbox', { name: 'Compare' })).toBeChecked();
    rerender(<Sheet {...props({ shown: { stage: 'intake', slice: '', version: 1 } })} />);
    rerender(<Sheet {...props()} />);
    expect(screen.getByRole('checkbox', { name: 'Compare' })).not.toBeChecked();
  });

  it('follows the draft to its end unless the reader scrolled up', () => {
    const draft = (text: string) => holdDraft({ stage: 'solution_design', version: 3, offset: 0, text });
    const p = props({ view: writingView(), shown: { stage: 'solution_design', slice: '', version: 3 }, draft: draft('One.') });
    const { container, rerender } = render(<Sheet {...p} />);
    const wrap = container.querySelector<HTMLElement>('.bp-paperwrap')!;
    Object.defineProperty(wrap, 'scrollHeight', { configurable: true, value: 500 });
    Object.defineProperty(wrap, 'clientHeight', { configurable: true, value: 100 });
    rerender(<Sheet {...p} draft={draft('One.\n\nTwo.')} />);
    expect(wrap.scrollTop).toBe(500);
    wrap.scrollTop = 0;
    fireEvent.scroll(wrap);
    rerender(<Sheet {...p} draft={draft('One.\n\nTwo.\n\nThree.')} />);
    expect(wrap.scrollTop).toBe(0);
  });

  it('closes when asked', () => {
    const onClose = vi.fn();
    render(<Sheet {...props({ onClose })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalled();
  });

  it('hands the gate the review of the version it decides, never an earlier one', () => {
    const { rerender } = render(
      <Sheet {...props({ gatePrompt, view: viewAtLedgerGate(), shown: { stage: 'solution_design', slice: '', version: 3 } })} />,
    );
    expect(screen.getByRole('region', { name: 'Gate decision' })).toHaveTextContent('R5 critical, open');
    expect(screen.getByRole('button', { name: 'Findings (3)' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Senior developer, 3 of 5, failed' })).toBeInTheDocument();
    // An older architect never reviews rev 2: rev 1's review stands in on the title block, not at the gate.
    rerender(<Sheet {...props({ gatePrompt })} />);
    expect(screen.getByRole('region', { name: 'Gate decision' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Findings/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Senior developer, 4 of 5' })).toBeInTheDocument();
  });

  it("reads the shown slice's version", async () => {
    render(<Sheet {...props({ chatId: 'S2', view: viewSliced(), shown: { stage: 'solution_design', slice: 'SL1', version: 1 } })} />);
    expect(await screen.findByText('CloudNativePG cluster with one replica.')).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith('S2', 'solution_design', 'SL1', 1);
    expect(within(screen.getByRole('group', { name: 'Title block' })).getByText('Approved')).toBeInTheDocument();
  });

  it("reads a one-slice blueprint's design with its slice", async () => {
    render(<Sheet {...props({ chatId: 'S2', view: viewOneSlice(), shown: { stage: 'solution_design', slice: 'SL1', version: 1 } })} />);
    expect(await screen.findByText('CloudNativePG cluster with one replica.')).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith('S2', 'solution_design', 'SL1', 1);
  });
});

describe('Sheet slices', () => {
  beforeEach(() => {
    resetArtifactCache();
    get.mockImplementation((_c, stage, slice, version) => Promise.resolve(findArtifact(stage, version, slice)));
  });

  const sliced = (over: Partial<Props> = {}) =>
    props({ chatId: 'S2', view: viewSliced(), shown: { stage: 'solution_design', slice: 'SL2', version: 1 }, ...over });

  it('opens the slice row under a per-slice stage tab only', () => {
    const { rerender } = render(<Sheet {...sliced()} />);
    const row = screen.getByRole('tablist', { name: 'Slices' });
    expect(within(row).getByRole('tab', { name: /^SL2/ })).toHaveAttribute('aria-selected', 'true');
    expect(within(row).getByRole('tab', { name: /^SL2/ })).toHaveClass('bp-slice--wait');
    expect(within(row).getByRole('tab', { name: /^SL3/ })).toBeDisabled();
    expect(screen.getByRole('tab', { name: /^Design/ })).toHaveClass('bp-tab--joined');
    rerender(<Sheet {...sliced({ shown: { stage: 'intake', slice: '', version: 1 } })} />);
    expect(screen.queryByRole('tablist', { name: 'Slices' })).not.toBeInTheDocument();
  });

  it('picks a slice from the row', () => {
    const onShow = vi.fn();
    render(<Sheet {...sliced({ onShow })} />);
    fireEvent.click(within(screen.getByRole('tablist', { name: 'Slices' })).getByRole('tab', { name: /^SL1/ }));
    expect(onShow).toHaveBeenLastCalledWith('solution_design', 'SL1', null);
  });

  it('keeps the chosen slice when switching stage tabs', () => {
    const onShow = vi.fn();
    render(<Sheet {...sliced({ shown: { stage: 'solution_design', slice: 'SL1', version: 1 }, onShow })} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Grounding' }));
    expect(onShow).toHaveBeenLastCalledWith('grounding', 'SL1', null, 'SL1');
  });

  it('returns to the picked slice from a whole-blueprint sheet', () => {
    const onShow = vi.fn();
    render(<Sheet {...sliced({ shown: { stage: 'intake', slice: '', version: 1 }, picked: 'SL1', onShow })} />);
    fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
    expect(onShow).toHaveBeenLastCalledWith('solution_design', 'SL1', null, 'SL1');
  });

  it('prefers the picked slice over the shown one when both have the sheet', () => {
    const onShow = vi.fn();
    const view = viewSliced();
    view.artifacts = [...view.artifacts, ref('grounding', 1, 'current', { slice: 'SL2' })];
    render(<Sheet {...sliced({ view, shown: { stage: 'solution_design', slice: 'SL1', version: 1 }, picked: 'SL2', onShow })} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Grounding' }));
    expect(onShow).toHaveBeenLastCalledWith('grounding', 'SL2', null, 'SL2');
  });

  describe('with the pane state', () => {
    function Harness({ view }: { view: BlueprintView }) {
      const sheet = useSheetState(view, null, 'split');
      const shown = resolveShown(view, null, sheet.pin);
      return <Sheet {...props({ chatId: 'S2', view, shown, onShow: sheet.show, picked: sheet.picked })} />;
    }
    const selectedSlice = () => within(screen.getByRole('tablist', { name: 'Slices' })).getByRole('tab', { selected: true });

    it('returns a stage tab to the chosen slice after a fallback', () => {
      render(<Harness view={viewSliced()} />);
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      expect(selectedSlice()).toHaveTextContent(/^SL2/);
      fireEvent.click(screen.getByRole('tab', { name: 'Grounding' }));
      expect(selectedSlice()).toHaveTextContent(/^SL1/);
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      expect(selectedSlice()).toHaveTextContent(/^SL2/);
    });

    it('keeps the chosen slice when picking a revision of a fallback sheet', () => {
      const view = viewSliced();
      view.artifacts = [...view.artifacts, ref('grounding', 2, 'current', { slice: 'SL1' })];
      get.mockImplementation((_c, stage, slice, version) =>
        stage === 'grounding' ? Promise.reject(new Error('no fixture')) : Promise.resolve(findArtifact(stage, version, slice)),
      );
      render(<Harness view={view} />);
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      fireEvent.click(screen.getByRole('tab', { name: 'Grounding' }));
      expect(selectedSlice()).toHaveTextContent(/^SL1/);
      fireEvent.change(screen.getByRole('combobox'), { target: { value: '1' } });
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      expect(selectedSlice()).toHaveTextContent(/^SL2/);
    });

    it('survives two fallbacks in a row', () => {
      render(<Harness view={viewSliced()} />);
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      fireEvent.click(screen.getByRole('tab', { name: 'Grounding' }));
      fireEvent.click(screen.getByRole('tab', { name: 'Acceptance' }));
      expect(selectedSlice()).toHaveTextContent(/^SL1/);
      fireEvent.click(screen.getByRole('tab', { name: /^Design/ }));
      expect(selectedSlice()).toHaveTextContent(/^SL2/);
    });
  });

  it('names the slice under the sheet title', async () => {
    render(<Sheet {...sliced({ shown: { stage: 'solution_design', slice: 'SL1', version: 1 } })} />);
    expect(await screen.findByText('SL1 · Service running with its database')).toBeInTheDocument();
  });

  it('shows nothing slice-related for one slice', async () => {
    render(<Sheet {...props({ chatId: 'S2', view: viewOneSlice(), shown: { stage: 'solution_design', slice: 'SL1', version: 1 } })} />);
    await screen.findByText('CloudNativePG cluster with one replica.');
    expect(screen.queryByRole('tablist', { name: 'Slices' })).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Shared' })).not.toBeInTheDocument();
    expect(screen.queryByText(/^SL1 · /)).not.toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /^Design/ })).not.toHaveClass('bp-tab--joined');
  });

  it('shows no slice row or separators for a blueprint without slices', () => {
    const { container } = render(<Sheet {...props()} />);
    expect(screen.queryByRole('tablist', { name: 'Slices' })).not.toBeInTheDocument();
    expect(container.querySelector('.bp-tabs-sep')).toBeNull();
  });
});
