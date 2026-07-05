import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AutomationCheckboxes } from './AutomationCheckboxes';

const baseProps = {
  autonomous: false,
  useOpusOrchestrator: false,
  featureBranch: false,
  createPR: false,
  onAutonomousChange: vi.fn(),
  onUseOpusOrchestratorChange: vi.fn(),
  onFeatureBranchChange: vi.fn(),
  onCreatePRChange: vi.fn(),
  onModelPinChange: vi.fn(),
  onBaseBranchChange: vi.fn(),
  branches: ['main', 'develop'],
};

describe('AutomationCheckboxes — Opus orchestrator label', () => {
  it('shows "Sonnet (default)" hint when Opus is unticked', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={false} />);
    expect(screen.getByText(/Sonnet \(default\)/)).toBeInTheDocument();
  });

  it('shows "Opus" hint when Opus is ticked', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={true} />);
    expect(screen.getByText(/Opus.*deeper planning/)).toBeInTheDocument();
  });

  it('calls onUseOpusOrchestratorChange with true when clicked while unchecked', () => {
    const onUseOpusOrchestratorChange = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        useOpusOrchestrator={false}
        onUseOpusOrchestratorChange={onUseOpusOrchestratorChange}
      />,
    );
    fireEvent.click(screen.getByLabelText('Opus as orchestrator'));
    expect(onUseOpusOrchestratorChange).toHaveBeenCalledWith(true);
  });
});

describe('AutomationCheckboxes — forced-on-run badges', () => {
  it('renders ⚡ forced on run badge on Feature branch when forcedFeatureBranch=true', () => {
    render(<AutomationCheckboxes {...baseProps} forcedFeatureBranch />);
    // Two badges share the text; look for the one near Feature branch.
    const forcedMessages = screen.getAllByText(/forced on run/);
    expect(forcedMessages.length).toBeGreaterThanOrEqual(1);
  });

  it('does not render forced badges when both are false', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByText(/forced on run/)).not.toBeInTheDocument();
  });

  it('calls onClearForcedFeatureBranch when user toggles Feature branch', () => {
    const onClear = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        forcedFeatureBranch
        onClearForcedFeatureBranch={onClear}
      />,
    );
    fireEvent.click(screen.getByLabelText('Feature branch'));
    expect(onClear).toHaveBeenCalledOnce();
  });
});

describe('AutomationCheckboxes — base branch selector', () => {
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

describe('AutomationCheckboxes — inline status hints', () => {
  it('renders "not created yet" when branchName is absent', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.getByText(/not created yet/)).toBeInTheDocument();
  });

  it('renders the branch name when set', () => {
    render(<AutomationCheckboxes {...baseProps} branchName="ctxmax-123/foo" />);
    expect(screen.getByText('ctxmax-123/foo')).toBeInTheDocument();
  });

  it('renders the PR as a `PR #N ↗` link when prUrl matches the PR pattern', () => {
    render(<AutomationCheckboxes {...baseProps} prUrl="https://example.com/pr/1" />);
    const link = screen.getByRole('link', { name: /PR #1/ });
    expect(link).toHaveAttribute('href', 'https://example.com/pr/1');
    // Crucially: the visible text is the short label, NOT the full URL.
    expect(link).not.toHaveTextContent('https://example.com/pr/1');
  });

  it('renders the review-attempts line when reviewAttempts > 0', () => {
    render(<AutomationCheckboxes {...baseProps} reviewAttempts={1} />);
    expect(screen.getByText(/1 review attempt · max 5/)).toBeInTheDocument();
  });

  it('renders the locked banner when disabled', () => {
    render(<AutomationCheckboxes {...baseProps} disabled />);
    expect(screen.getByText(/Automation locked during remote run/)).toBeInTheDocument();
  });

  it('keeps the PR link clickable when the section is disabled (e.g. card in done state)', () => {
    const { container } = render(
      <AutomationCheckboxes {...baseProps} disabled prUrl="https://example.com/pr/42" />,
    );
    // The link itself must render with the correct href.
    const link = screen.getByRole('link', { name: /PR #42/ });
    expect(link).toHaveAttribute('href', 'https://example.com/pr/42');
    // The outer automation stack must NOT apply `pointer-events-none`, which
    // would otherwise swallow the link's click.
    const stack = container.querySelector('.bf-auto-stack');
    expect(stack?.className).not.toMatch(/pointer-events-none/);
    // Checkboxes still disabled — the wrapper's opacity cue is enough.
    expect(screen.getByLabelText('Autonomous mode')).toBeDisabled();
    expect(screen.getByLabelText('Create PR')).toBeDisabled();
  });
});

describe('AutomationCheckboxes — Best of N selector', () => {
  it('renders the "Best of N" select when taskBackend is agent', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    expect(screen.getByLabelText('Best of N')).toBeInTheDocument();
  });

  it('hides the "Best of N" select when taskBackend is not agent', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="runner" />);
    expect(screen.queryByLabelText('Best of N')).not.toBeInTheDocument();
  });

  it('hides the "Best of N" select entirely when taskBackend is unset', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByLabelText('Best of N')).not.toBeInTheDocument();
  });

  it('hides the "Best of N" select in create mode even when taskBackend is agent', () => {
    // best_of_n is edit-only in this task: CreateCardInput has no field for
    // it yet, so the create-mode panel must not offer a dead control.
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" mode="create" />);
    expect(screen.queryByLabelText('Best of N')).not.toBeInTheDocument();
  });

  it('offers Off, 2, 3, 4, 5 as options for the default max of 5', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    expect(screen.getByRole('option', { name: 'Off' })).toBeInTheDocument();
    for (const n of ['2', '3', '4', '5']) {
      expect(screen.getByRole('option', { name: n })).toBeInTheDocument();
    }
  });

  it('enabling from Off selects the default and calls onBestOfNChange with it', () => {
    const onBestOfNChange = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        bestOfN={0}
        bestOfNDefault={3}
        onBestOfNChange={onBestOfNChange}
      />,
    );
    fireEvent.change(screen.getByLabelText('Best of N'), { target: { value: '3' } });
    expect(onBestOfNChange).toHaveBeenCalledWith(3);
  });

  it('choosing Off calls onBestOfNChange with 0', () => {
    const onBestOfNChange = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        bestOfN={3}
        onBestOfNChange={onBestOfNChange}
      />,
    );
    fireEvent.change(screen.getByLabelText('Best of N'), { target: { value: '0' } });
    expect(onBestOfNChange).toHaveBeenCalledWith(0);
  });

  it('is a plain pass-through — picking 5 from Off with a default of 3 calls back with 5, not 3', () => {
    // Pins the contract: bestOfNDefault is a tooltip recommendation only.
    // Selecting a value never gets snapped/overridden to the default.
    const onBestOfNChange = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        bestOfN={0}
        bestOfNDefault={3}
        onBestOfNChange={onBestOfNChange}
      />,
    );
    fireEvent.change(screen.getByLabelText('Best of N'), { target: { value: '5' } });
    expect(onBestOfNChange).toHaveBeenCalledWith(5);
    expect(onBestOfNChange).not.toHaveBeenCalledWith(3);
  });
});
