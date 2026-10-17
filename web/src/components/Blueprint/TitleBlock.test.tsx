import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TitleBlock } from './TitleBlock';
import { DESIGN_REVIEW } from '../../test-utils/blueprintFixtures';

function renderBlock(over: Partial<Parameters<typeof TitleBlock>[0]> = {}) {
  const props: Parameters<typeof TitleBlock>[0] = {
    stage: 'solution_design',
    persona: 'system_designer',
    options: [
      { version: 1, label: '1, superseded' },
      { version: 2, label: '2' },
    ],
    version: 2,
    onPickVersion: vi.fn(),
    review: { review: DESIGN_REVIEW, version: 1 },
    reviewing: null,
    status: { text: 'At your gate', tone: 'waiting' },
    compare: { available: true, on: false, onToggle: vi.fn() },
    ...over,
  };
  render(<TitleBlock {...props} />);
  return props;
}

describe('TitleBlock', () => {
  it('names the reviewer at work instead of Contract checks', () => {
    renderBlock({ reviewing: 'senior_developer', review: null });
    expect(screen.getByText('Senior developer, reviewing')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reviewing/ })).not.toBeInTheDocument();
    expect(screen.queryByText('Contract checks')).not.toBeInTheDocument();
  });

  it('prefers the reviewer at work over an earlier score', () => {
    renderBlock({ reviewing: 'senior_developer' });
    expect(screen.getByText('Senior developer, reviewing')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /of 5/ })).not.toBeInTheDocument();
  });

  it('names the sheet, who drew it and its status', () => {
    renderBlock();
    expect(screen.getByText('Design')).toBeInTheDocument();
    expect(screen.getByText('System designer')).toBeInTheDocument();
    expect(screen.getByText('At your gate')).toHaveClass('bp-st--waiting');
  });

  it('picks another revision', () => {
    const props = renderBlock();
    const rev = screen.getByRole('combobox', { name: 'Rev' });
    expect([...rev.querySelectorAll('option')].map((o) => o.textContent)).toEqual(['1, superseded', '2']);
    fireEvent.change(rev, { target: { value: '1' } });
    expect(props.onPickVersion).toHaveBeenCalledWith(1);
  });

  it('shows a single revision as plain text', () => {
    renderBlock({ options: [{ version: 1, label: '1' }], version: 1, compare: { available: false, on: false, onToggle: vi.fn() } });
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.queryByRole('checkbox', { name: 'Compare' })).not.toBeInTheDocument();
  });

  it('opens the review with each finding marked addressed or sent', () => {
    renderBlock();
    fireEvent.click(screen.getByRole('button', { name: 'Senior developer, 4 of 5' }));
    const review = screen.getByRole('dialog', { name: 'Review of rev 1' });
    expect(review).toHaveTextContent('addressed in rev 2 by System designer: Uses SetIndent on the existing encoder.');
    expect(review).toHaveTextContent('sent to rev 2');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('prevents default on Escape in the open review so an enclosing overlay stays open', () => {
    renderBlock();
    const trigger = screen.getByRole('button', { name: 'Senior developer, 4 of 5' });
    fireEvent.click(trigger);
    expect(fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })).toBe(false);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(fireEvent.keyDown(trigger, { key: 'Escape' })).toBe(true);
  });

  it('shows a review without findings', () => {
    renderBlock({ review: { review: { ...DESIGN_REVIEW, findings: [] }, version: 1 } });
    fireEvent.click(screen.getByRole('button', { name: 'Senior developer, 4 of 5' }));
    expect(screen.getByRole('dialog')).toHaveTextContent('No findings.');
  });

  it('says contract checks for an unreviewed stage', () => {
    renderBlock({ review: null });
    expect(screen.getByText('Contract checks')).toBeInTheDocument();
  });

  it('toggles compare', () => {
    const props = renderBlock();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compare' }));
    expect(props.compare.onToggle).toHaveBeenCalled();
  });
});
