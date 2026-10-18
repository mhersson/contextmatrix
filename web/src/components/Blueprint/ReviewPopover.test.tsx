import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { ReviewPopover } from './ReviewPopover';
import type { BlueprintArtifactReview } from '../../types';
import { DESIGN_REVIEW, LEDGER_REVIEW_FAILED, LEDGER_REVIEW_V1, LEDGER_REVIEW_V2 } from '../../test-utils/blueprintFixtures';

function findings(review: BlueprintArtifactReview, version: number) {
  render(<ReviewPopover review={{ review, version }} />);
  return within(screen.getByRole('list', { name: 'Findings' })).getAllByRole('listitem');
}

describe('ReviewPopover', () => {
  it('shows each finding with its id, severity and the reviewer status', () => {
    const [r1, r2, r3, r4] = findings(LEDGER_REVIEW_V2, 2);
    expect(within(r1).getByText('R1')).toBeInTheDocument();
    expect(within(r1).getByText('major')).toBeInTheDocument();
    expect(within(r1).getByText('fixed in rev 2')).toBeInTheDocument();
    expect(within(r2).getByText('critical')).toBeInTheDocument();
    expect(within(r3).getByText('still open in rev 2')).toBeInTheDocument();
    expect(r3).toHaveTextContent('rev 3 says: Asserts the key order in the handler test.');
    expect(within(r4).getByText('open')).toBeInTheDocument();
    expect(r4).toHaveTextContent('sent to rev 3');
    expect(screen.getByRole('list', { name: 'Findings' }).tagName).toBe('UL');
  });

  it('puts the builder note beside the verdict as a claim', () => {
    const [r1, , r3] = findings(LEDGER_REVIEW_V1, 1);
    expect(within(r1).getByText('open')).toBeInTheDocument();
    expect(r1).toHaveTextContent('rev 2 says: Uses SetIndent on the existing encoder.');
    expect(r1).not.toHaveTextContent('fixed');
    expect(r3).toHaveTextContent('sent to rev 2');
  });

  it('marks a critical finding the reviewer has not fixed', () => {
    const [r3, r4, r5] = findings(LEDGER_REVIEW_FAILED, 3);
    expect(r5).toHaveClass('bp-finding--blocking');
    expect(r3).not.toHaveClass('bp-finding--blocking');
    expect(r4).not.toHaveClass('bp-finding--blocking');
  });

  it('does not mark a critical finding the reviewer fixed', () => {
    const [, r2] = findings(LEDGER_REVIEW_V2, 2);
    expect(r2).not.toHaveClass('bp-finding--blocking');
  });

  it('states the round and the verdict', () => {
    render(<ReviewPopover review={{ review: LEDGER_REVIEW_FAILED, version: 3 }} />);
    expect(screen.getByRole('dialog', { name: 'Review of rev 3' })).toHaveTextContent('Round 3 · passes at 4 of 5 · failed');
  });

  it('keeps an older architect review numbered, with claims and no verdict', () => {
    const [f1, f2, f3] = findings(DESIGN_REVIEW, 1);
    expect(screen.getByRole('list', { name: 'Findings' }).tagName).toBe('OL');
    expect(f1).toHaveTextContent('rev 2 says: Uses SetIndent on the existing encoder.');
    expect(f2).toHaveTextContent('sent to rev 2');
    expect(f3).toHaveTextContent('rev 2 says it is addressed');
    expect(screen.getByRole('dialog')).not.toHaveTextContent(/passed|failed/);
  });
});
