import { describe, it, expect, vi } from 'vitest';
import type { ComponentProps } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardDefaultsSection } from './CardDefaultsSection';
import { BUILTIN_CARD_DEFAULTS } from '../../lib/cardDefaults';

function renderSection(overrides: Partial<ComponentProps<typeof CardDefaultsSection>> = {}) {
  const onChange = vi.fn();
  render(
    <CardDefaultsSection
      value={BUILTIN_CARD_DEFAULTS}
      onChange={onChange}
      taskBackend="agent"
      mobMaxParticipants={5}
      mobDefaultParticipants={3}
      mobExecuteCheckpoints={false}
      {...overrides}
    />,
  );
  return onChange;
}

describe('CardDefaultsSection', () => {
  it('renders the built-ins: only create PR is on', () => {
    renderSection();
    expect(screen.getByLabelText('Default autonomous mode')).not.toBeChecked();
    expect(screen.getByLabelText('Default create PR')).toBeChecked();
    expect(screen.getByLabelText('Default mob seats')).toHaveValue('0');
    expect(screen.queryByLabelText('Default wait for CI')).toBeInTheDocument();
  });

  it('toggling autonomous reports the merged value', () => {
    const onChange = renderSection();
    fireEvent.click(screen.getByLabelText('Default autonomous mode'));
    expect(onChange).toHaveBeenCalledWith({ ...BUILTIN_CARD_DEFAULTS, autonomous: true });
  });

  it('enabling mob seats from off defaults the phases to review', () => {
    const onChange = renderSection();
    fireEvent.change(screen.getByLabelText('Default mob seats'), { target: { value: '3' } });
    expect(onChange).toHaveBeenCalledWith({ ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review'] });
  });

  it('turning mob seats off clears the phases', () => {
    const onChange = renderSection({ value: { ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review'] } });
    fireEvent.change(screen.getByLabelText('Default mob seats'), { target: { value: '0' } });
    expect(onChange).toHaveBeenCalledWith({ ...BUILTIN_CARD_DEFAULTS, mob_participants: 0, mob_phases: [] });
  });

  it('shows phase pills only when seats are on and toggles them', () => {
    const onChange = renderSection({ value: { ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review'] } });
    fireEvent.click(screen.getByLabelText('Default mob phase plan'));
    expect(onChange).toHaveBeenCalledWith({ ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review', 'plan'] });
  });

  it('hides the PR gates when create PR is off', () => {
    renderSection({ value: { ...BUILTIN_CARD_DEFAULTS, create_pr: false } });
    expect(screen.queryByLabelText('Default wait for CI')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Default request Copilot review')).not.toBeInTheDocument();
  });

  it('hides agent-only rows without the agent backend', () => {
    renderSection({ taskBackend: '' });
    expect(screen.queryByLabelText('Default maximum capability')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Default mob seats')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Default autonomous mode')).toBeInTheDocument();
  });

  it('disables the execute phase pill when the server has execute checkpoints off', () => {
    renderSection({ value: { ...BUILTIN_CARD_DEFAULTS, mob_participants: 3, mob_phases: ['review'] } });
    expect(screen.getByLabelText('Default mob phase execute')).toBeDisabled();
  });
});
