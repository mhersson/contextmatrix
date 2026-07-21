import { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ModelPinsSection, type ModelPinField } from './ModelPinsSection';

/**
 * Stateful harness echoing onChange back into props, the way AutomationTab
 * and CreateCardPanel do - required for behaviors that only manifest when
 * the parent re-renders with the values the section just committed.
 */
function StatefulPins({ initial = {}, models = [] as string[] }: {
  initial?: Partial<Record<ModelPinField, string>>;
  models?: string[];
}) {
  const [pins, setPins] = useState<Record<ModelPinField, string>>({
    model_orchestrator: initial.model_orchestrator ?? '',
    model_coder: initial.model_coder ?? '',
    model_reviewer: initial.model_reviewer ?? '',
  });
  return (
    <ModelPinsSection
      orchestrator={pins.model_orchestrator}
      coder={pins.model_coder}
      reviewer={pins.model_reviewer}
      onChange={(field, value) => setPins((prev) => ({ ...prev, [field]: value }))}
      models={models}
    />
  );
}

const baseProps = {
  orchestrator: '',
  coder: '',
  reviewer: '',
  onChange: vi.fn(),
  models: [] as string[],
};

describe('ModelPinsSection - automatic model selection toggle', () => {
  it('defaults to automatic with the pin inputs hidden when no pin is set', () => {
    render(<ModelPinsSection {...baseProps} />);
    expect(screen.getByLabelText('Automatic model selection')).toBeChecked();
    expect(screen.queryByLabelText('Orchestrator model pin')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Coder model pin')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Reviewer model pin')).not.toBeInTheDocument();
  });

  it('reveals the pin inputs when unchecked', () => {
    render(<ModelPinsSection {...baseProps} />);
    fireEvent.click(screen.getByLabelText('Automatic model selection'));
    expect(screen.getByLabelText('Automatic model selection')).not.toBeChecked();
    expect(screen.getByLabelText('Orchestrator model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Coder model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Reviewer model pin')).toBeInTheDocument();
  });

  it('starts unchecked with pins visible when a pin value is already set', () => {
    render(<ModelPinsSection {...baseProps} coder="openrouter/auto" />);
    expect(screen.getByLabelText('Automatic model selection')).not.toBeChecked();
    expect(screen.getByLabelText('Coder model pin')).toHaveValue('openrouter/auto');
  });

  it('clears every set pin when re-checked', () => {
    const onChange = vi.fn();
    render(
      <ModelPinsSection
        {...baseProps}
        onChange={onChange}
        orchestrator="anthropic/claude-opus-4"
        reviewer="google/gemini-2.5-pro"
      />,
    );
    fireEvent.click(screen.getByLabelText('Automatic model selection'));
    expect(onChange).toHaveBeenCalledWith('model_orchestrator', '');
    expect(onChange).toHaveBeenCalledWith('model_reviewer', '');
    expect(onChange).not.toHaveBeenCalledWith('model_coder', '');
  });

  it('hides the favorites chips while automatic and shows them when revealed', () => {
    render(<ModelPinsSection {...baseProps} favorites={['qwen/qwen3-coder']} />);
    expect(screen.queryByText('qwen/qwen3-coder')).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('Automatic model selection'));
    expect(screen.getByText('qwen/qwen3-coder')).toBeInTheDocument();
  });

  it('disables the toggle when disabled', () => {
    render(<ModelPinsSection {...baseProps} disabled />);
    expect(screen.getByLabelText('Automatic model selection')).toBeDisabled();
  });
});

describe('ModelPinsSection - toggle against a stateful parent', () => {
  it('keeps the section revealed when the user empties the last pin', () => {
    render(<StatefulPins initial={{ model_coder: 'typo/model' }} />);
    fireEvent.change(screen.getByLabelText('Coder model pin'), { target: { value: '' } });
    expect(screen.getByLabelText('Coder model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Automatic model selection')).not.toBeChecked();
  });

  it('derives the toggle off when a pin appears while checked', () => {
    const { rerender } = render(<ModelPinsSection {...baseProps} />);
    expect(screen.getByLabelText('Automatic model selection')).toBeChecked();
    rerender(<ModelPinsSection {...baseProps} coder="openrouter/auto" />);
    expect(screen.getByLabelText('Automatic model selection')).not.toBeChecked();
    expect(screen.getByLabelText('Coder model pin')).toHaveValue('openrouter/auto');
  });

  it('re-check clears and hides, re-uncheck shows empty pins', () => {
    render(
      <StatefulPins
        initial={{ model_orchestrator: 'anthropic/claude-opus-4', model_reviewer: 'google/gemini-2.5-pro' }}
      />,
    );
    const toggle = screen.getByLabelText('Automatic model selection');
    fireEvent.click(toggle);
    expect(toggle).toBeChecked();
    expect(screen.queryByLabelText('Orchestrator model pin')).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).not.toBeChecked();
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveValue('');
    expect(screen.getByLabelText('Reviewer model pin')).toHaveValue('');
  });

  it('hints "selector decides" while automatic and "pin models per role" when revealed', () => {
    render(<ModelPinsSection {...baseProps} />);
    expect(screen.getByText('selector decides')).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('Automatic model selection'));
    expect(screen.queryByText('selector decides')).not.toBeInTheDocument();
    expect(screen.getByText('pin models per role')).toBeInTheDocument();
  });
});

function renderRevealed(props: Partial<Parameters<typeof ModelPinsSection>[0]> = {}) {
  const result = render(<ModelPinsSection {...baseProps} {...props} />);
  const toggle = screen.getByLabelText('Automatic model selection');
  if ((toggle as HTMLInputElement).checked) fireEvent.click(toggle);
  return result;
}

describe('ModelPinsSection', () => {
  it('renders three labeled pin inputs', () => {
    renderRevealed();
    expect(screen.getByLabelText('Orchestrator model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Coder model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Reviewer model pin')).toBeInTheDocument();
  });

  it('fires onChange with the field name when a pin is typed', () => {
    const onChange = vi.fn();
    renderRevealed({ onChange });
    fireEvent.change(screen.getByLabelText('Coder model pin'), {
      target: { value: 'openrouter/auto' },
    });
    expect(onChange).toHaveBeenCalledWith('model_coder', 'openrouter/auto');
  });

  it('fires onChange for each distinct field', () => {
    const onChange = vi.fn();
    renderRevealed({ onChange });
    fireEvent.change(screen.getByLabelText('Orchestrator model pin'), {
      target: { value: 'anthropic/claude-opus-4' },
    });
    fireEvent.change(screen.getByLabelText('Reviewer model pin'), {
      target: { value: 'google/gemini-2.5-pro' },
    });
    expect(onChange).toHaveBeenCalledWith('model_orchestrator', 'anthropic/claude-opus-4');
    expect(onChange).toHaveBeenCalledWith('model_reviewer', 'google/gemini-2.5-pro');
  });

  it('accepts free text when models=[] (no catalog)', () => {
    const onChange = vi.fn();
    renderRevealed({ models: [], onChange });
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveAttribute('type', 'text');
    fireEvent.change(screen.getByLabelText('Orchestrator model pin'), {
      target: { value: 'some/custom-slug' },
    });
    expect(onChange).toHaveBeenCalledWith('model_orchestrator', 'some/custom-slug');
  });

  it('renders comboboxes when a catalog is supplied and commits only listed slugs', () => {
    const onChange = vi.fn();
    renderRevealed({ onChange, models: ['anthropic/claude-opus-4.8', 'qwen/qwen3-coder'] });
    const input = screen.getByRole('combobox', { name: 'Coder model pin' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'qwen' } });
    fireEvent.mouseDown(screen.getByRole('option', { name: 'qwen/qwen3-coder' }));
    expect(onChange).toHaveBeenCalledWith('model_coder', 'qwen/qwen3-coder');
  });

  it('does not commit free text when a catalog is supplied', () => {
    const onChange = vi.fn();
    renderRevealed({ onChange, models: ['anthropic/claude-opus-4.8'] });
    const input = screen.getByRole('combobox', { name: 'Reviewer model pin' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'typo/model' } });
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('reflects the bound pin values', () => {
    renderRevealed({ orchestrator: 'anthropic/claude-opus-4', coder: 'openrouter/auto', reviewer: '' });
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveValue('anthropic/claude-opus-4');
    expect(screen.getByLabelText('Coder model pin')).toHaveValue('openrouter/auto');
    expect(screen.getByLabelText('Reviewer model pin')).toHaveValue('');
  });

  it('disables all inputs when disabled', () => {
    render(<ModelPinsSection {...baseProps} disabled orchestrator="anthropic/claude-opus-4" />);
    expect(screen.getByLabelText('Orchestrator model pin')).toBeDisabled();
    expect(screen.getByLabelText('Coder model pin')).toBeDisabled();
    expect(screen.getByLabelText('Reviewer model pin')).toBeDisabled();
  });
});
