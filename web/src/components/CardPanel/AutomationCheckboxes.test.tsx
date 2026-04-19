import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { AutomationCheckboxes } from './AutomationCheckboxes';

const baseProps = {
  autonomous: false,
  featureBranch: false,
  createPR: false,
  onAutonomousChange: vi.fn(),
  onFeatureBranchChange: vi.fn(),
  onCreatePRChange: vi.fn(),
  onBaseBranchChange: vi.fn(),
  branches: ['main', 'develop'],
};

describe('AutomationCheckboxes — checkboxes', () => {
  it('renders all three checkboxes regardless of props', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.getByLabelText('Autonomous mode')).toBeInTheDocument();
    expect(screen.getByLabelText('Feature branch')).toBeInTheDocument();
    expect(screen.getByLabelText('Create PR')).toBeInTheDocument();
  });
});

describe('AutomationCheckboxes — Run Now button visibility', () => {
  it('renders Run HITL button when canRun=true and onRun is provided and autonomous=false', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        canRun
        onRun={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Run HITL' })).toBeInTheDocument();
  });

  it('does not render run button when canRun=false', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        canRun={false}
        onRun={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });

  it('does not render run button when onRun is omitted', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        canRun
      />,
    );
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });

  it('does not render run button when both canRun and onRun are omitted', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });
});

describe('AutomationCheckboxes — Run button label by mode', () => {
  it('shows Run HITL button when autonomous=false and run controls are present', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous={false}
        canRun
        onRun={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Run HITL' })).toBeInTheDocument();
  });

  it('shows Run Auto button when autonomous=true and run controls are present', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous
        canRun
        onRun={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Run Auto' })).toBeInTheDocument();
  });

  it('does not show Run HITL/Run Auto button when canRun=false', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous={false}
        canRun={false}
        onRun={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });

  it('does not show Run HITL/Run Auto button when onRun is omitted', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous
        canRun
      />,
    );
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });

  it('does not render a standalone AUTO or HITL text label when run controls are visible', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous={false}
        canRun
        onRun={vi.fn()}
      />,
    );
    // The button text contains 'HITL' as part of 'Run HITL', but there must be
    // no standalone element whose entire text content is exactly 'AUTO' or 'HITL'.
    expect(screen.queryByText('AUTO')).not.toBeInTheDocument();
    expect(screen.queryByText('HITL')).not.toBeInTheDocument();
  });
});

describe('AutomationCheckboxes — Run Now interaction', () => {
  it('calls onRun when Run HITL is clicked', async () => {
    const onRun = vi.fn().mockResolvedValue(undefined);
    render(
      <AutomationCheckboxes
        {...baseProps}
        canRun
        onRun={onRun}
      />,
    );
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run HITL' }));
    });
    expect(onRun).toHaveBeenCalledOnce();
  });

  it('disables button and shows Starting... while onRun is pending', async () => {
    let resolve: () => void;
    const onRun = vi.fn().mockReturnValue(
      new Promise<void>((res) => { resolve = res; }),
    );
    render(
      <AutomationCheckboxes
        {...baseProps}
        canRun
        onRun={onRun}
      />,
    );

    const button = screen.getByRole('button', { name: 'Run HITL' });
    fireEvent.click(button);

    expect(await screen.findByRole('button', { name: 'Starting...' })).toBeDisabled();

    await act(async () => { resolve!(); });
    expect(screen.getByRole('button', { name: 'Run HITL' })).not.toBeDisabled();
  });
});

describe('AutomationCheckboxes — base branch selector', () => {
  it('renders base branch selector when autonomous=false', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous={false}
      />,
    );
    expect(screen.getByRole('combobox', { name: 'Base branch' })).toBeInTheDocument();
  });

  it('renders base branch selector when autonomous=true', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        autonomous
      />,
    );
    expect(screen.getByRole('combobox', { name: 'Base branch' })).toBeInTheDocument();
  });

  it('renders branch options', () => {
    render(
      <AutomationCheckboxes
        {...baseProps}
        branches={['main', 'develop']}
      />,
    );
    expect(screen.getByRole('option', { name: 'main' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'develop' })).toBeInTheDocument();
  });
});
