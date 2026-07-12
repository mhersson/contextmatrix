import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AutomationCheckboxes } from './AutomationCheckboxes';

const baseProps = {
  autonomous: false,
  featureBranch: false,
  createPR: false,
  onAutonomousChange: vi.fn(),
  onFeatureBranchChange: vi.fn(),
  onCreatePRChange: vi.fn(),
  onModelPinChange: vi.fn(),
  onBaseBranchChange: vi.fn(),
  branches: ['main', 'develop'],
};

describe('AutomationCheckboxes — model steering', () => {
  it('renders the per-role model pins when taskBackend is agent', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    expect(screen.getByLabelText('Orchestrator model pin')).toBeInTheDocument();
  });

  it('renders no model steering when taskBackend is empty', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByLabelText('Orchestrator model pin')).not.toBeInTheDocument();
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
    render(<AutomationCheckboxes {...baseProps} taskBackend="other" />);
    expect(screen.queryByLabelText('Best of N')).not.toBeInTheDocument();
  });

  it('hides the "Best of N" select entirely when taskBackend is unset', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByLabelText('Best of N')).not.toBeInTheDocument();
  });

  it('renders the "Best of N" select in create mode when taskBackend is agent', () => {
    // best_of_n now wires through CreateCardInput, so create mode offers
    // the selector (the branch/PR hints still branch on mode separately).
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" mode="create" />);
    expect(screen.getByLabelText('Best of N')).toBeInTheDocument();
  });

  it('offers Off, 2, 3, 4, 5 as options for the default max of 5', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    // Scoped to the Best-of-N select: the sibling Co-op selector renders the
    // same Off/2/3/4/5 option text, so an unscoped query is ambiguous.
    const select = screen.getByLabelText('Best of N');
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(values).toEqual(['Off', '2', '3', '4', '5']);
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

describe('AutomationCheckboxes — Co-op block', () => {
  it('renders the "Co-op seats" select when taskBackend is agent', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    expect(screen.getByLabelText('Co-op seats')).toBeInTheDocument();
  });

  it('hides the Co-op block when taskBackend is not agent', () => {
    render(<AutomationCheckboxes {...baseProps} />);
    expect(screen.queryByLabelText('Co-op seats')).not.toBeInTheDocument();
  });

  it('hides the Co-op block in create mode when taskBackend is not agent', () => {
    render(<AutomationCheckboxes {...baseProps} mode="create" />);
    expect(screen.queryByLabelText('Co-op seats')).not.toBeInTheDocument();
  });

  it('renders the "Co-op seats" select in create mode when taskBackend is agent', () => {
    // Co-op fields now wire through CreateCardInput, so create mode offers
    // the selector alongside the Best-of-N one.
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" mode="create" />);
    expect(screen.getByLabelText('Co-op seats')).toBeInTheDocument();
  });

  it('offers Off, 2, 3, 4, 5 for the default max of 5', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" />);
    const select = screen.getByLabelText('Co-op seats');
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(values).toEqual(['Off', '2', '3', '4', '5']);
  });

  it('respects coopMaxParticipants for the option range', () => {
    render(<AutomationCheckboxes {...baseProps} taskBackend="agent" coopMaxParticipants={3} />);
    const select = screen.getByLabelText('Co-op seats');
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(values).toEqual(['Off', '2', '3']);
  });

  it('enabling from Off defaults phases to plan+review', () => {
    const onParticipants = vi.fn();
    const onPhases = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        onCoopParticipantsChange={onParticipants}
        onCoopPhasesChange={onPhases}
      />,
    );
    fireEvent.change(screen.getByLabelText('Co-op seats'), { target: { value: '3' } });
    expect(onParticipants).toHaveBeenCalledWith(3);
    expect(onPhases).toHaveBeenCalledWith(['plan', 'review']);
  });

  it('turning Off clears phases and guests', () => {
    const onPhases = vi.fn();
    const onGuests = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        coopParticipants={3}
        coopPhases={['plan']}
        coopGuests={['laptop']}
        coopGuestNames={['laptop']}
        onCoopParticipantsChange={vi.fn()}
        onCoopPhasesChange={onPhases}
        onCoopGuestsChange={onGuests}
      />,
    );
    fireEvent.change(screen.getByLabelText('Co-op seats'), { target: { value: '0' } });
    expect(onPhases).toHaveBeenCalledWith([]);
    expect(onGuests).toHaveBeenCalledWith([]);
  });

  it('phase chips render only while co-op is on and toggle the phase list', () => {
    const onPhases = vi.fn();
    const { rerender } = render(
      <AutomationCheckboxes {...baseProps} taskBackend="agent" coopParticipants={0} />,
    );
    expect(screen.queryByLabelText('Co-op phase plan')).not.toBeInTheDocument();

    rerender(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        coopParticipants={3}
        coopPhases={['plan', 'review']}
        onCoopPhasesChange={onPhases}
      />,
    );
    expect(screen.getByLabelText('Co-op phase plan')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByLabelText('Co-op phase execute')).toHaveAttribute('aria-pressed', 'false');

    fireEvent.click(screen.getByLabelText('Co-op phase review'));
    expect(onPhases).toHaveBeenCalledWith(['plan']);

    fireEvent.click(screen.getByLabelText('Co-op phase execute'));
    expect(onPhases).toHaveBeenCalledWith(['plan', 'review', 'execute']);
  });

  it('guest chips render from the registry names and toggle selection', () => {
    const onGuests = vi.fn();
    render(
      <AutomationCheckboxes
        {...baseProps}
        taskBackend="agent"
        coopParticipants={3}
        coopGuests={['laptop']}
        coopGuestNames={['laptop', 'desk']}
        onCoopGuestsChange={onGuests}
      />,
    );
    expect(screen.getByLabelText('Co-op guest laptop')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByLabelText('Co-op guest desk')).toHaveAttribute('aria-pressed', 'false');

    fireEvent.click(screen.getByLabelText('Co-op guest desk'));
    expect(onGuests).toHaveBeenCalledWith(['laptop', 'desk']);

    fireEvent.click(screen.getByLabelText('Co-op guest laptop'));
    expect(onGuests).toHaveBeenCalledWith([]);
  });

  it('renders no guest row when the registry is empty', () => {
    render(
      <AutomationCheckboxes {...baseProps} taskBackend="agent" coopParticipants={3} />,
    );
    expect(screen.queryByText('Co-op guests')).not.toBeInTheDocument();
  });
});
