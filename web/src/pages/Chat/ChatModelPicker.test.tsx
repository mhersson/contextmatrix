import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatModelPicker } from './ChatModelPicker';
import type { ChatModel } from '../../types';

// Mock the OpenRouter catalog hook (returns slugs) and useTheme (returns the
// favorites map) so the picker renders without any network call.
vi.mock('../../hooks/useOpenRouterModels', () => ({
  useOpenRouterModels: () => ['anthropic/claude-sonnet-4', 'openai/gpt-5'],
}));
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ favorites: { complex: ['anthropic/claude-opus-4'] } }),
}));

const configModels: ChatModel[] = [
  { id: 'claude-sonnet-4-6', label: 'Sonnet 4.6', max_tokens: 1_000_000 },
  { id: 'claude-opus-4-8', label: 'Opus 4.8', max_tokens: 1_000_000 },
];

describe('ChatModelPicker', () => {
  it('config mode: renders a <select> of the allowlist', () => {
    render(
      <ChatModelPicker
        source="config"
        model="claude-sonnet-4-6"
        defaultModel="claude-sonnet-4-6"
        models={configModels}
        onChange={vi.fn()}
      />,
    );
    const select = screen.getByLabelText('Model');
    expect(select.tagName).toBe('SELECT');
    expect(screen.getByRole('option', { name: /Sonnet 4\.6/ })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /Opus 4\.8/ })).toBeInTheDocument();
  });

  it('config mode: renders nothing when the allowlist is empty', () => {
    const { container } = render(
      <ChatModelPicker
        source="config"
        model=""
        defaultModel=""
        models={[]}
        onChange={vi.fn()}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('openrouter mode: renders an input + datalist from the live catalog', () => {
    const { container } = render(
      <ChatModelPicker
        source="openrouter"
        model="anthropic/claude-sonnet-4"
        defaultModel="anthropic/claude-sonnet-4"
        models={[]}
        onChange={vi.fn()}
      />,
    );
    // Renders even though the config `models` list is empty (regression guard
    // for the old `models.length > 0` visibility gate).
    const input = screen.getByLabelText('Model');
    expect(input.tagName).toBe('INPUT');

    const options = container.querySelectorAll('datalist option');
    expect(options).toHaveLength(2);
    expect(options[0]).toHaveAttribute('value', 'anthropic/claude-sonnet-4');
    expect(options[1]).toHaveAttribute('value', 'openai/gpt-5');
  });

  it('openrouter mode: typing a free-text slug fires onChange', () => {
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
    fireEvent.change(screen.getByLabelText('Model'), {
      target: { value: 'deepseek/deepseek-v4' },
    });
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

  it('endpoint mode: renders a <select> over the server-provided models', () => {
    const onChange = vi.fn();
    render(
      <ChatModelPicker
        source="endpoint"
        model="model-a"
        defaultModel="model-a"
        models={[{ id: 'model-a', label: 'Model A', max_tokens: 200000 }]}
        onChange={onChange}
      />,
    );
    const select = screen.getByLabelText('Model');
    expect(select.tagName).toBe('SELECT');
    expect(screen.getByRole('option', { name: /Model A/ })).toBeInTheDocument();
  });
});
