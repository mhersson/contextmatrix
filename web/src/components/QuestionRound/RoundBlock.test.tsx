import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { rawRound } from '../../test-utils/roundFixtures';
import { parseRound } from './parseRound';
import { RoundBlock } from './RoundBlock';
import type { RoundMode } from './roundView';

function renderBlock(extra: Record<string, unknown> = {}, mode: RoundMode = 'open') {
  const round = parseRound(rawRound(extra))!;
  return render(<RoundBlock promptId="round:1" round={round} mode={mode} />);
}

const tab = (name: RegExp) => screen.getByRole('tab', { name });
const option = (name: RegExp) => screen.getByRole('button', { name });

describe('RoundBlock questions', () => {
  it('shows one tab per question, ticks answered ones and opens on the first open one', () => {
    renderBlock({ answers: [{ number: 1, option: 1 }] });
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'Deploy tooling, answered',
      'Logging, open',
      'Postgres, open',
    ]);
    expect(tab(/Deploy tooling/)).toHaveClass('done');
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('region', { name: /3 questions from the system designer/ })).toBeInTheDocument();
  });

  it('selects an option on click, expands its pros, cons and sources, and collapses it on a second click', () => {
    renderBlock();
    const kustomize = option(/Kustomize/);
    expect(kustomize).toHaveAccessibleName(/Recommended/);
    fireEvent.click(kustomize);
    expect(kustomize).toHaveAttribute('aria-pressed', 'true');
    expect(kustomize).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Built into kubectl')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'kubectl.docs.kubernetes.io/guides' })).toHaveAttribute(
      'href',
      'https://kubectl.docs.kubernetes.io/guides/',
    );
    fireEvent.click(kustomize);
    expect(kustomize).toHaveAttribute('aria-expanded', 'false');
    expect(kustomize).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByText('Built into kubectl')).not.toBeInTheDocument();
  });

  it('moves the selection, never stacking two picks on one question', () => {
    renderBlock();
    fireEvent.click(option(/Kustomize/));
    fireEvent.click(option(/Helm/));
    expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'false');
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
  });

  it('says model knowledge instead of links when research is off', () => {
    renderBlock({ research_off: true });
    fireEvent.click(option(/Kustomize/));
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText(/model knowledge/)).toBeInTheDocument();
  });

  it('shows a code source and an unsafe url as text, never as a link', () => {
    renderBlock({
      questions: [
        {
          number: 1,
          title: 'T',
          text: 'Q?',
          options: [{ title: 'Alpha', sources: [{ tag: 'C1', title: 'internal/api/chats.go' }, { tag: 'S1', title: 'evil', url: 'javascript:alert(1)' }] }],
        },
      ],
    });
    fireEvent.click(option(/Alpha/));
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText('internal/api/chats.go')).toBeInTheDocument();
    expect(screen.getByText('evil')).toBeInTheDocument();
  });

  it('offers a typed answer as the last option on every question, and typing it replaces an option pick', () => {
    renderBlock();
    for (const name of [/Deploy tooling/, /Logging/, /Postgres/]) {
      fireEvent.click(tab(name));
      expect(screen.getByRole('textbox', { name: /Your own answer/ })).toBeInTheDocument();
    }
    fireEvent.click(tab(/Deploy tooling/));
    fireEvent.click(option(/Helm/));
    fireEvent.change(screen.getByRole('textbox', { name: /Your own answer to Q1/ }), { target: { value: 'Neither, plain YAML' } });
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'false');
  });

  it('switches tabs with the arrow keys, picks with digits and jumps to the own answer with the next digit', () => {
    renderBlock();
    const first = tab(/Deploy tooling/);
    first.focus();
    fireEvent.keyDown(first, { key: 'ArrowRight' });
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(tab(/Logging/));
    fireEvent.keyDown(tab(/Logging/), { key: '2' });
    expect(option(/No stack/)).toHaveAttribute('aria-pressed', 'true');
    fireEvent.keyDown(tab(/Logging/), { key: '3' });
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: /Your own answer to Q2/ }));
    fireEvent.keyDown(tab(/Logging/), { key: 'ArrowLeft' });
    expect(tab(/Deploy tooling/)).toHaveAttribute('aria-selected', 'true');
  });

  it('leaves typing alone: digits and arrows in the own answer type, Enter moves to the next question', () => {
    renderBlock();
    const input = screen.getByRole('textbox', { name: /Your own answer to Q1/ });
    fireEvent.keyDown(input, { key: '2' });
    fireEvent.keyDown(input, { key: 'ArrowRight' });
    expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'false');
    expect(tab(/Deploy tooling/)).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(tab(/Logging/)).toHaveAttribute('aria-selected', 'true');
  });

  it('lets a paused or responder round be read but never answered', () => {
    for (const mode of ['paused', 'readonly'] as const) {
      const { unmount } = renderBlock({ answers: [{ number: 1, option: 2 }] }, mode);
      fireEvent.click(tab(/Deploy tooling/));
      expect(option(/Helm/)).toHaveAttribute('aria-pressed', 'true');
      fireEvent.click(option(/Kustomize/));
      expect(option(/Kustomize/)).toHaveAttribute('aria-expanded', 'true');
      expect(option(/Kustomize/)).toHaveAttribute('aria-pressed', 'false');
      expect(screen.getByRole('textbox', { name: /Your own answer to Q1/ })).toBeDisabled();
      unmount();
    }
  });

  it('shows the stored own answer in the input of an answered question', () => {
    renderBlock({ answers: [{ number: 2, text: 'Loki only' }] });
    fireEvent.click(tab(/Logging/));
    const pane = screen.getByRole('tabpanel');
    expect(within(pane).getByRole('textbox', { name: /Your own answer to Q2/ })).toHaveValue('Loki only');
  });
});
