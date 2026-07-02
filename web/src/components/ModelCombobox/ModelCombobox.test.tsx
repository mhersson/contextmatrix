import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ModelCombobox } from './ModelCombobox';

const OPTIONS = ['anthropic/claude-sonnet-4.5', 'anthropic/claude-opus-4.8', 'qwen/qwen3-coder'];

type Props = Parameters<typeof ModelCombobox>[0];

function setup(props: Partial<Props> = {}) {
  const onChange = vi.fn();
  render(
    <ModelCombobox value="" onChange={onChange} options={OPTIONS} ariaLabel="Model" {...props} />,
  );
  return { onChange, input: screen.getByLabelText('Model') };
}

describe('ModelCombobox', () => {
  it('filters options as the user types', () => {
    const { input } = setup();
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'opus' } });
    const options = screen.getAllByRole('option');
    expect(options).toHaveLength(1);
    expect(options[0]).toHaveTextContent('anthropic/claude-opus-4.8');
  });

  it('commits a clicked option', () => {
    const { onChange, input } = setup();
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'qwen' } });
    fireEvent.mouseDown(screen.getByRole('option', { name: 'qwen/qwen3-coder' }));
    expect(onChange).toHaveBeenCalledWith('qwen/qwen3-coder');
  });

  it('commits the highlighted option on Enter', () => {
    const { onChange, input } = setup();
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'sonnet' } });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('anthropic/claude-sonnet-4.5');
  });

  it('never commits free text: blur with a non-match reverts', () => {
    const { onChange, input } = setup({ value: 'qwen/qwen3-coder' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'not-a-model' } });
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
    expect(input).toHaveValue('qwen/qwen3-coder');
  });

  it('clearing the input commits the empty string', () => {
    const { onChange, input } = setup({ value: 'qwen/qwen3-coder' });
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: '' } });
    fireEvent.blur(input);
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('falls back to free text when options are empty', () => {
    const { onChange, input } = setup({ options: [] });
    fireEvent.change(input, { target: { value: 'any/slug' } });
    expect(onChange).toHaveBeenCalledWith('any/slug');
  });

  it('flags a value that is not in the catalog', () => {
    setup({ value: 'legacy/delisted-model' });
    expect(screen.getByTitle('Not in the model catalog')).toBeInTheDocument();
  });

  it('sets aria-controls only while the listbox is open, and clears it (with aria-activedescendant) on Escape', () => {
    const { input } = setup();
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'sonnet' } });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(input).toHaveAttribute('aria-controls');
    expect(input).toHaveAttribute('aria-activedescendant');

    fireEvent.keyDown(input, { key: 'Escape' });
    expect(input).not.toHaveAttribute('aria-controls');
    expect(input).not.toHaveAttribute('aria-activedescendant');
  });

  it('does not set aria-controls on a fresh render before the listbox has ever opened', () => {
    const { input } = setup();
    expect(input).not.toHaveAttribute('aria-controls');
  });
});
