import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { ScaleBar } from './ScaleBar';
import { viewAtDesignGate } from '../../test-utils/blueprintFixtures';

function tones() {
  return screen.getAllByRole('listitem').map((li) => li.getAttribute('data-tone'));
}

describe('ScaleBar', () => {
  it('draws done, folded, waiting and pending segments', () => {
    render(<ScaleBar view={viewAtDesignGate()} compact={false} onOpenStage={() => {}} />);
    expect(tones()).toEqual(['done', 'done', 'waiting', 'pending', 'pending', 'pending', 'pending', 'pending', 'pending']);
    expect(screen.getAllByRole('listitem')[1]).toHaveClass('bp-seg--folded');
  });

  it('labels only the current stage, beside the bar', () => {
    render(<ScaleBar view={viewAtDesignGate()} compact={false} onOpenStage={() => {}} />);
    const label = screen.getByRole('status');
    expect(label).toHaveTextContent('Solution design');
    expect(label).toHaveTextContent('Design rev 2 is at your gate');
    expect(label).not.toHaveTextContent('Grounding');
  });

  it('moves the label into the tooltip on a compact pane', () => {
    render(<ScaleBar view={viewAtDesignGate()} compact onOpenStage={() => {}} />);
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
    render(<ScaleBar view={view} compact={false} onOpenStage={() => {}} />);
    expect(tones()[2]).toBe('running');
  });

  it('describes each segment and opens a finished one', () => {
    const onOpenStage = vi.fn();
    render(<ScaleBar view={viewAtDesignGate()} compact={false} onOpenStage={onOpenStage} />);
    fireEvent.click(screen.getByRole('button', { name: /Open Need/ }));
    expect(onOpenStage).toHaveBeenCalledWith('intake');
    const grounding = screen.getAllByRole('listitem')[3];
    expect(within(grounding).getByRole('img')).toHaveAccessibleName('Grounding. Senior developer. Not started');
    expect(within(grounding).queryByRole('button')).not.toBeInTheDocument();
  });

  it('colours the meter by how much of the cap is spent', () => {
    const view = viewAtDesignGate();
    view.cost = { ...view.cost, spent_usd: 1.8 };
    const { container } = render(<ScaleBar view={view} compact={false} onOpenStage={() => {}} />);
    expect(container.querySelector('.bp-cost')).toHaveClass('bp-cost--warn');
  });
});
