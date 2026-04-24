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
  onBaseBranchChange: vi.fn(),
  branches: ['main', 'develop'],
};

describe('AutomationCheckboxes — checkboxes', () => {
  it('renders all four checkboxes regardless of props', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.getByLabelText('Autonomous mode')).toBeInTheDocument();
    expect(screen.getByLabelText('Opus as orchestrator')).toBeInTheDocument();
    expect(screen.getByLabelText('Feature branch')).toBeInTheDocument();
    expect(screen.getByLabelText('Create PR')).toBeInTheDocument();
  });

  it('does not render Run HITL / Run Auto button (primary action moved to header)', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByRole('button', { name: 'Run HITL' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run Auto' })).not.toBeInTheDocument();
  });
});

describe('AutomationCheckboxes — Opus orchestrator label', () => {
  it('shows "Sonnet (default)" hint when Opus is unticked', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={false} />);
    expect(screen.getByText(/Sonnet \(default\)/)).toBeInTheDocument();
  });

  it('shows "Opus" hint when Opus is ticked', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={true} />);
    expect(screen.getByText(/Opus.*deeper planning/)).toBeInTheDocument();
  });

  it('reflects useOpusOrchestrator=false (unchecked)', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={false} />);
    expect(screen.getByLabelText('Opus as orchestrator')).not.toBeChecked();
  });

  it('reflects useOpusOrchestrator=true (checked)', () => {
    render(<AutomationCheckboxes {...baseProps} useOpusOrchestrator={true} />);
    expect(screen.getByLabelText('Opus as orchestrator')).toBeChecked();
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
  it('renders base branch selector', () => {
    render(<AutomationCheckboxes {...baseProps} />);
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
    expect(screen.getByText(/1 review attempt · max 2/)).toBeInTheDocument();
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
