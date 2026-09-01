import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatModelPicker } from './ChatModelPicker';
import type { ChatModel } from '../../types';

// Mock useTheme (returns the favorites map) so the picker renders without
// pulling in the theme provider.
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ favorites: { complex: ['anthropic/claude-opus-4'] } }),
}));

const endpointModels: ChatModel[] = [
  { id: 'claude-sonnet-4-6', label: 'Sonnet 4.6', max_tokens: 1_000_000 },
  { id: 'claude-opus-4-8', label: 'Opus 4.8', max_tokens: 1_000_000 },
];

const flaggedModels: ChatModel[] = [
  { id: 'bad/model', label: 'bad/model', max_tokens: 200_000, blacklisted: true },
  { id: 'good/model', label: 'good/model', max_tokens: 200_000 },
];

const flaggedEndpointModels: ChatModel[] = [
  { id: 'claude-sonnet-4-6', label: 'Sonnet 4.6', max_tokens: 1_000_000 },
  { id: 'claude-opus-4-8', label: 'Opus 4.8', max_tokens: 1_000_000, blacklisted: true },
];

describe('ChatModelPicker', () => {
  it('endpoint mode: renders a <select> of the server models', () => {
    render(
      <ChatModelPicker
        source="endpoint"
        model="claude-sonnet-4-6"
        defaultModel="claude-sonnet-4-6"
        models={endpointModels}
        onChange={vi.fn()}
      />,
    );
    const select = screen.getByLabelText('Model');
    expect(select.tagName).toBe('SELECT');
    expect(screen.getByRole('option', { name: /Sonnet 4\.6/ })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /Opus 4\.8/ })).toBeInTheDocument();
  });

  it('endpoint mode: renders nothing when the model list is empty', () => {
    const { container } = render(
      <ChatModelPicker
        source="endpoint"
        model=""
        defaultModel=""
        models={[]}
        onChange={vi.fn()}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('openrouter mode: renders a strict combobox over the server-provided list', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="openrouter"
        model=""
        defaultModel="anthropic/claude-sonnet-4.5"
        models={[
          { id: 'anthropic/claude-sonnet-4.5', label: 'anthropic/claude-sonnet-4.5', max_tokens: 200000 },
        ]}
        onChange={onChange}
      />,
    );
    const input = screen.getByRole('combobox');
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'sonnet' } });
    fireEvent.mouseDown(screen.getByRole('option', { name: 'anthropic/claude-sonnet-4.5' }));
    expect(onChange).toHaveBeenCalledWith('anthropic/claude-sonnet-4.5');
  });

  it('openrouter mode: empty server list degrades to a free-text input', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="openrouter"
        model=""
        defaultModel=""
        models={[]}
        onChange={onChange}
      />,
    );
    // Renders even though the server `models` list is empty (regression guard
    // for the old `models.length > 0` visibility gate). The fallback input has
    // an implicit role of `textbox` (not `combobox`) - query by label so both
    // branches (combobox vs. plain input) resolve the same way.
    const input = screen.getByLabelText('Model');
    expect(input.tagName).toBe('INPUT');
    expect(screen.getByRole('textbox')).toBe(input);

    fireEvent.change(input, { target: { value: 'deepseek/deepseek-v4' } });
    expect(onChange).toHaveBeenCalledWith('deepseek/deepseek-v4');
  });

  it('openrouter mode: clicking a favorite chip fills the input', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="openrouter"
        model=""
        defaultModel=""
        models={[]}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /anthropic\/claude-opus-4/ }));
    expect(onChange).toHaveBeenCalledWith('anthropic/claude-opus-4');
  });

  it('openrouter mode: marks the selected flagged model with the blacklisted chip, unflagged without', () => {
    render(
      <ChatModelPicker
        source="openrouter"
        model="bad/model"
        defaultModel=""
        models={flaggedModels}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText('blacklisted')).toBeInTheDocument();
  });

  it('openrouter mode: no chip for an unflagged value', () => {
    render(
      <ChatModelPicker
        source="openrouter"
        model="good/model"
        defaultModel=""
        models={flaggedModels}
        onChange={vi.fn()}
      />,
    );
    expect(screen.queryByText('blacklisted')).not.toBeInTheDocument();
  });

  it('openrouter mode: selecting the flagged model still calls onChange with its id', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="openrouter"
        model=""
        defaultModel=""
        models={flaggedModels}
        onChange={onChange}
      />,
    );
    const input = screen.getByRole('combobox');
    fireEvent.focus(input);
    fireEvent.mouseDown(screen.getByRole('option', { name: /bad\/model/ }));
    expect(onChange).toHaveBeenCalledWith('bad/model');
  });

  it('openrouter mode: marks the flagged entry in the dropdown options before anything is committed', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="openrouter"
        model=""
        defaultModel=""
        models={flaggedModels}
        onChange={onChange}
      />,
    );
    const input = screen.getByRole('combobox');
    fireEvent.focus(input);
    expect(screen.getByRole('option', { name: /bad\/model/ })).toHaveTextContent('blacklisted');
    expect(screen.getByRole('option', { name: /good\/model/ })).not.toHaveTextContent(
      'blacklisted',
    );
    expect(onChange).not.toHaveBeenCalled();
  });

  it('endpoint mode: flags blacklisted entries in the option text, unflagged without', () => {
    render(
      <ChatModelPicker
        source="endpoint"
        model="claude-sonnet-4-6"
        defaultModel=""
        models={flaggedEndpointModels}
        onChange={vi.fn()}
      />,
    );
    const flagged = screen.getByRole('option', { name: /Opus 4\.8/ });
    expect(flagged).toHaveTextContent('blacklisted');
    expect(flagged).toHaveTextContent('reported incapable');
    expect(flagged).not.toBeDisabled();
    expect(screen.getByRole('option', { name: /Sonnet 4\.6/ })).not.toHaveTextContent(
      'blacklisted',
    );
  });

  it('endpoint mode: selecting the flagged model still calls onChange with its id', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="endpoint"
        model="claude-sonnet-4-6"
        defaultModel=""
        models={flaggedEndpointModels}
        onChange={onChange}
      />,
    );
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'claude-opus-4-8' } });
    expect(onChange).toHaveBeenCalledWith('claude-opus-4-8');
  });
});
