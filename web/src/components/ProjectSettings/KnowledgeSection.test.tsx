import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { KnowledgeSection } from './KnowledgeSection';

describe('KnowledgeSection', () => {
  it('with no providers and no value, renders no controls', () => {
    render(
      <KnowledgeSection provider="" scope="" providers={[]} onProviderChange={vi.fn()} onScopeChange={vi.fn()} />,
    );
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('lists None plus each configured name and reports a choice', () => {
    const onProviderChange = vi.fn();
    render(
      <KnowledgeSection
        provider=""
        scope=""
        providers={['intent', 'docs']}
        onProviderChange={onProviderChange}
        onScopeChange={vi.fn()}
      />,
    );
    const select = screen.getByRole('combobox', { name: /knowledge provider/i });
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => (o as HTMLOptionElement).value),
    ).toEqual(['', 'intent', 'docs']);
    fireEvent.change(select, { target: { value: 'intent' } });
    expect(onProviderChange).toHaveBeenCalledWith('intent');
  });

  it('keeps a stale provider selectable and flags it', () => {
    render(
      <KnowledgeSection
        provider="ghost"
        scope="p"
        providers={['intent']}
        onProviderChange={vi.fn()}
        onScopeChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('combobox', { name: /knowledge provider/i })).toHaveValue('ghost');
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('keeps a stale provider clearable when no providers are configured', () => {
    render(
      <KnowledgeSection provider="ghost" scope="p" providers={[]} onProviderChange={vi.fn()} onScopeChange={vi.fn()} />,
    );
    expect(screen.getByRole('combobox', { name: /knowledge provider/i })).toHaveValue('ghost');
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('disables the scope field until a provider is chosen', () => {
    render(
      <KnowledgeSection
        provider=""
        scope=""
        providers={['intent']}
        onProviderChange={vi.fn()}
        onScopeChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('textbox', { name: /knowledge scope/i })).toBeDisabled();
  });

  it('reports scope edits', () => {
    const onScopeChange = vi.fn();
    render(
      <KnowledgeSection
        provider="intent"
        scope=""
        providers={['intent']}
        onProviderChange={vi.fn()}
        onScopeChange={onScopeChange}
      />,
    );
    fireEvent.change(screen.getByRole('textbox', { name: /knowledge scope/i }), { target: { value: 'payments' } });
    expect(onScopeChange).toHaveBeenCalledWith('payments');
  });
});
