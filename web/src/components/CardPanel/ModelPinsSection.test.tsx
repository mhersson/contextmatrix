import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ModelPinsSection } from './ModelPinsSection';

const baseProps = {
  orchestrator: '',
  coder: '',
  reviewer: '',
  onChange: vi.fn(),
  models: [] as string[],
};

describe('ModelPinsSection', () => {
  it('renders three labeled pin inputs', () => {
    render(<ModelPinsSection {...baseProps} />);
    expect(screen.getByLabelText('Orchestrator model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Coder model pin')).toBeInTheDocument();
    expect(screen.getByLabelText('Reviewer model pin')).toBeInTheDocument();
  });

  it('fires onChange with the field name when a pin is typed', () => {
    const onChange = vi.fn();
    render(<ModelPinsSection {...baseProps} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Coder model pin'), {
      target: { value: 'openrouter/auto' },
    });
    expect(onChange).toHaveBeenCalledWith('model_coder', 'openrouter/auto');
  });

  it('fires onChange for each distinct field', () => {
    const onChange = vi.fn();
    render(<ModelPinsSection {...baseProps} onChange={onChange} />);
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
    render(<ModelPinsSection {...baseProps} models={[]} onChange={onChange} />);
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveAttribute('type', 'text');
    fireEvent.change(screen.getByLabelText('Orchestrator model pin'), {
      target: { value: 'some/custom-slug' },
    });
    expect(onChange).toHaveBeenCalledWith('model_orchestrator', 'some/custom-slug');
  });

  it('renders comboboxes when a catalog is supplied and commits only listed slugs', () => {
    const onChange = vi.fn();
    render(
      <ModelPinsSection
        {...baseProps}
        onChange={onChange}
        models={['anthropic/claude-opus-4.8', 'qwen/qwen3-coder']}
      />,
    );
    const input = screen.getByRole('combobox', { name: 'Coder model pin' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'qwen' } });
    fireEvent.mouseDown(screen.getByRole('option', { name: 'qwen/qwen3-coder' }));
    expect(onChange).toHaveBeenCalledWith('model_coder', 'qwen/qwen3-coder');
  });

  it('does not commit free text when a catalog is supplied', () => {
    const onChange = vi.fn();
    render(
      <ModelPinsSection {...baseProps} onChange={onChange} models={['anthropic/claude-opus-4.8']} />,
    );
    const input = screen.getByRole('combobox', { name: 'Reviewer model pin' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'typo/model' } });
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('reflects the bound pin values', () => {
    render(
      <ModelPinsSection
        {...baseProps}
        orchestrator="anthropic/claude-opus-4"
        coder="openrouter/auto"
        reviewer=""
      />,
    );
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveValue('anthropic/claude-opus-4');
    expect(screen.getByLabelText('Coder model pin')).toHaveValue('openrouter/auto');
    expect(screen.getByLabelText('Reviewer model pin')).toHaveValue('');
  });

  it('disables all inputs when disabled', () => {
    render(<ModelPinsSection {...baseProps} disabled />);
    expect(screen.getByLabelText('Orchestrator model pin')).toBeDisabled();
    expect(screen.getByLabelText('Coder model pin')).toBeDisabled();
    expect(screen.getByLabelText('Reviewer model pin')).toBeDisabled();
  });
});
