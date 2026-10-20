import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { ScaleBar } from './ScaleBar';
import { THREE_SLICES, ref, viewAtDesignGate, viewEndedAtSlicing, viewEndedInSlice, viewOneSlice, viewSliced } from '../../test-utils/blueprintFixtures';
import { stageText } from './stages';

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

  it('offers the mode beside the cost until the pipeline ends', () => {
    const { unmount } = render(<ScaleBar chatId="S1" view={viewAtDesignGate()} compact={false} onOpenStage={() => {}} />);
    expect(screen.getByRole('button', { name: /^Mode:/ })).toBeInTheDocument();
    unmount();
    render(<ScaleBar chatId="S1" view={viewEndedAtSlicing()} compact={false} onOpenStage={() => {}} />);
    expect(screen.queryByRole('button', { name: /^Mode:/ })).not.toBeInTheDocument();
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

describe('ScaleBar on an ended blueprint', () => {
  it('folds the technical end reason under the plain one beside the bar', () => {
    const view = {
      ...viewEndedAtSlicing(),
      end_reason: 'The architect found nothing it could build on.',
      end_detail: 'slicing stored no slices, so nothing could be designed',
    };
    render(<ScaleBar chatId="S3" view={view} compact={false} onOpenStage={() => {}} />);
    const where = screen.getByRole('status');
    expect(where.querySelector('span.bp-failed')).toHaveTextContent(view.end_reason);
    expect(where.querySelector('span.bp-failed')).not.toHaveTextContent(view.end_detail);
    const fold = within(where).getByText(view.end_detail).closest('details');
    expect(fold).not.toBeNull();
    expect(fold).not.toHaveAttribute('open');
  });

  it('draws the stopping step in the failed tone and every step after it as not reached', () => {
    render(<ScaleBar chatId="S3" view={viewEndedAtSlicing()} compact={false} onOpenStage={() => {}} />);
    expect(tones()).toEqual(['done', 'failed', ...Array(7).fill('not-reached')]);
  });

  it('names the stopping step and the end reason beside the bar', () => {
    const view = viewEndedAtSlicing();
    render(<ScaleBar chatId="S3" view={view} compact={false} onOpenStage={() => {}} />);
    const where = screen.getByRole('status');
    expect(where).toHaveTextContent(stageText('slicing').name);
    expect(where).toHaveTextContent(view.end_reason!);
    expect(where.querySelector('span')).toHaveClass('bp-failed');
    expect(where.querySelector('span')?.getAttribute('title')).toContain(view.end_reason!);
  });

  it('opens the stopping step and never a step the pipeline did not reach, even one holding an artifact', () => {
    const onOpen = vi.fn();
    const view = viewEndedAtSlicing();
    view.artifacts = [...view.artifacts, ref('solution_design', 1, 'superseded')];
    render(<ScaleBar chatId="S3" view={view} compact={false} onOpenStage={onOpen} />);
    expect(within(screen.getAllByRole('listitem')[2]).queryByRole('button')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /^Open Slices/ }));
    expect(onOpen).toHaveBeenCalledWith('slicing', '');
  });

  it('brackets the slice that stopped the pipeline in the failed tone and names it beside the bar', () => {
    render(<ScaleBar chatId="S2" view={viewEndedInSlice()} compact={false} onOpenStage={() => {}} />);
    expect(screen.getByText(`SL2 · ${THREE_SLICES[1].title}`).parentElement).toHaveClass('bp-seg-group--failed');
    expect(screen.getByText('SL3').parentElement).not.toHaveClass('bp-seg-group--failed');
    expect(screen.getByRole('status')).toHaveTextContent(`${stageText('decomposition').name} · SL2`);
  });
});
