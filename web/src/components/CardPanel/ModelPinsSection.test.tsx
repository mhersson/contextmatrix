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

  it('renders datalist options when models are supplied, wired to the inputs', () => {
    const { container } = render(
      <ModelPinsSection {...baseProps} models={['openrouter/auto', 'anthropic/claude-opus-4']} />,
    );
    const datalist = container.querySelector('datalist');
    expect(datalist).not.toBeNull();
    const options = datalist!.querySelectorAll('option');
    expect(options).toHaveLength(2);
    expect(options[0]).toHaveAttribute('value', 'openrouter/auto');
    // Each input's `list` must point at this instance's datalist id (the id
    // is useId-generated so two mounted panels don't collide).
    expect(screen.getByLabelText('Orchestrator model pin')).toHaveAttribute(
      'list',
      datalist!.id,
    );
  });

  it('generates distinct datalist ids per instance (no duplicate-id collision)', () => {
    const { container } = render(
      <>
        <ModelPinsSection {...baseProps} models={['openrouter/auto']} />
        <ModelPinsSection {...baseProps} models={['openrouter/auto']} />
      </>,
    );
    const datalists = container.querySelectorAll('datalist');
    expect(datalists).toHaveLength(2);
    expect(datalists[0].id).not.toBe(datalists[1].id);
  });

  it('renders no datalist options when models=[] but still accepts free text', () => {
    const onChange = vi.fn();
    const { container } = render(
      <ModelPinsSection {...baseProps} models={[]} onChange={onChange} />,
    );
    expect(container.querySelectorAll('datalist option')).toHaveLength(0);
    // Free-text fallback: the input still accepts arbitrary text.
    fireEvent.change(screen.getByLabelText('Orchestrator model pin'), {
      target: { value: 'some/custom-slug' },
    });
    expect(onChange).toHaveBeenCalledWith('model_orchestrator', 'some/custom-slug');
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
