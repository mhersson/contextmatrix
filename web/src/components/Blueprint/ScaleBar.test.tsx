import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { ScaleBar } from './ScaleBar';
import { viewAtDesignGate, viewOneSlice, viewSliced } from '../../test-utils/blueprintFixtures';

function tones() {
  return screen.getAllByRole('listitem').map((li) => li.getAttribute('data-tone'));
}

describe('ScaleBar', () => {
  it('draws done, folded, waiting and pending segments', () => {
    render(<ScaleBar chatId="S1" view={viewAtDesignGate()} compact={false} onOpenStage={() => {}} />);
    expect(tones()).toEqual(['done', 'done', 'waiting', 'pending', 'pending', 'pending', 'pending', 'pending', 'pending']);
    expect(screen.getAllByRole('listitem')[1]).toHaveClass('bp-seg--folded');
  });

  it('labels only the current stage, beside the bar', () => {
    render(<ScaleBar chatId="S1" view={viewAtDesignGate()} compact={false} onOpenStage={() => {}} />);
    const label = screen.getByRole('status');
    expect(label).toHaveTextContent('Solution design');
    expect(label).toHaveTextContent('Design rev 2 is at your gate');
    expect(label).not.toHaveTextContent('Grounding');
  });

  it('moves the label into the tooltip on a compact pane', () => {
    render(<ScaleBar chatId="S1" view={viewAtDesignGate()} compact onOpenStage={() => {}} />);
    expect(screen.queryByText('Design rev 2 is at your gate')).not.toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'Blueprint stages' })).toHaveAttribute(
      'title',
      'Solution design: Design rev 2 is at your gate',
    );
    expect(screen.getByText('$0.16 of $2.00')).toBeInTheDocument();
  });

  it('shows a running run as an aqua hatch', () => {
    const view = viewAtDesignGate();
    view.open = undefined;
    view.pipeline[2] = { ...view.pipeline[2], status: 'running' };
    view.current = { stage: 'solution_design', kind: 'stage', persona: 'system_designer', model: 'm', started_at: '', turns: 1, tool_calls: 0, tokens: 0 };
    render(<ScaleBar chatId="S1" view={view} compact={false} onOpenStage={() => {}} />);
    expect(tones()[2]).toBe('running');
  });

  it('describes each segment and opens a finished one', () => {
    const onOpenStage = vi.fn();
    render(<ScaleBar chatId="S1" view={viewAtDesignGate()} compact={false} onOpenStage={onOpenStage} />);
    fireEvent.click(screen.getByRole('button', { name: /Open Need/ }));
    expect(onOpenStage).toHaveBeenCalledWith('intake', '');
    const grounding = screen.getAllByRole('listitem')[3];
    expect(within(grounding).getByRole('img')).toHaveAccessibleName('Grounding. Senior developer. Not started');
    expect(within(grounding).queryByRole('button')).not.toBeInTheDocument();
  });

  it('colours the meter by how much of the cap is spent', () => {
    const view = viewAtDesignGate();
    view.cost = { ...view.cost, spent_usd: 1.8 };
    const { container } = render(<ScaleBar chatId="S1" view={view} compact={false} onOpenStage={() => {}} />);
    expect(container.querySelector('.bp-cost')).toHaveClass('bp-cost--warn');
  });

  it('opens a finished slice step only when its own slice has the sheet', () => {
    const view = viewSliced();
    view.pipeline = view.pipeline.map((s) => (s.stage === 'grounding' && s.slice === 'SL2' ? { ...s, status: 'done' as const } : s));
    render(<ScaleBar chatId="S2" view={view} compact={false} onOpenStage={() => {}} />);
    expect(screen.getAllByRole('button', { name: /^Open Grounding/ })).toHaveLength(1);
  });

  it('opens a finished slice step on its own sheet', () => {
    const onOpen = vi.fn();
    render(<ScaleBar chatId="S2" view={viewSliced()} compact={false} onOpenStage={onOpen} />);
    fireEvent.click(screen.getByRole('button', { name: /^Open Grounding/ }));
    expect(onOpen).toHaveBeenCalledWith('grounding', 'SL1');
  });
});

describe('ScaleBar slices', () => {
  it("brackets each slice's steps under its id, the current one with its title", () => {
    render(<ScaleBar chatId="S2" view={viewSliced()} compact={false} onOpenStage={() => {}} />);
    expect(screen.getAllByRole('listitem')).toHaveLength(18);
    const sl1 = screen.getByText('SL1');
    expect(within(sl1.parentElement!).getAllByRole('listitem')).toHaveLength(4);
    expect(screen.getByText('SL2 · Logs searchable in the cluster').parentElement).toHaveClass('bp-seg-group--waiting');
    expect(screen.getByText('SL3')).toBeInTheDocument();
  });

  it('names the slice beside the bar and in the segment labels', () => {
    render(<ScaleBar chatId="S2" view={viewSliced()} compact={false} onOpenStage={() => {}} />);
    expect(screen.getByRole('status')).toHaveTextContent('Solution design · SL2');
    expect(screen.getByRole('img', { name: /^Solution design · SL2, gate\. / })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Open Grounding: Grounding · SL1\. / })).toBeInTheDocument();
  });

  it('draws one slice like a blueprint without slices', () => {
    const { container } = render(<ScaleBar chatId="S2" view={viewOneSlice()} compact={false} onOpenStage={() => {}} />);
    expect(screen.getAllByRole('listitem')).toHaveLength(9);
    expect(container.querySelector('.bp-seg-group')).toBeNull();
    expect(screen.getByRole('status')).not.toHaveTextContent('SL1');
  });

  it('marks only the first segment and the last three for edge and tooltip placement', () => {
    render(<ScaleBar chatId="S2" view={viewSliced()} compact={false} onOpenStage={() => {}} />);
    const items = screen.getAllByRole('listitem');
    expect(items.filter((li) => li.classList.contains('bp-seg--first'))).toEqual([items[0]]);
    expect(items.filter((li) => li.classList.contains('bp-seg--end'))).toEqual(items.slice(-3));
  });
});
