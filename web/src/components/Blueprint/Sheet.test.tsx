import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { Sheet } from './Sheet';
import { resetArtifactCache } from './artifactCache';
import { holdDraft } from './draft';
import type { GatePrompt } from './sheetModel';
import { findArtifact, viewAtDesignGate, viewAtLedgerGate } from '../../test-utils/blueprintFixtures';
import type { BlueprintView } from '../../types';

vi.mock('../../api/client', () => ({ api: { getBlueprintArtifact: vi.fn() } }));
vi.mock('../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

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
    shown: { stage: 'solution_design', version: 2 },
    onShow: vi.fn(),
    gatePrompt: null,
    onReply: vi.fn().mockResolvedValue(undefined),
    ...over,
  };
}

describe('Sheet', () => {
  it('hands the gate the review of the version it decides, never an earlier one', () => {
    const { rerender } = render(
      <Sheet {...props({ gatePrompt, view: viewAtLedgerGate(), shown: { stage: 'solution_design', version: 3 } })} />,
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
  beforeEach(() => {
    resetArtifactCache();
    vi.mocked(api.getBlueprintArtifact).mockImplementation((_c, stage, version) => Promise.resolve(findArtifact(stage, version)));
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
    expect(p.onShow).toHaveBeenCalledWith('intake', null);
  });

  it('picks a revision from the title block', () => {
    const p = props();
    render(<Sheet {...p} />);
    fireEvent.change(screen.getByRole('combobox', { name: 'Rev' }), { target: { value: '1' } });
    expect(p.onShow).toHaveBeenCalledWith('solution_design', 1);
  });

  it('repeats the open gate only under the version it decides', () => {
    const { rerender } = render(<Sheet {...props({ gatePrompt })} />);
    expect(screen.getByRole('region', { name: 'Gate decision' })).toBeInTheDocument();
    expect(screen.getByText('At your gate')).toBeInTheDocument();
    rerender(<Sheet {...props({ gatePrompt, shown: { stage: 'solution_design', version: 1 } })} />);
    expect(screen.queryByRole('region', { name: 'Gate decision' })).not.toBeInTheDocument();
  });

  it('keeps compare on across snapshots and drops it on another version', async () => {
    const { container, rerender } = render(<Sheet {...props()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compare' }));
    await waitFor(() => expect(container.querySelector('.bp-dec del')).not.toBeNull());
    rerender(<Sheet {...props({ view: viewAtDesignGate() })} />);
    expect(screen.getByRole('checkbox', { name: 'Compare' })).toBeChecked();
    rerender(<Sheet {...props({ shown: { stage: 'intake', version: 1 } })} />);
    rerender(<Sheet {...props()} />);
    expect(screen.getByRole('checkbox', { name: 'Compare' })).not.toBeChecked();
  });

  it('follows the draft to its end unless the reader scrolled up', () => {
    const draft = (text: string) => holdDraft({ stage: 'solution_design', version: 3, offset: 0, text });
    const p = props({ view: writingView(), shown: { stage: 'solution_design', version: 3 }, draft: draft('One.') });
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
});
